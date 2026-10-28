// Paquete client: lo que un bot necesita para entrar al sistema como
// cualquier jugador. Solo habla con /api/* y /play a través del proxy;
// nunca con los servicios internos por gRPC.
package client

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// maxBatch es el máximo de cuentas por solicitud de la API.
	maxBatch = 100
	// defaultRetryAfter se usa si un 429 llega sin cabecera Retry-After válida.
	defaultRetryAfter = 6 * time.Second
	// maxRetryAfter acota la espera pedida por el servidor.
	maxRetryAfter = 2 * time.Minute
)

// ErrUnauthorized indica credenciales o token rechazados (HTTP 401 o cierre
// 4401 del WebSocket). Un bot en ese estado no debe reintentar.
var ErrUnauthorized = errors.New("unauthorized")

// HTTPError es una respuesta no exitosa de la API.
type HTTPError struct {
	Status  int
	Message string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("api responded %d: %s", e.Status, e.Message)
}

func (e *HTTPError) Is(target error) bool {
	return target == ErrUnauthorized && e.Status == http.StatusUnauthorized
}

// Account es una cuenta de bot aprovisionada, ya con su token.
type Account struct {
	PlayerID  string `json:"player_id"`
	Username  string `json:"username"`
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
}

// RankingEntry es una fila del ranking devuelta por GET /api/ranking.
type RankingEntry struct {
	Rank         uint32 `json:"rank"`
	PlayerID     string `json:"player_id"`
	Username     string `json:"username"`
	LeavesEarned string `json:"leaves_earned"`
}

// RankingResponse es la respuesta de GET /api/ranking.
type RankingResponse struct {
	Tick    string         `json:"tick"`
	Entries []RankingEntry `json:"entries"`
}

// API es el cliente REST de la API del juego.
type API struct {
	mu     sync.Mutex
	bases  []*url.URL
	active int

	HTTP *http.Client
	// Sleep espera d o hasta que ctx termine; las pruebas lo sustituyen.
	Sleep func(ctx context.Context, d time.Duration) error
	// MaxRetries son los reintentos por solicitud ante un 429.
	MaxRetries int
}

// NewAPI crea el cliente. baseURL es la dirección del proxy (o una lista de
// direcciones separadas por comas, p. ej.
// https://10.68.17.80:8080,https://10.68.17.99:8080).
func NewAPI(baseURL string) (*API, error) {
	var bases []*url.URL
	seen := make(map[string]struct{})
	for _, part := range strings.Split(baseURL, ",") {
		raw := strings.TrimSpace(part)
		if raw == "" {
			continue
		}
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, fmt.Errorf("invalid base URL %q: want http(s)://host[:port]", raw)
		}
		if _, ok := seen[u.String()]; !ok {
			seen[u.String()] = struct{}{}
			bases = append(bases, u)
		}
	}
	if len(bases) == 0 {
		return nil, fmt.Errorf("invalid base URL %q: want http(s)://host[:port]", baseURL)
	}
	return &API{
		bases: bases,
		// 100 cuentas con Argon2id tardan varios segundos (src/api/README.md).
		HTTP:       &http.Client{Timeout: 60 * time.Second},
		Sleep:      sleepCtx,
		MaxRetries: 6,
	}, nil
}

func (a *API) currentBase() *url.URL {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.bases[a.active%len(a.bases)]
}

func (a *API) advanceBase() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.bases) > 1 {
		a.active = (a.active + 1) % len(a.bases)
	}
}

func (a *API) baseCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.bases)
}

// UseTLS fija la configuración TLS del cliente (certificado de confianza
// propio o, solo en pruebas, sin verificación). nil deja los valores del
// sistema.
func (a *API) UseTLS(cfg *tls.Config) {
	if cfg == nil {
		return
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = cfg
	a.HTTP.Transport = tr
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Login entra con usuario y contraseña y devuelve el token firmado.
func (a *API) Login(ctx context.Context, username, password string) (string, error) {
	var out struct {
		Token string `json:"token"`
	}
	_, err := a.post(ctx, "/api/auth/login", "", map[string]string{"username": username, "password": password}, &out)
	if err != nil {
		return "", fmt.Errorf("login: %w", err)
	}
	if out.Token == "" {
		return "", errors.New("login: response without token")
	}
	return out.Token, nil
}

// ProvisionBots crea count cuentas de bot con POST /api/admin/bot-accounts,
// en lotes de hasta 100 y respetando Retry-After ante un 429. Ante un error
// devuelve las cuentas de los lotes ya completados junto con el error.
func (a *API) ProvisionBots(ctx context.Context, adminToken string, count int, prefix string) ([]Account, error) {
	var all []Account
	for remaining := count; remaining > 0; {
		n := min(remaining, maxBatch)
		var out struct {
			Accounts []Account `json:"accounts"`
		}
		body := map[string]any{"count": n, "prefix": prefix}
		if err := a.postWithRetry(ctx, "/api/admin/bot-accounts", adminToken, body, &out); err != nil {
			return all, fmt.Errorf("provision bot accounts: %w", err)
		}
		if len(out.Accounts) != n {
			return all, fmt.Errorf("provision bot accounts: asked for %d, got %d", n, len(out.Accounts))
		}
		all = append(all, out.Accounts...)
		remaining -= n
	}
	return all, nil
}

func (a *API) postWithRetry(ctx context.Context, path, token string, body, out any) error {
	for attempt := 0; ; attempt++ {
		hdr, err := a.post(ctx, path, token, body, out)
		var he *HTTPError
		if !errors.As(err, &he) || he.Status != http.StatusTooManyRequests || attempt >= a.MaxRetries {
			return err
		}
		wait := retryAfter(hdr)
		if serr := a.Sleep(ctx, wait); serr != nil {
			return serr
		}
	}
}

func retryAfter(h http.Header) time.Duration {
	secs, err := strconv.Atoi(h.Get("Retry-After"))
	if err != nil || secs < 0 {
		return defaultRetryAfter
	}
	return min(time.Duration(secs)*time.Second, maxRetryAfter)
}

func (a *API) post(ctx context.Context, path, token string, body, out any) (http.Header, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	hdr, _, err := a.doWithFailover(ctx, token, out, func(base *url.URL) (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, base.JoinPath(path).String(), bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		return req, nil
	})
	return hdr, err
}

func isGatewayDown(err error) bool {
	if err == nil {
		return false
	}
	var he *HTTPError
	if errors.As(err, &he) {
		return he.Status == http.StatusBadGateway || he.Status == http.StatusServiceUnavailable || he.Status == http.StatusGatewayTimeout
	}
	return true
}

func (a *API) doWithFailover(ctx context.Context, token string, out any, buildReq func(base *url.URL) (*http.Request, error)) (http.Header, time.Duration, error) {
	total := a.baseCount()
	var lastHdr http.Header
	var lastLat time.Duration
	var lastErr error
	for attempt := 0; attempt < total; attempt++ {
		req, err := buildReq(a.currentBase())
		if err != nil {
			return nil, 0, err
		}
		lastHdr, lastLat, lastErr = a.do(req, token, out)
		if lastErr == nil || ctx.Err() != nil || !isGatewayDown(lastErr) || attempt+1 >= total {
			return lastHdr, lastLat, lastErr
		}
		a.advanceBase()
	}
	return lastHdr, lastLat, lastErr
}

// do envía la petición con el token (si hay) y decodifica en out una
// respuesta 2xx (out puede ser nil). Devuelve las cabeceras y lo que tardó el
// intercambio HTTP; una respuesta que no es 2xx es un *HTTPError.
func (a *API) do(req *http.Request, token string, out any) (http.Header, time.Duration, error) {
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	start := time.Now()
	resp, err := a.HTTP.Do(req)
	latency := time.Since(start)
	if latency <= 0 {
		latency = time.Microsecond
	}
	if err != nil {
		return nil, latency, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return resp.Header, latency, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var e struct {
			Error string `json:"error"`
		}
		msg := http.StatusText(resp.StatusCode)
		if json.Unmarshal(data, &e) == nil && e.Error != "" {
			msg = e.Error
		}
		return resp.Header, latency, &HTTPError{Status: resp.StatusCode, Message: msg}
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return resp.Header, latency, fmt.Errorf("decode response: %w", err)
		}
	}
	return resp.Header, latency, nil
}

// GetRanking consulta GET /api/ranking?limit=N con el token de sesión y
// devuelve la respuesta, la latencia de la petición HTTP y el posible error.
func (a *API) GetRanking(ctx context.Context, token string, limit int) (RankingResponse, time.Duration, error) {
	path := "/api/ranking"
	if limit > 0 {
		path = fmt.Sprintf("/api/ranking?limit=%d", limit)
	}
	var out RankingResponse
	_, latency, err := a.get(ctx, path, token, &out)
	if err != nil {
		return RankingResponse{}, latency, fmt.Errorf("get ranking: %w", err)
	}
	return out, latency, nil
}

func (a *API) get(ctx context.Context, path, token string, out any) (http.Header, time.Duration, error) {
	u, err := url.Parse(path)
	if err != nil {
		return nil, 0, err
	}
	return a.doWithFailover(ctx, token, out, func(base *url.URL) (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodGet, base.ResolveReference(u).String(), nil)
	})
}

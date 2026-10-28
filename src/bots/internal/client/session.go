package client

import (
	"context"
	crand "crypto/rand"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

const (
	closeUnauthorized = websocket.StatusCode(4401)
	writeTimeout      = 5 * time.Second
	dialTimeout       = 6 * time.Second
	// DefaultMaxResendAge es la edad máxima de una acción pendiente para
	// reenviarla al reconectarse: el hub responde `expired` a los 30 s.
	DefaultMaxResendAge = 30 * time.Second
)

// ErrNotConnected se devuelve al enviar sin conexión abierta.
var ErrNotConnected = errors.New("not connected")

// Outcome es el resultado final de una acción enviada.
type Outcome struct {
	ActionID string
	// Kind es el type del comando ("drop", "place_installation", ...).
	Kind string
	// Status: applied | duplicate | rejected | expired | rate_limited |
	// paused | forbidden, o "error" (marco de error del hub) o "expired"
	// con Local=true (descartada por el cliente al reconectarse).
	Status string
	// Reason: motivo de un `rejected`, o el code de un `error`.
	Reason string
	// Latency va desde el primer envío hasta este resultado.
	Latency time.Duration
	// Local indica que el resultado lo decidió el cliente, no el hub.
	Local bool
}

// Applied dice si la acción cuenta como aplicada: el cliente trata
// `duplicate` como `applied` (CLAUDE.md).
func (o Outcome) Applied() bool { return o.Status == "applied" || o.Status == "duplicate" }

// Frame es un marco `result` o `error` del hub que no corresponde a ninguna
// acción pendiente: el `duplicate` de un reenvío, o el error de un mensaje
// mal formado o de tipo desconocido. Reason es el motivo de un `rejected` o el
// code de un `error`.
type Frame struct {
	Type     string
	ActionID string
	Status   string
	Reason   string
}

// SessionConfig configura una PlaySession. Solo BaseURL y Token son
// obligatorios.
type SessionConfig struct {
	// BaseURL es la dirección del proxy (http(s)://host:puerto); se
	// convierte a ws(s) y se le añade /play.
	BaseURL string
	Token   string
	// TLS es la configuración de wss (certificado de confianza propio o, solo
	// en pruebas, sin verificación); nil usa los certificados del sistema.
	TLS *tls.Config
	// Map pide el mapa en cada StateMessage (map=true); los bots de carga
	// lo apagan.
	Map    bool
	Logger *slog.Logger
	// Now, NewID y Backoff existen para las pruebas.
	Now     func() time.Time
	NewID   func() string
	Backoff func(attempt int) time.Duration
	// MaxResendAge: por defecto DefaultMaxResendAge.
	MaxResendAge time.Duration
	// ReadTimeout limita cuánto tiempo puede pasar conn.Read sin recibir
	// ningún marco (por ejemplo cuando el contenedor del proxy o del hub está
	// en docker pause y no envía TCP RST). Si es 0, se usa el ctx de Run.
	ReadTimeout time.Duration
}

type pendingAction struct {
	id     string
	kind   string
	frame  []byte
	sentAt time.Time
}

// Session es una sesión de un jugador en /play: envía comandos, lleva sus
// pendientes, entrega resultados y estados por canales y se reconecta sola.
type Session struct {
	cfg       SessionConfig
	urls      []string
	activeURL int
	states    chan State
	outcomes  chan Outcome
	unmatched chan Frame

	mu      sync.Mutex
	conn    *websocket.Conn
	pending map[string]*pendingAction
}

// NewSession valida la configuración y arma las URLs de /play (admite varias
// URLs separadas por comas en BaseURL para conmutación automática).
func NewSession(cfg SessionConfig) (*Session, error) {
	var urls []string
	seen := make(map[string]struct{})
	for _, part := range strings.Split(cfg.BaseURL, ",") {
		raw := strings.TrimSpace(part)
		if raw == "" {
			continue
		}
		u, err := playURL(raw, cfg.Token, cfg.Map)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[u]; !ok {
			seen[u] = struct{}{}
			urls = append(urls, u)
		}
	}
	if len(urls) == 0 {
		return nil, fmt.Errorf("invalid base URL %q", cfg.BaseURL)
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.DiscardHandler)
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.NewID == nil {
		cfg.NewID = newUUID
	}
	if cfg.Backoff == nil {
		cfg.Backoff = defaultBackoff
	}
	if cfg.MaxResendAge <= 0 {
		cfg.MaxResendAge = DefaultMaxResendAge
	}
	return &Session{
		cfg:       cfg,
		urls:      urls,
		states:    make(chan State, 1),
		outcomes:  make(chan Outcome, 256),
		unmatched: make(chan Frame, 64),
		pending:   make(map[string]*pendingAction),
	}, nil
}

func playURL(base, token string, withMap bool) (string, error) {
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("invalid base URL %q", base)
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	default:
		return "", fmt.Errorf("invalid base URL %q: want http(s)", base)
	}
	u = u.JoinPath("play")
	q := url.Values{}
	q.Set("token", token)
	q.Set("map", fmt.Sprint(withMap))
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func defaultBackoff(attempt int) time.Duration {
	d := min(250*time.Millisecond<<min(attempt, 6), 10*time.Second)
	return d/2 + rand.N(d/2+1)
}

func newUUID() string {
	var b [16]byte
	_, _ = crand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// States entrega el último estado recibido (si el consumidor se retrasa, se
// descartan los anteriores).
func (s *Session) States() <-chan State { return s.states }

// Outcomes entrega el resultado final de cada acción. El consumidor debe
// vaciarlo: con el canal lleno, la lectura de la conexión espera.
func (s *Session) Outcomes() <-chan Outcome { return s.outcomes }

// Unmatched entrega los marcos `result` y `error` sin acción pendiente. No
// bloquea: si nadie lo vacía, se descartan los que no caben.
func (s *Session) Unmatched() <-chan Frame { return s.unmatched }

// SendRaw escribe un marco tal cual, sin registrar nada como pendiente; sirve
// para probar mensajes mal formados. Sin conexión devuelve ErrNotConnected.
func (s *Session) SendRaw(frame []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn == nil {
		return ErrNotConnected
	}
	return s.writeLocked(s.conn, frame)
}

// ResendDrop reenvía una gota con un action_id ya usado, sin registrarla como
// pendiente: su resultado (`duplicate`) llega por Unmatched.
func (s *Session) ResendDrop(id string, x, y uint32) error {
	return s.SendRaw(encodeDrop(id, x, y))
}

// Connected dice si hay una conexión abierta.
func (s *Session) Connected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conn != nil
}

// PendingCount es el número de acciones enviadas sin resultado final.
func (s *Session) PendingCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.pending)
}

// Drop envía una gota sobre la casilla (x, y) y devuelve su action_id.
func (s *Session) Drop(x, y uint32) (string, error) {
	return s.send(typeDrop, func(id string) []byte { return encodeDrop(id, x, y) })
}

// PlaceInstallation compra y coloca "sprinkler", "drip" o "treatment_plant".
func (s *Session) PlaceInstallation(kind string, x, y uint32) (string, error) {
	return s.send(typePlace, func(id string) []byte { return encodePlace(id, kind, x, y) })
}

// BuyUpgrade compra "bigger_drop" o "bigger_tank".
func (s *Session) BuyUpgrade(upgrade string) (string, error) {
	return s.send(typeUpgrade, func(id string) []byte { return encodeUpgrade(id, upgrade) })
}

// SetDemoMode activa o apaga el modo demo (solo con token de administrador).
func (s *Session) SetDemoMode(enabled bool) (string, error) {
	return s.send(typeSetDemoMode, func(id string) []byte { return encodeDemoMode(id, enabled) })
}

// ResetValley reinicia el valle (solo con token de administrador).
func (s *Session) ResetValley() (string, error) {
	return s.send(typeResetValley, encodeReset)
}

// send registra la acción como pendiente y la escribe. Sin conexión no
// registra nada. Si la escritura falla, la acción sigue pendiente y se
// reenvía al reconectarse (si no es de administración y es reciente).
func (s *Session) send(kind string, build func(id string) []byte) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn == nil {
		return "", ErrNotConnected
	}
	id := s.cfg.NewID()
	p := &pendingAction{id: id, kind: kind, frame: build(id), sentAt: s.cfg.Now()}
	s.pending[id] = p
	if err := s.writeLocked(s.conn, p.frame); err != nil {
		s.cfg.Logger.Warn("write failed, will resend after reconnect", "err", err)
		_ = s.conn.CloseNow()
	}
	return id, nil
}

func (s *Session) writeLocked(c *websocket.Conn, frame []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
	defer cancel()
	return c.Write(ctx, websocket.MessageText, frame)
}

func (s *Session) dialOptions() *websocket.DialOptions {
	if s.cfg.TLS == nil {
		return nil
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = s.cfg.TLS
	return &websocket.DialOptions{HTTPClient: &http.Client{Transport: tr}}
}

// Run mantiene la conexión hasta que ctx termine (devuelve nil) o el hub
// cierre con 4401 o responda 401 (devuelve ErrUnauthorized, sin reconectar).
// Cualquier otra caída se reintenta con espera creciente y, al volver, se
// reenvían las acciones pendientes de menos de 30 s que no sean de
// administración.
func (s *Session) Run(ctx context.Context) error {
	attempt := 0
	for {
		targetURL := s.urls[s.activeURL%len(s.urls)]
		dialCtx, cancelDial := context.WithTimeout(ctx, dialTimeout)
		conn, resp, err := websocket.Dial(dialCtx, targetURL, s.dialOptions())
		cancelDial()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if resp != nil && resp.StatusCode == http.StatusUnauthorized {
				return ErrUnauthorized
			}
			s.cfg.Logger.Debug("dial failed", "url", targetURL, "err", err)
			if len(s.urls) > 1 {
				s.activeURL = (s.activeURL + 1) % len(s.urls)
			}
		} else {
			var gotMessage bool
			gotMessage, err = s.serve(ctx, conn)
			if gotMessage {
				attempt = 0
			}
			if errors.Is(err, ErrUnauthorized) {
				return err
			}
			if ctx.Err() != nil {
				return nil
			}
			s.cfg.Logger.Debug("connection lost", "url", targetURL, "err", err)
			if len(s.urls) > 1 {
				s.activeURL = (s.activeURL + 1) % len(s.urls)
			}
		}
		if err := sleepCtx(ctx, s.cfg.Backoff(attempt)); err != nil {
			return nil
		}
		attempt++
	}
}

func (s *Session) serve(ctx context.Context, conn *websocket.Conn) (gotMessage bool, err error) {
	conn.SetReadLimit(1 << 20)
	defer func() {
		s.mu.Lock()
		if s.conn == conn {
			s.conn = nil
		}
		s.mu.Unlock()
		_ = conn.Close(websocket.StatusNormalClosure, "bye")
	}()

	dropped, err := s.attach(conn)
	for _, o := range dropped {
		s.emit(ctx, o)
	}
	if err != nil {
		return false, err
	}

	for {
		readCtx := ctx
		var cancel context.CancelFunc
		if s.cfg.ReadTimeout > 0 {
			readCtx, cancel = context.WithTimeout(ctx, s.cfg.ReadTimeout)
		}
		_, data, err := conn.Read(readCtx)
		if cancel != nil {
			cancel()
		}
		if err != nil {
			if websocket.CloseStatus(err) == closeUnauthorized {
				return gotMessage, ErrUnauthorized
			}
			return gotMessage, err
		}
		gotMessage = true
		s.handle(ctx, data)
	}
}

// attach publica la conexión, descarta las pendientes vencidas y reenvía las
// vigentes (salvo las de administración) por orden de envío.
func (s *Session) attach(conn *websocket.Conn) (dropped []Outcome, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.conn = conn
	now := s.cfg.Now()
	list := make([]*pendingAction, 0, len(s.pending))
	for _, p := range s.pending {
		list = append(list, p)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].sentAt.Before(list[j].sentAt) })
	for _, p := range list {
		age := now.Sub(p.sentAt)
		if age > s.cfg.MaxResendAge {
			delete(s.pending, p.id)
			dropped = append(dropped, Outcome{ActionID: p.id, Kind: p.kind, Status: "expired", Latency: age, Local: true})
			continue
		}
		if isAdminType(p.kind) {
			continue
		}
		if err := s.writeLocked(conn, p.frame); err != nil {
			return dropped, err
		}
	}
	return dropped, nil
}

func (s *Session) handle(ctx context.Context, data []byte) {
	var env envelope
	if err := jsonUnmarshal(data, &env); err != nil {
		s.cfg.Logger.Debug("undecodable frame", "err", err)
		return
	}
	switch env.Type {
	case "state":
		var st State
		if err := jsonUnmarshal(data, &st); err != nil {
			s.cfg.Logger.Debug("undecodable state", "err", err)
			return
		}
		s.publishState(st)
	case "result":
		var r resultFrame
		if jsonUnmarshal(data, &r) == nil && !s.resolve(ctx, r.ActionID, r.Status, r.Reason) {
			s.publishUnmatched(Frame{Type: "result", ActionID: r.ActionID, Status: r.Status, Reason: r.Reason})
		}
	case "error":
		var e errorFrame
		if jsonUnmarshal(data, &e) == nil && !s.resolve(ctx, e.ActionID, "error", e.Code) {
			s.publishUnmatched(Frame{Type: "error", ActionID: e.ActionID, Reason: e.Code})
		}
	}
}

func (s *Session) publishState(st State) {
	select {
	case s.states <- st:
		return
	default:
	}
	select {
	case <-s.states:
	default:
	}
	select {
	case s.states <- st:
	default:
	}
}

func (s *Session) publishUnmatched(f Frame) {
	select {
	case s.unmatched <- f:
	default:
	}
}

// resolve cierra una acción pendiente y dice si lo era. Un resultado de una
// acción que ya no está pendiente (p. ej. el `duplicate` de un reenvío) no
// genera Outcome: el llamador lo publica por Unmatched.
func (s *Session) resolve(ctx context.Context, id, status, reason string) bool {
	s.mu.Lock()
	p, ok := s.pending[id]
	if ok {
		delete(s.pending, id)
	}
	now := s.cfg.Now()
	s.mu.Unlock()
	if !ok {
		return false
	}
	s.emit(ctx, Outcome{ActionID: id, Kind: p.kind, Status: status, Reason: reason, Latency: now.Sub(p.sentAt)})
	return true
}

func (s *Session) emit(ctx context.Context, o Outcome) {
	select {
	case s.outcomes <- o:
	case <-ctx.Done():
	}
}

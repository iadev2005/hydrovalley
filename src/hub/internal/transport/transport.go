// Package transport expone el hub por HTTP: el WebSocket /play y las
// comprobaciones /healthz y /readyz para Docker Compose.
//
// Biblioteca de WebSocket: github.com/coder/websocket (antes
// nhooyr.io/websocket). Tiene una API con context.Context (plazos y
// cancelación en cada lectura y escritura), se monta sobre net/http sin
// dependencias extra y está mantenida; gorilla/websocket no admite
// contextos y exige serializar las escrituras a mano.
package transport

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/coder/websocket"
	"github.com/iadev2005/hydrovalley/src/hub/internal/auth"
	"github.com/iadev2005/hydrovalley/src/hub/internal/frames"
	"github.com/iadev2005/hydrovalley/src/hub/internal/hub"
)

const (
	// Tamaño máximo de un mensaje del navegador; un comando ocupa ~100 B.
	readLimit = 4 << 10
	// Plazo de cada escritura: un cliente que no lee en este tiempo se cae.
	writeTimeout = 5 * time.Second
)

// Options ajusta el WebSocket.
type Options struct {
	// Orígenes (host[:puerto], con comodines de path.Match) que pueden abrir
	// /play además del mismo origen. Los clientes sin cabecera Origin (bots)
	// siempre pueden.
	OriginPatterns []string
	// Verifier comprueba el token de /play. Sin él (nil) todo intento se
	// cierra con 4401: nunca se abre /play sin verificar.
	Verifier *auth.Verifier
}

// closeUnauthorized es el código de cierre de /play con un token ausente,
// inválido o vencido (diagrama 09). Está en el rango 4000-4999 de uso
// privado del protocolo WebSocket y remeda al 401 de HTTP.
const closeUnauthorized websocket.StatusCode = 4401

// NewHandler monta las rutas. ctx es la vida del servidor: al cancelarlo se
// cierran todas las conexiones WebSocket con StatusGoingAway.
func NewHandler(ctx context.Context, h *hub.Hub, log *slog.Logger, opts Options) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		// Una sola foto: "ready" y el resto de campos son del mismo instante.
		st := h.Status()
		code := http.StatusOK
		if !st.Ready {
			code = http.StatusServiceUnavailable
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ready": code == http.StatusOK, "tick": strconv.FormatUint(st.Tick, 10),
			"paused": st.Paused, "breaker": st.Breaker, "queue": st.QueueLen,
		})
	})
	mux.Handle("GET /play", &playHandler{ctx: ctx, hub: h, log: log, opts: opts})
	return mux
}

type playHandler struct {
	ctx  context.Context
	hub  *hub.Hub
	log  *slog.Logger
	opts Options
}

// ServeHTTP atiende GET /play?token=<jwt>[&map=false].
//
// El token se comprueba una sola vez, al abrir (diagrama 09): firma,
// vencimiento y rol. Con un token ausente o inválido se completa el upgrade
// y se cierra con el código 4401, porque un navegador no puede leer el estado
// HTTP de un upgrade fallido y sin el código no distinguiría «vuelve a
// entrar» de una caída de red. La identidad de la sesión (player_id,
// username y rol) sale solo del token: no hay más parámetro `player_id`. Si
// el token vence con la conexión abierta, no se corta.
func (p *playHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	withMap := true
	if v := q.Get("map"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			http.Error(w, "map must be true or false", http.StatusBadRequest)
			return
		}
		withMap = b
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: p.opts.OriginPatterns})
	if err != nil {
		p.log.Debug("websocket upgrade failed", "err", err)
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(readLimit)

	id, err := p.authenticate(q.Get("token"))
	if err != nil {
		// Solo el motivo, nunca el token: acaba en registros y en la consulta.
		p.log.Info("/play refused", "reason", err)
		_ = conn.Close(closeUnauthorized, "invalid or expired token")
		return
	}

	session := p.hub.Connect(id.PlayerID, id.Role, withMap)
	defer p.hub.Disconnect(session)
	log := p.log.With("player_id", id.PlayerID, "role", id.Role)
	log.Debug("player connected", "map", withMap)

	// Al apagar el servidor se cierra la conexión con StatusGoingAway; la
	// lectura termina y con ella la escritura.
	stop := context.AfterFunc(p.ctx, func() {
		_ = conn.Close(websocket.StatusGoingAway, "server shutting down")
	})
	defer stop()
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		p.writeLoop(ctx, conn, session, log)
	}()
	p.readLoop(ctx, conn, session, log)
	cancel()
	<-done
	log.Debug("player disconnected")
}

func (p *playHandler) authenticate(token string) (auth.Identity, error) {
	if p.opts.Verifier == nil {
		return auth.Identity{}, errors.New("no token verifier configured")
	}
	return p.opts.Verifier.Verify(token)
}

// readLoop es la goroutine del jugador (Capa 1): procesa sus mensajes en
// fila, uno detrás de otro.
func (p *playHandler) readLoop(ctx context.Context, conn *websocket.Conn, s *hub.Session, log *slog.Logger) {
	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			if websocket.CloseStatus(err) == -1 && !errors.Is(err, context.Canceled) {
				log.Debug("read failed", "err", err)
			}
			return
		}
		if typ != websocket.MessageText {
			s.Send(frames.Error(frames.CodeInvalidMessage, ""))
			continue
		}
		cmd, errMsg := frames.DecodeClient(data)
		if errMsg != nil {
			s.Send(errMsg)
			continue
		}
		var outcome hub.Outcome
		if cmd.Admin != nil {
			outcome = p.hub.SubmitAdmin(s, *cmd.Admin)
		} else {
			outcome = p.hub.SubmitDrop(s, *cmd.Drop)
		}
		if outcome == hub.RejectedNoSession {
			return
		}
	}
}

// writeLoop escribe lo que el hub deja en el buzón de la sesión. Cierra la
// conexión si el hub la echa (buzón lleno).
func (p *playHandler) writeLoop(ctx context.Context, conn *websocket.Conn, s *hub.Session, log *slog.Logger) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.Kicked():
			log.Warn("closing slow connection: outbox full")
			_ = conn.Close(websocket.StatusPolicyViolation, "too slow")
			return
		case msg := <-s.Outbox():
			data, err := frames.Marshal(msg)
			if err != nil {
				log.Error("marshal failed", "err", err)
				continue
			}
			wctx, cancel := context.WithTimeout(ctx, writeTimeout)
			err = conn.Write(wctx, websocket.MessageText, data)
			cancel()
			if err != nil {
				log.Debug("write failed", "err", err)
				_ = conn.CloseNow()
				return
			}
		}
	}
}

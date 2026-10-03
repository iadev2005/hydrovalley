// Package hub es el núcleo del hub en tiempo real: admisión de comandos
// (Capa 1), idempotencia con los conjuntos pending y confirmed (Capa 3),
// cola del tick, llamada a WorldService.Step, reparto de resultados,
// Circuit Breaker y difusión del estado.
//
// No sabe nada de la red: recibe el mundo como un puerto (WorldClient) y el
// reloj como una interfaz, y el bucle del tick se alimenta de un canal. Así
// todo se prueba sin sockets y de forma determinista.
package hub

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	worldv1 "github.com/iadev2005/hydrovalley/gen/go/hydrovalley/world/v1"
	"github.com/iadev2005/hydrovalley/src/hub/internal/auth"
	"github.com/iadev2005/hydrovalley/src/hub/internal/breaker"
	"github.com/iadev2005/hydrovalley/src/hub/internal/frames"
	"github.com/iadev2005/hydrovalley/src/hub/internal/metrics"
	"github.com/iadev2005/hydrovalley/src/hub/internal/ratelimit"
	"google.golang.org/protobuf/proto"
)

// WorldClient es el puerto hacia el simulador (WorldService). El adaptador
// gRPC vive en internal/worldclient.
type WorldClient interface {
	Step(ctx context.Context, req *worldv1.StepRequest) (*worldv1.StepResponse, error)
}

// Clock da la hora del hub: el único reloj para los plazos (regla 2).
type Clock interface {
	Now() time.Time
}

// SystemClock es el reloj real.
type SystemClock struct{}

// Now devuelve la hora actual (con lectura monotónica).
func (SystemClock) Now() time.Time { return time.Now() }

// Config reúne las cifras del hub. DefaultConfig trae las del documento.
type Config struct {
	// Cada cuánto se marca el tick (lo usa quien crea el time.Ticker de Run).
	TickInterval time.Duration
	// Plazo de cada Step, sin reintento (regla de portabilidad 4).
	StepTimeout time.Duration
	// Comandos por Step como mucho.
	BatchSize int
	// Comandos en cola como mucho; los nuevos reciben "paused".
	QueueCap int
	// Gotas admitidas por jugador en cada RateWindow (RF-05).
	DropsPerSecond int
	RateWindow     time.Duration
	// Un comando con más de este tiempo en la cola recibe "expired".
	ExpireAfter time.Duration
	// Ticks que una acción aplicada sigue en confirmed (60 s).
	ConfirmedTicks uint64
	// Ticks de los que se guarda la cuenta de gotas aceptadas.
	LedgerTicks uint64
	// Fallos seguidos que abren el Circuit Breaker y espera entre pruebas.
	BreakerThreshold int
	ProbeInterval    time.Duration
	// Cada cuánto se difunde el estado (RF-03).
	BroadcastEvery time.Duration
	// Mensajes pendientes de envío por conexión; si se llena, se cierra la
	// conexión (el cliente se reconecta y reenvía sus pendientes).
	OutboxSize int
	// Cada cuánto, como mucho, se recorren todos los jugadores para borrar
	// los registros inactivos. Las bajas por eventos (desconexión, resultado
	// final, poda de confirmed) no esperan a este barrido.
	SweepInterval time.Duration
}

// DefaultConfig devuelve las cifras de CLAUDE.md y del documento (Capa 3).
func DefaultConfig() Config {
	return Config{
		TickInterval:     100 * time.Millisecond,
		StepTimeout:      200 * time.Millisecond,
		BatchSize:        500,
		QueueCap:         5000,
		DropsPerSecond:   15,
		RateWindow:       time.Second,
		ExpireAfter:      30 * time.Second,
		ConfirmedTicks:   600,
		LedgerTicks:      600,
		BreakerThreshold: 3,
		ProbeInterval:    time.Second,
		BroadcastEvery:   time.Second,
		OutboxSize:       256,
		SweepInterval:    time.Second,
	}
}

// Outcome es lo que decidió la admisión de un comando.
type Outcome int

const (
	// Admitted: entra en pending y en la cola; el resultado llega tras Step.
	Admitted Outcome = iota
	// IgnoredPending: ya estaba en pending; no se responde nada.
	IgnoredPending
	// RejectedDuplicate: ya estaba en confirmed; se responde "duplicate".
	RejectedDuplicate
	// RejectedRateLimited: más de Config.DropsPerSecond gotas en
	// Config.RateWindow; "rate_limited".
	RejectedRateLimited
	// RejectedPaused: cola llena; "paused".
	RejectedPaused
	// RejectedForbidden: comando de administración sin role = admin;
	// "forbidden". Nunca entra en pending ni en la cola.
	RejectedForbidden
	// RejectedUnsupported: comando de administración de un tipo que el hub no
	// conoce; se responde un error unknown_type y la conexión sigue abierta.
	RejectedUnsupported
	// RejectedNoSession: la conexión ya no está registrada; no se responde.
	RejectedNoSession
)

// String da un nombre legible para los registros. No es un valor del
// protocolo: para eso está WireStatus.
func (o Outcome) String() string {
	switch o {
	case Admitted:
		return "admitted"
	case IgnoredPending:
		return "ignored_pending"
	case RejectedDuplicate:
		return "rejected_duplicate"
	case RejectedRateLimited:
		return "rejected_rate_limited"
	case RejectedPaused:
		return "rejected_paused"
	case RejectedForbidden:
		return "rejected_forbidden"
	case RejectedUnsupported:
		return "rejected_unsupported"
	case RejectedNoSession:
		return "no_session"
	default:
		return "unknown"
	}
}

// WireStatus traduce un rechazo inmediato de la admisión al valor de
// ResultMessage.status de play.proto. ok es falso para los resultados que no
// se responden al jugador (admitido, ya pendiente o sin sesión).
func WireStatus(o Outcome) (status string, ok bool) {
	switch o {
	case RejectedDuplicate:
		return frames.StatusDuplicate, true
	case RejectedRateLimited:
		return frames.StatusRateLimited, true
	case RejectedPaused:
		return frames.StatusPaused, true
	case RejectedForbidden:
		return frames.StatusForbidden, true
	default:
		return "", false
	}
}

// Status es una foto del hub para /readyz y los registros, tomada bajo un
// solo candado: Ready es coherente con el resto de campos.
type Status struct {
	Tick     uint64
	Paused   bool
	Breaker  string
	QueueLen int
	// Ready: el hub puede servir partidas (circuito cerrado y el último Step
	// respondió).
	Ready bool
}

type queued struct {
	cmd        *worldv1.Command
	admittedAt time.Time
	// inFailedStep: el comando ya viajó en un Step fallido (plazo vencido,
	// error o respuesta inválida). Rust pudo haberlo aplicado sin que el hub
	// lo supiera, así que su DUPLICATE en el reintento cuenta como APPLIED.
	inFailedStep bool
}

// isDrop dice si el comando es una gota: solo las gotas consumen el límite
// de 15/s y cuentan como gotas aceptadas (las compras y los comandos de
// administración no).
func (q queued) isDrop() bool { return q.cmd.GetDrop() != nil }

type confirmEntry struct {
	playerID, actionID string
	tick               uint64
}

// player es la sesión lógica de un jugador: sobrevive a las reconexiones
// mientras el hub siga en marcha, para que un reenvío tras reconectarse
// se trate igual que en la misma conexión.
type player struct {
	sessions  map[*Session]struct{}
	pending   map[string]struct{}
	confirmed map[string]uint64
	limiter   *ratelimit.SlidingWindow
}

// idle dice si el registro del jugador se puede borrar: sin conexiones, sin
// acciones en pending o confirmed y sin gotas en su ventana del último
// segundo (borrarla permitiría saltarse el límite reconectándose).
func (p *player) idle(now time.Time) bool {
	return len(p.sessions) == 0 && len(p.pending) == 0 && len(p.confirmed) == 0 && !p.limiter.Active(now)
}

// Hub guarda todo el estado compartido tras un único candado. El candado
// nunca se mantiene durante la llamada a Step.
type Hub struct {
	world WorldClient
	clock Clock
	cfg   Config
	log   *slog.Logger

	mu      sync.Mutex
	players map[string]*player
	queue   []queued
	// confirmed en orden de tick, para podar la ventana sin recorrerlo todo.
	confirmLog []confirmEntry
	breaker    *breaker.Breaker
	paused     bool
	succeeded  bool
	tick       uint64
	bootID     string
	// latestWorld es el resumen del valle (WorldState) de la última
	// respuesta correcta de Step. Es la base del estado que se difunde y
	// se conserva mientras el valle está en pausa.
	latestWorld *worldv1.WorldState
	// Gotas aceptadas (APPLIED) por tick y en total (comprobación del modo
	// demo, RF-30).
	ledger        map[uint64]uint64
	acceptedDrops uint64
	loggedDrops   uint64
	lastBroadcast time.Time
	lastSweep     time.Time
	// Métricas de /stats: tienen su propio candado corto y no dependen de mu.
	admittedRate *metrics.Rate
	appliedRate  *metrics.Rate
	latency      *metrics.Samples
	stepTimes    *metrics.Samples
	stepFailures atomic.Uint64
}

// New crea el hub. El bucle del tick arranca con Run.
func New(world WorldClient, clock Clock, cfg Config, log *slog.Logger) *Hub {
	return &Hub{
		world:   world,
		clock:   clock,
		cfg:     cfg,
		log:     log,
		players: make(map[string]*player),
		breaker: breaker.New(cfg.BreakerThreshold, cfg.ProbeInterval),
		ledger:  make(map[uint64]uint64),

		admittedRate: metrics.NewRate(StatsWindowSeconds),
		appliedRate:  metrics.NewRate(StatsWindowSeconds),
		latency:      metrics.NewSamples(latencySamples, StatsWindowSeconds*time.Second),
		stepTimes:    metrics.NewSamples(stepSamples, 0),
	}
}

// Session es una conexión WebSocket registrada en el hub.
type Session struct {
	playerID string
	role     auth.Role
	withMap  bool
	out      chan proto.Message
	kicked   chan struct{}
	kickOnce sync.Once
}

// PlayerID devuelve el jugador de la conexión.
func (s *Session) PlayerID() string { return s.playerID }

// Role devuelve el rol del token con el que se abrió la conexión.
func (s *Session) Role() auth.Role { return s.role }

// Outbox es el buzón de mensajes que la conexión tiene que escribir.
func (s *Session) Outbox() <-chan proto.Message { return s.out }

// Kicked se cierra cuando el hub decide cerrar la conexión (buzón lleno).
func (s *Session) Kicked() <-chan struct{} { return s.kicked }

// Send encola un mensaje sin bloquear; si el buzón está lleno, echa a la
// conexión en vez de frenar el bucle del tick.
func (s *Session) Send(m proto.Message) {
	select {
	case s.out <- m:
	default:
		s.kickOnce.Do(func() { close(s.kicked) })
	}
}

// Connect registra una conexión del jugador con el rol de su token (solo
// role = admin puede enviar comandos de administración). withMap = false
// (map=false) omite el mapa en la difusión.
func (h *Hub) Connect(playerID string, role auth.Role, withMap bool) *Session {
	s := &Session{
		playerID: playerID,
		role:     role,
		withMap:  withMap,
		out:      make(chan proto.Message, h.cfg.OutboxSize),
		kicked:   make(chan struct{}),
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	p := h.playerLocked(playerID)
	p.sessions[s] = struct{}{}
	return s
}

// Disconnect da de baja la conexión. pending y confirmed del jugador se
// conservan para su próxima conexión.
func (h *Hub) Disconnect(s *Session) {
	h.mu.Lock()
	defer h.mu.Unlock()
	p, ok := h.players[s.playerID]
	if !ok {
		return
	}
	delete(p.sessions, s)
	h.dropIfIdleLocked(s.playerID, p)
}

func (h *Hub) playerLocked(id string) *player {
	p, ok := h.players[id]
	if !ok {
		p = &player{
			sessions:  make(map[*Session]struct{}),
			pending:   make(map[string]struct{}),
			confirmed: make(map[string]uint64),
			limiter:   ratelimit.NewSlidingWindow(h.cfg.DropsPerSecond, h.cfg.RateWindow),
		}
		h.players[id] = p
	}
	return p
}

// SubmitDrop aplica el filtro de la Capa 1 a una gota ya decodificada, en
// este orden: sesión, action_id en pending (se ignora) o en confirmed
// ("duplicate"), límite de gotas ("rate_limited") y cola llena ("paused").
// Los rechazos se responden al instante por el buzón de la sesión. La gota
// solo ocupa hueco en la ventana del límite cuando se admite: el límite se
// consulta sin registrar y se registra después de todas las comprobaciones.
func (h *Hub) SubmitDrop(s *Session, d frames.Drop) Outcome {
	return h.submit(s, d.ActionID, func() (*worldv1.Command, Outcome) {
		return &worldv1.Command{Kind: &worldv1.Command_Drop{Drop: &worldv1.DropCommand{X: d.X, Y: d.Y}}}, Admitted
	})
}

// SubmitPlace filtra la compra y colocación de una instalación (RF-04,
// RF-07). Pasa por la misma admisión que la gota (sesión, pending, confirmed
// y cola llena), pero no es una gota: no consume el límite de 15 por segundo
// ni cuenta como gota aceptada. Qué cuesta y si hay hojas o sitio lo decide el
// simulador, que responde con un rechazo y su motivo si no se puede.
func (h *Hub) SubmitPlace(s *Session, p frames.Place) Outcome {
	return h.submit(s, p.ActionID, func() (*worldv1.Command, Outcome) {
		return &worldv1.Command{Kind: &worldv1.Command_PlaceInstallation{
			PlaceInstallation: &worldv1.PlaceInstallationCommand{Kind: p.Kind, X: p.X, Y: p.Y},
		}}, Admitted
	})
}

// SubmitBuy filtra la compra de una mejora (RF-07), con las mismas reglas
// que SubmitPlace.
func (h *Hub) SubmitBuy(s *Session, b frames.Buy) Outcome {
	return h.submit(s, b.ActionID, func() (*worldv1.Command, Outcome) {
		return &worldv1.Command{Kind: &worldv1.Command_BuyUpgrade{
			BuyUpgrade: &worldv1.BuyUpgradeCommand{Kind: b.Kind},
		}}, Admitted
	})
}

// SubmitAdmin filtra un comando de administración (RF-31). Sin role = admin
// responde "forbidden" al instante y no deja rastro (ni pending, ni cola).
// Con ese rol pasa por la misma admisión que la gota (pending, confirmed y
// cola llena), sin el límite de gotas, y viaja a Step como cualquier comando.
func (h *Hub) SubmitAdmin(s *Session, a frames.Admin) Outcome {
	var cmd *worldv1.Command
	switch a.Kind {
	case frames.AdminSetDemoMode:
		cmd = &worldv1.Command{Kind: &worldv1.Command_SetDemoMode{SetDemoMode: &worldv1.SetDemoModeCommand{Enabled: a.Enabled}}}
	case frames.AdminResetValley:
		cmd = &worldv1.Command{Kind: &worldv1.Command_ResetValley{ResetValley: &worldv1.ResetValleyCommand{}}}
	}
	return h.submit(s, a.ActionID, func() (*worldv1.Command, Outcome) {
		switch {
		case s.role != auth.RoleAdmin:
			return nil, RejectedForbidden
		case cmd == nil:
			return nil, RejectedUnsupported
		}
		return cmd, Admitted
	})
}

// submit es la admisión común. build decide qué comando viaja o el rechazo
// previo a la cola (rol, tipo desconocido); a ese comando se le pone aquí el
// action_id y el jugador de la sesión (nunca los del mensaje).
func (h *Hub) submit(s *Session, actionID string, build func() (*worldv1.Command, Outcome)) Outcome {
	h.mu.Lock()
	defer h.mu.Unlock()
	p, ok := h.players[s.playerID]
	if !ok {
		return RejectedNoSession
	}
	if _, ok := p.sessions[s]; !ok {
		return RejectedNoSession
	}
	cmd, outcome := build()
	if outcome != Admitted {
		s.replyLocked(actionID, outcome)
		return outcome
	}
	q := queued{cmd: cmd}
	cmd.ActionId = actionID
	cmd.PlayerId = s.playerID
	if _, ok := p.pending[actionID]; ok {
		return IgnoredPending
	}
	now := h.clock.Now()
	switch {
	case hasKey(p.confirmed, actionID):
		outcome = RejectedDuplicate
	case q.isDrop() && !p.limiter.Permits(now):
		outcome = RejectedRateLimited
	case len(h.queue) >= h.cfg.QueueCap:
		outcome = RejectedPaused
	}
	if outcome != Admitted {
		s.replyLocked(actionID, outcome)
		return outcome
	}
	if q.isDrop() {
		p.limiter.Record(now)
	}
	p.pending[actionID] = struct{}{}
	q.admittedAt = now
	h.queue = append(h.queue, q)
	h.admittedRate.Add(now)
	return Admitted
}

// replyLocked responde al instante un rechazo de la admisión.
func (s *Session) replyLocked(actionID string, o Outcome) {
	if o == RejectedUnsupported {
		s.Send(frames.Error(frames.CodeUnknownType, actionID))
		return
	}
	if status, ok := WireStatus(o); ok {
		s.Send(frames.Result(actionID, status, worldv1.RejectReason_REJECT_REASON_UNSPECIFIED))
	}
}

func hasKey[V any](m map[string]V, k string) bool {
	_, ok := m[k]
	return ok
}

// Run ejecuta el bucle del tick: una vuelta por cada valor de ticks (en
// producción, un time.Ticker de 100 ms). Como es un solo bucle y Step es
// síncrono, nunca hay dos llamadas a Step a la vez; si una tarda más de
// 100 ms, el siguiente tick espera (time.Ticker descarta los atrasados).
func (h *Hub) Run(ctx context.Context, ticks <-chan time.Time) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
			h.Tick(ctx)
		}
	}
}

// AcceptedDrops devuelve la cuenta de gotas aceptadas (APPLIED).
func (h *Hub) AcceptedDrops() uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.acceptedDrops
}

// Status devuelve una foto del estado del hub, disponibilidad incluida, con
// una sola toma del candado.
func (h *Hub) Status() Status {
	h.mu.Lock()
	defer h.mu.Unlock()
	state := h.breaker.State()
	return Status{
		Tick: h.tick, Paused: h.paused, Breaker: state.String(), QueueLen: len(h.queue),
		Ready: h.succeeded && !h.paused && state == breaker.Closed,
	}
}

// Sessions devuelve cuántas conexiones hay registradas.
func (h *Hub) Sessions() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, p := range h.players {
		n += len(p.sessions)
	}
	return n
}

package load

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/iadev2005/hydrovalley/src/bots/internal/client"
)

// Config afina la prueba. Todo lo que valga cero toma el valor del
// documento (CP-11), salvo Reset y Seed.
type Config struct {
	// Duration es el tiempo que los bots envían (por defecto 60 s).
	Duration time.Duration
	// Interval y Jitter dan la espera entre gotas de cada bot: 250 ms ±
	// 50 ms, unas 4 gotas por segundo.
	Interval, Jitter time.Duration
	// Drain es lo que se espera, tras dejar de enviar, a los resultados
	// pendientes (por defecto 5 s); lo que siga sin resultado cuenta como
	// sin resolver.
	Drain time.Duration
	// Settle es la espera antes de leer los contadores finales, para que el
	// último tick ya se haya difundido (por defecto 1,5 s; el hub difunde
	// el estado una vez por segundo).
	Settle time.Duration
	// ConnectTimeout limita la espera de las conexiones /play (10 s);
	// StepTimeout, la de cada resultado de administración o estado (10 s).
	ConnectTimeout, StepTimeout time.Duration
	// Reset reinicia el valle al terminar (reset_valley). El modo demo se
	// apaga siempre.
	Reset bool
	// Seed fija las casillas y las esperas de los bots.
	Seed uint64
	// RankingClient y RankingToken activan la consulta en bucle de
	// GET /api/ranking durante la carga para medir RNF-03.
	RankingClient   RankingClient
	RankingToken    string
	RankingInterval time.Duration
	BaselineP95     time.Duration
}

func (c Config) withDefaults() Config {
	def := func(d *time.Duration, v time.Duration) {
		if *d <= 0 {
			*d = v
		}
	}
	def(&c.Duration, 60*time.Second)
	def(&c.Interval, 250*time.Millisecond)
	def(&c.Jitter, 50*time.Millisecond)
	def(&c.Drain, 5*time.Second)
	def(&c.Settle, 1500*time.Millisecond)
	def(&c.ConnectTimeout, 10*time.Second)
	def(&c.StepTimeout, 10*time.Second)
	def(&c.RankingInterval, 1*time.Second)
	return c
}

// RankingClient es lo que la prueba de carga necesita del cliente API para
// consultar GET /api/ranking y medir RNF-03.
type RankingClient interface {
	GetRanking(ctx context.Context, token string, limit int) (client.RankingResponse, time.Duration, error)
}

// Session es lo que la prueba necesita de client.Session; la interfaz vive
// aquí, en quien la usa. Los bots solo usan Drop; la sesión del
// administrador, los comandos de administración y los estados.
type Session interface {
	Run(ctx context.Context) error
	States() <-chan client.State
	Outcomes() <-chan client.Outcome
	Connected() bool
	Drop(x, y uint32) (string, error)
	SetDemoMode(enabled bool) (string, error)
	ResetValley() (string, error)
}

// Run ejecuta CP-11: activa el modo demo con la sesión del administrador,
// lee los contadores iniciales, deja que los bots suelten gotas durante
// cfg.Duration, espera los resultados pendientes, lee los contadores
// finales y devuelve el informe. Al terminar, aunque falle o se interrumpa
// (ctx), apaga el modo demo y, si cfg.Reset, reinicia el valle; los fallos
// de esa restauración van en Report.Notes. log puede ser nil.
func Run(ctx context.Context, cfg Config, admin Session, bots []Session, log *slog.Logger) (rep Report, err error) {
	cfg = cfg.withDefaults()
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	// Las sesiones y la restauración no se cortan con la interrupción:
	// el valle no debe quedar en modo demo.
	bg := context.WithoutCancel(ctx)
	sessCtx, stopSessions := context.WithCancel(bg)
	defer stopSessions()
	go func() {
		if err := admin.Run(sessCtx); err != nil {
			log.Warn("admin session ended", "err", err)
		}
	}()
	for i, b := range bots {
		go func() {
			if err := b.Run(sessCtx); err != nil {
				log.Warn("bot session ended", "bot", i, "err", err)
			}
		}()
	}

	if !waitConnected(ctx, admin, cfg.ConnectTimeout) {
		return rep, errors.New("the admin session could not connect to /play")
	}
	demoMayBeOn := true
	defer func() {
		if !demoMayBeOn {
			return
		}
		rep.Notes = append(rep.Notes, restore(bg, cfg, admin, log)...)
	}()
	if err := command(ctx, cfg, admin, func() (string, error) { return admin.SetDemoMode(true) }); err != nil {
		var rejected *rejectedError
		if errors.As(err, &rejected) {
			demoMayBeOn = false // el hub dijo que no: no hay nada que restaurar
		}
		return rep, fmt.Errorf("enabling demo mode: %w", err)
	}
	log.Info("demo mode on")

	base, err := awaitState(ctx, admin, cfg.StepTimeout, func(st client.State) bool { return st.DemoMode })
	if err != nil {
		return rep, fmt.Errorf("waiting for a state in demo mode: %w", err)
	}
	if base.Width == 0 || base.Height == 0 {
		return rep, errors.New("the state carries no valley size (width/height)")
	}
	baseline := Counters{Humidity: uint64(base.HumidityContributed), Accepted: uint64(base.AcceptedDrops)}
	log.Info("baseline read", "tick", uint64(base.Tick), "humidity_contributed", baseline.Humidity, "accepted_drops", baseline.Accepted)

	waitBotsConnected(ctx, bots, cfg.ConnectTimeout, log)

	log.Info("load starting", "bots", len(bots), "duration", cfg.Duration)
	sendCtx, stopSend := context.WithTimeout(ctx, cfg.Duration)
	defer stopSend()
	start := time.Now()

	var rankingMeasurement *RankingMeasurement
	var rankingWg sync.WaitGroup
	if cfg.RankingClient != nil && cfg.RankingToken != "" {
		rankingWg.Add(1)
		go func() {
			defer rankingWg.Done()
			rankingMeasurement = runRankingLoop(sendCtx, cfg, log)
		}()
	}

	results := make([]botResult, len(bots))
	var wg sync.WaitGroup
	for i, b := range bots {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := rand.New(rand.NewPCG(cfg.Seed, uint64(i)+1))
			results[i] = runBot(sendCtx, bg, cfg, b, r, base.Width, base.Height)
		}()
	}
	<-sendCtx.Done()
	window := time.Since(start)
	wg.Wait()
	rankingWg.Wait()
	log.Info("sending finished, results drained", "window", window.Round(time.Millisecond))

	m := Measurement{
		Bots:        len(bots),
		Window:      window,
		Results:     map[string]int{},
		Baseline:    baseline,
		Ranking:     rankingMeasurement,
		BaselineP95: cfg.BaselineP95,
	}
	for _, r := range results {
		m.Sent += r.sent
		m.Unresolved += r.sent - r.resolved
		m.SendErrors += r.sendErrors
		m.Latencies = append(m.Latencies, r.latencies...)
		for k, n := range r.results {
			m.Results[k] += n
		}
	}

	// Un estado recibido después de la espera se difundió cuando ya había
	// resultados de todo lo aplicado.
	notBefore := time.Now().Add(cfg.Settle)
	final, ferr := awaitState(bg, admin, cfg.StepTimeout+cfg.Settle, func(client.State) bool { return !time.Now().Before(notBefore) })
	if ferr != nil {
		return rep, fmt.Errorf("reading the final counters: %w", ferr)
	}
	m.Final = Counters{Humidity: uint64(final.HumidityContributed), Accepted: uint64(final.AcceptedDrops)}
	rep = BuildReport(m)
	return rep, ctx.Err()
}

// rejectedError es un comando de administración que el hub respondió con un
// estado distinto de applied.
type rejectedError struct{ status, reason string }

func (e *rejectedError) Error() string {
	if e.reason != "" {
		return fmt.Sprintf("the hub answered %s (%s)", e.status, e.reason)
	}
	return fmt.Sprintf("the hub answered %s", e.status)
}

// command envía un comando de administración y espera su resultado final.
func command(ctx context.Context, cfg Config, admin Session, send func() (string, error)) error {
	id, err := send()
	if err != nil {
		return err
	}
	return awaitResult(ctx, admin, id, cfg.StepTimeout)
}

func awaitResult(ctx context.Context, s Session, id string, timeout time.Duration) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case o := <-s.Outcomes():
			if o.ActionID != id {
				continue
			}
			if !o.Applied() {
				return &rejectedError{status: o.Status, reason: o.Reason}
			}
			return nil
		case <-timer.C:
			return fmt.Errorf("no result within %v", timeout)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// awaitState lee estados hasta uno que cumpla accept.
func awaitState(ctx context.Context, s Session, timeout time.Duration, accept func(client.State) bool) (client.State, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case st := <-s.States():
			if accept(st) {
				return st, nil
			}
		case <-timer.C:
			return client.State{}, fmt.Errorf("no suitable state within %v", timeout)
		case <-ctx.Done():
			return client.State{}, ctx.Err()
		}
	}
}

func waitConnected(ctx context.Context, s Session, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for !s.Connected() {
		if time.Now().After(deadline) || ctx.Err() != nil {
			return false
		}
		time.Sleep(10 * time.Millisecond)
	}
	return true
}

// waitBotsConnected espera a los bots con un plazo común; los que no se
// conecten solo registran turnos sin conexión.
func waitBotsConnected(ctx context.Context, bots []Session, timeout time.Duration, log *slog.Logger) {
	deadline := time.Now().Add(timeout)
	missing := 0
	for _, b := range bots {
		if !waitConnected(ctx, b, max(time.Until(deadline), 0)) {
			missing++
		}
	}
	if missing > 0 {
		log.Warn("some bots are not connected", "missing", missing, "bots", len(bots))
	}
}

// restore apaga el modo demo y, si cfg.Reset, reinicia el valle. Devuelve
// una nota por cada paso que falle.
func restore(ctx context.Context, cfg Config, admin Session, log *slog.Logger) (notes []string) {
	ctx, cancel := context.WithTimeout(ctx, 3*cfg.StepTimeout)
	defer cancel()
	if err := command(ctx, cfg, admin, func() (string, error) { return admin.SetDemoMode(false) }); err != nil {
		log.Error("could not turn demo mode off", "err", err)
		notes = append(notes, "No se pudo apagar el modo demo: "+err.Error())
	}
	if !cfg.Reset {
		return notes
	}
	if err := command(ctx, cfg, admin, admin.ResetValley); err != nil {
		log.Error("could not reset the valley", "err", err)
		notes = append(notes, "No se pudo reiniciar el valle: "+err.Error())
	}
	return notes
}

type botResult struct {
	sent, resolved, sendErrors int
	results                    map[string]int
	latencies                  []time.Duration
}

// runBot suelta gotas hasta que sendCtx termine y después espera, hasta
// cfg.Drain, los resultados que falten.
func runBot(sendCtx, bg context.Context, cfg Config, s Session, r *rand.Rand, width, height uint32) botResult {
	res := botResult{results: map[string]int{}}
	record := func(o client.Outcome) {
		key := o.Status
		if o.Status == "rejected" || o.Status == "error" {
			key += ":" + o.Reason
		}
		res.results[key]++
		res.resolved++
		if !o.Local {
			res.latencies = append(res.latencies, o.Latency)
		}
	}
	// El primer envío se reparte al azar dentro del intervalo para que los
	// bots no vayan sincronizados.
	timer := time.NewTimer(time.Duration(r.Int64N(int64(cfg.Interval)) + 1))
	defer timer.Stop()
send:
	for {
		select {
		case <-sendCtx.Done():
			break send
		case o := <-s.Outcomes():
			record(o)
		case <-timer.C:
			x, y := PickTile(r, width, height)
			if _, err := s.Drop(x, y); err != nil {
				res.sendErrors++
			} else {
				res.sent++
			}
			timer.Reset(NextDelay(r, cfg.Interval, cfg.Jitter))
		}
	}

	drain := time.NewTimer(cfg.Drain)
	defer drain.Stop()
	for res.resolved < res.sent {
		select {
		case o := <-s.Outcomes():
			record(o)
		case <-drain.C:
			return res
		case <-bg.Done():
			return res
		}
	}
	return res
}

// runRankingLoop ejecuta consultas a GET /api/ranking periódicamente mientras
// ctx esté activo para medir RNF-03.
func runRankingLoop(ctx context.Context, cfg Config, log *slog.Logger) *RankingMeasurement {
	rm := &RankingMeasurement{}
	ticker := time.NewTicker(cfg.RankingInterval)
	defer ticker.Stop()

	query := func() {
		_, lat, err := cfg.RankingClient.GetRanking(ctx, cfg.RankingToken, 100)
		if err != nil && ctx.Err() != nil {
			// La ventana de envío terminó con la consulta en vuelo: la
			// cancelamos nosotros, no es un fallo del ranking ni cuenta.
			return
		}
		rm.Sent++
		if err != nil {
			rm.Errors++
			log.Debug("ranking query error", "err", err)
			return
		}
		rm.Success++
		rm.Latencies = append(rm.Latencies, lat)
	}

	query()
	for {
		select {
		case <-ctx.Done():
			return rm
		case <-ticker.C:
			query()
		}
	}
}

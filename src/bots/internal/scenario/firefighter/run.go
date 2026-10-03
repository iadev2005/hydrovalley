package firefighter

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"slices"
	"time"

	"github.com/iadev2005/hydrovalley/src/bots/internal/client"
)

// Config afina el escenario. Lo que valga cero toma su valor por defecto,
// salvo SeedPlants y Seed.
type Config struct {
	// Duration es la ventana de vigilancia (por defecto 120 s).
	Duration time.Duration
	// Bonus son las hojas de la extinción (catalog.json: fire.
	// extinguish_bonus_leaves, 5). El bot no calcula el juego: solo lo usa
	// para reconocer, en sus propias hojas, una subida de ese tamaño.
	Bonus uint64
	// Grace es lo que se espera, tras un incendio que desaparece y al
	// terminar la ventana, a que cada bot reciba un estado posterior
	// (por defecto 3 s). RoundTimeout es lo que se espera a los resultados
	// de una ronda antes de lanzar otra (3 s).
	Grace, RoundTimeout time.Duration
	ConnectTimeout      time.Duration
	// SeedPlants hace que cada bot, al empezar, riegue SeedTiles casillas
	// con SeedDrops gotas cada una (SeedPace entre gotas) para que haya
	// plantas que puedan arder. Por defecto 3 casillas, 4 gotas, 100 ms.
	SeedPlants bool
	SeedTiles  int
	SeedDrops  int
	SeedPace   time.Duration
	Seed       uint64
}

func (c Config) withDefaults() Config {
	def := func(d *time.Duration, v time.Duration) {
		if *d <= 0 {
			*d = v
		}
	}
	def(&c.Duration, 120*time.Second)
	def(&c.Grace, 3*time.Second)
	def(&c.RoundTimeout, 3*time.Second)
	def(&c.ConnectTimeout, 10*time.Second)
	def(&c.SeedPace, 100*time.Millisecond)
	if c.Bonus == 0 {
		c.Bonus = 5
	}
	if c.SeedTiles <= 0 {
		c.SeedTiles = 3
	}
	if c.SeedDrops <= 0 {
		c.SeedDrops = 4
	}
	return c
}

// Session es lo que el escenario necesita de client.Session.
type Session interface {
	Run(ctx context.Context) error
	States() <-chan client.State
	Outcomes() <-chan client.Outcome
	Connected() bool
	Drop(x, y uint32) (string, error)
}

type event struct {
	bot     int
	state   *client.State
	outcome *client.Outcome
}

type openFire struct {
	rec    *FireRecord
	before []uint64
}

type closing struct {
	rec      *FireRecord
	before   []uint64
	tick     uint64
	deadline time.Time
}

// coord es el estado del coordinador. Lo usa una sola goroutine: no necesita
// candados.
type coord struct {
	cfg  Config
	bots []Session
	log  *slog.Logger

	view          client.State
	haveView      bool
	baseline      Counters
	leaves        []uint64
	lastTick      []uint64
	records       []*FireRecord
	open          map[Tile]*openFire
	closing       []closing
	target        *Tile
	inflight      map[string]*FireRecord
	outstanding   int
	lastRoundTick uint64
	roundAt       time.Time
	results       map[string]int
}

// Run vigila el estado con todos los bots durante cfg.Duration. Cuando
// aparece un incendio, todos sueltan a la vez una gota sobre la misma casilla
// (una ronda) y repiten con cada estado nuevo hasta que desaparece. Al
// terminar espera cfg.Grace para cerrar lo pendiente. log puede ser nil.
func Run(ctx context.Context, cfg Config, bots []Session, log *slog.Logger) (Report, error) {
	cfg = cfg.withDefaults()
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	bg := context.WithoutCancel(ctx)
	sessCtx, stopSessions := context.WithCancel(bg)
	defer stopSessions()
	for i, b := range bots {
		go func() {
			if err := b.Run(sessCtx); err != nil {
				log.Warn("bot session ended", "bot", i, "err", err)
			}
		}()
	}
	if !anyConnected(ctx, bots, cfg.ConnectTimeout) {
		return Report{}, errors.New("no bot could connect to /play")
	}

	events := make(chan event, 512)
	for i, b := range bots {
		go forward(sessCtx, i, b, events)
	}

	c := &coord{
		cfg: cfg, bots: bots, log: log,
		leaves: make([]uint64, len(bots)), lastTick: make([]uint64, len(bots)),
		open: map[Tile]*openFire{}, inflight: map[string]*FireRecord{}, results: map[string]int{},
	}
	start := time.Now()
	winCtx, cancel := context.WithTimeout(ctx, cfg.Duration)
	defer cancel()
	log.Info("firefighters watching", "bots", len(bots), "duration", cfg.Duration)

	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
watch:
	for {
		select {
		case <-winCtx.Done():
			break watch
		case ev := <-events:
			c.handle(winCtx, ev, true)
		case <-tick.C:
			c.settle()
		}
	}
	window := time.Since(start)

	// Gracia: sin enviar más, se siguen leyendo estados y resultados.
	grace := time.NewTimer(cfg.Grace)
	defer grace.Stop()
drain:
	for {
		select {
		case <-grace.C:
			break drain
		case ev := <-events:
			c.handle(winCtx, ev, false)
		case <-tick.C:
			c.settle()
		}
	}
	c.finish()

	m := Measurement{Bots: len(bots), DurationS: window.Seconds(), Results: c.results, Baseline: c.baseline,
		Final: Counters{Humidity: uint64(c.view.HumidityContributed), Accepted: uint64(c.view.AcceptedDrops)}}
	for _, r := range c.records {
		m.Fires = append(m.Fires, *r)
	}
	if !c.haveView {
		m.Notes = append(m.Notes, "Ningún bot recibió un estado del hub.")
	}
	return BuildReport(m), ctx.Err()
}

func anyConnected(ctx context.Context, bots []Session, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		connected := 0
		for _, b := range bots {
			if b.Connected() {
				connected++
			}
		}
		// Basta con uno para empezar; los demás se unen al reconectar.
		if connected > 0 {
			return true
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return false
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// forward pasa los estados y resultados de un bot al coordinador.
func forward(ctx context.Context, i int, b Session, out chan<- event) {
	for {
		var ev event
		select {
		case st := <-b.States():
			ev = event{bot: i, state: &st}
		case o := <-b.Outcomes():
			ev = event{bot: i, outcome: &o}
		case <-ctx.Done():
			return
		}
		select {
		case out <- ev:
		case <-ctx.Done():
			return
		}
	}
}

func (c *coord) handle(ctx context.Context, ev event, sending bool) {
	switch {
	case ev.state != nil:
		st := *ev.state
		if st.Player != nil {
			c.leaves[ev.bot] = uint64(st.Player.Leaves)
		}
		c.lastTick[ev.bot] = uint64(st.Tick)
		if !c.haveView || st.Tick > c.view.Tick {
			first := !c.haveView
			c.view, c.haveView = st, true
			if first {
				c.baseline = Counters{Humidity: uint64(st.HumidityContributed), Accepted: uint64(st.AcceptedDrops)}
				if c.cfg.SeedPlants && sending {
					c.seed(ctx, st.Width, st.Height)
				}
			}
			c.reconcile()
		}
		if sending {
			c.maybeRound()
		}
	case ev.outcome != nil:
		o := ev.outcome
		key := o.Status
		if o.Status == "rejected" || o.Status == "error" {
			key += ":" + o.Reason
		}
		c.results[key]++
		if rec, ok := c.inflight[o.ActionID]; ok {
			delete(c.inflight, o.ActionID)
			if o.Applied() {
				rec.Applied++
			}
			c.outstanding = max(c.outstanding-1, 0)
		}
	}
	c.settle()
}

// reconcile compara los incendios del estado más reciente con los abiertos.
func (c *coord) reconcile() {
	present := map[Tile]bool{}
	for _, f := range c.view.Fires {
		t := Tile{X: f.X, Y: f.Y}
		present[t] = true
		if _, ok := c.open[t]; !ok {
			rec := &FireRecord{X: t.X, Y: t.Y, FirstTick: uint64(c.view.Tick)}
			c.records = append(c.records, rec)
			c.open[t] = &openFire{rec: rec, before: slices.Clone(c.leaves)}
			c.log.Info("fire seen", "x", t.X, "y", t.Y, "tick", uint64(c.view.Tick))
		}
	}
	for t, of := range c.open {
		if present[t] {
			continue
		}
		of.rec.Outcome = Classify(c.view.Stages, c.view.Width, t)
		delete(c.open, t)
		if c.target != nil && *c.target == t {
			c.target = nil
		}
		c.closing = append(c.closing, closing{rec: of.rec, before: of.before, tick: uint64(c.view.Tick), deadline: time.Now().Add(c.cfg.Grace)})
		c.log.Info("fire gone", "x", t.X, "y", t.Y, "outcome", of.rec.Outcome, "rounds", of.rec.Rounds)
	}
}

// maybeRound lanza una ronda sobre el objetivo si hay un estado nuevo desde
// la última y la anterior ya tiene sus resultados (o venció).
func (c *coord) maybeRound() {
	target, ok := NextTarget(c.view.Fires, c.target)
	if !ok {
		c.target = nil
		return
	}
	c.target = &target
	if uint64(c.view.Tick) <= c.lastRoundTick {
		return
	}
	if c.outstanding > 0 && time.Since(c.roundAt) < c.cfg.RoundTimeout {
		return
	}
	of := c.open[target]
	if of == nil {
		return
	}
	c.lastRoundTick, c.roundAt, c.outstanding = uint64(c.view.Tick), time.Now(), 0
	of.rec.Rounds++
	for i, b := range c.bots {
		id, err := b.Drop(target.X, target.Y)
		if err != nil {
			c.log.Debug("drop not sent", "bot", i, "err", err)
			continue
		}
		c.inflight[id] = of.rec
		of.rec.Drops++
		c.outstanding++
	}
}

// settle cierra los incendios desaparecidos cuando cada bot ya vio un estado
// posterior a la desaparición (o venció el plazo), y mira las hojas.
func (c *coord) settle() {
	kept := c.closing[:0]
	for _, cl := range c.closing {
		ready := time.Now().After(cl.deadline)
		if !ready {
			ready = true
			for _, t := range c.lastTick {
				if t < cl.tick {
					ready = false
					break
				}
			}
		}
		if !ready {
			kept = append(kept, cl)
			continue
		}
		cl.rec.BonusBots = BonusBots(cl.before, c.leaves, c.cfg.Bonus)
		if cl.rec.BonusBots == nil {
			cl.rec.BonusBots = []int{}
		}
	}
	c.closing = kept
}

// finish cierra lo que quede al terminar: los incendios en curso se marcan
// como sin apagar si se atacaron y como pendientes si no.
func (c *coord) finish() {
	for i := range c.closing {
		c.closing[i].deadline = time.Time{}
	}
	c.settle()
	for _, of := range c.open {
		if of.rec.Rounds > 0 {
			of.rec.Outcome = OutcomeUnresolved
		} else {
			of.rec.Outcome = OutcomePendingAtEnd
		}
	}
	for _, r := range c.records {
		if r.BonusBots == nil {
			r.BonusBots = []int{}
		}
	}
}

// seed riega unas casillas propias de cada bot para que haya plantas.
func (c *coord) seed(ctx context.Context, width, height uint32) {
	if width == 0 || height == 0 {
		return
	}
	for i, b := range c.bots {
		r := rand.New(rand.NewPCG(c.cfg.Seed, uint64(i)+1))
		tiles := SeedTiles(r, width, height, c.cfg.SeedTiles)
		go func() {
			for _, t := range tiles {
				for range c.cfg.SeedDrops {
					select {
					case <-ctx.Done():
						return
					case <-time.After(c.cfg.SeedPace):
					}
					_, _ = b.Drop(t.X, t.Y)
				}
			}
		}()
	}
}

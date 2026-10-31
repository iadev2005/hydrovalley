package cheater

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/iadev2005/hydrovalley/src/bots/internal/client"
)

// Config afina el escenario. Todo lo que valga cero toma el valor del
// documento (CP-03), salvo Seed.
type Config struct {
	// Duration es lo que dura la inundación (por defecto 10 s) y Rate las
	// gotas que se envían por segundo (50).
	Duration time.Duration
	Rate     int
	// Limit es el límite del hub (15 gotas por segundo, RF-05) y Tolerance lo
	// que se admite de más por segundo medido (por defecto 1). El hub usa una
	// ventana deslizante con su propio reloj (src/hub/internal/ratelimit):
	// nunca admite más de Limit en cualquier intervalo de 1 s. El bot mide
	// con su reloj de envío y en cubos fijos de 1 s, así que la latencia de
	// la red puede mover una gota de un cubo al vecino: de ahí el +1.
	Limit, Tolerance int
	// Settle es la espera entre las fases y la inundación para que la
	// ventana del límite quede vacía (1,1 s). Drain es lo que se espera a los
	// resultados pendientes (5 s). Wait es lo que se espera a los marcos
	// sueltos tras cada envío de las fases 2 y 3 (1 s).
	Settle, Drain, Wait time.Duration
	ConnectTimeout      time.Duration
	// Seed fija las casillas elegidas.
	Seed uint64
}

func (c Config) withDefaults() Config {
	def := func(d *time.Duration, v time.Duration) {
		if *d <= 0 {
			*d = v
		}
	}
	def(&c.Duration, 10*time.Second)
	def(&c.Settle, 1100*time.Millisecond)
	def(&c.Drain, 5*time.Second)
	def(&c.Wait, time.Second)
	def(&c.ConnectTimeout, 10*time.Second)
	if c.Rate <= 0 {
		c.Rate = 50
	}
	if c.Limit <= 0 {
		c.Limit = 15
	}
	if c.Tolerance <= 0 {
		c.Tolerance = 1
	}
	return c
}

// Session es lo que el escenario necesita de client.Session.
type Session interface {
	Run(ctx context.Context) error
	States() <-chan client.State
	Outcomes() <-chan client.Outcome
	Unmatched() <-chan client.Frame
	Connected() bool
	Drop(x, y uint32) (string, error)
	ResendDrop(id string, x, y uint32) error
	SendRaw(frame []byte) error
}

// Resultado de los reenvíos de la fase 2: uno mientras la acción sigue
// pendiente y dos después de su resultado final.
const (
	immediateResends  = 1
	afterFinalResends = 2
)

// Run ejecuta las tres fases con una sola sesión. El orden es: acción
// repetida (RF-09), mensajes inválidos (RF-06) y, al final, la inundación
// (CP-03). La inundación va última porque, fuera del modo demo, cada gota
// cuesta agua y vacía el depósito: con el depósito vacío la fase 2 no podría
// ver su `applied`.
func Run(ctx context.Context, cfg Config, s Session) (Report, error) {
	cfg = cfg.withDefaults()
	sessCtx, stop := context.WithCancel(context.WithoutCancel(ctx))
	defer stop()
	go func() { _ = s.Run(sessCtx) }()

	if err := waitUntil(ctx, cfg.ConnectTimeout, s.Connected); err != nil {
		return Report{}, errors.New("the session could not connect to /play")
	}
	var st client.State
	select {
	case st = <-s.States():
	case <-time.After(cfg.ConnectTimeout):
		return Report{}, errors.New("no state received from the hub")
	case <-ctx.Done():
		return Report{}, ctx.Err()
	}
	if st.Width == 0 || st.Height == 0 {
		return Report{}, errors.New("the state carries no valley size (width/height)")
	}
	pick := tilePicker(rand.New(rand.NewPCG(cfg.Seed, 1)), st)

	var rep Report
	var err error
	if rep.Duplicates, err = duplicatesPhase(ctx, cfg, s, pick); err != nil {
		return rep, err
	}
	if rep.Invalid, err = invalidPhase(ctx, cfg, s, pick); err != nil {
		return rep, err
	}
	if err := sleepCtx(ctx, cfg.Settle); err != nil {
		return rep, err
	}
	if rep.Flood, err = floodPhase(ctx, cfg, s, pick); err != nil {
		return rep, err
	}
	if rep.Flood.Results["rejected:insufficient_water"] > 0 {
		rep.Notes = append(rep.Notes, "Fuera del modo demo las gotas cuestan agua: las rechazadas por `insufficient_water` cuentan como admitidas por el hub, porque el límite las dejó pasar (CP-03 prueba el límite, no la economía).")
	}
	return rep, nil
}

// tilePicker elige casillas al azar entre las que aceptan una gota según el
// primer estado: con el terreno natural, los edificios y la basura, una
// casilla cualquiera podría rechazarla con `tile_occupied` y la fase de la
// acción repetida no vería su `applied`. Sin mapa, cualquier casilla.
func tilePicker(r *rand.Rand, st client.State) func() (uint32, uint32) {
	var free []int
	if n := int(st.Width) * int(st.Height); len(st.Stages) >= n {
		for i := range n {
			if st.AcceptsDrop(i) {
				free = append(free, i)
			}
		}
	}
	if len(free) == 0 {
		return func() (uint32, uint32) { return r.Uint32N(st.Width), r.Uint32N(st.Height) }
	}
	return func() (uint32, uint32) {
		i := free[r.IntN(len(free))]
		return uint32(i) % st.Width, uint32(i) / st.Width
	}
}

func duplicatesPhase(ctx context.Context, cfg Config, s Session, pick func() (uint32, uint32)) (Duplicates, error) {
	flush(s)
	x, y := pick()
	id, err := s.Drop(x, y)
	if err != nil {
		return Duplicates{}, err
	}
	for range immediateResends {
		if err := s.ResendDrop(id, x, y); err != nil {
			return Duplicates{}, err
		}
	}
	var first []string
	if o, ok := awaitOutcome(ctx, s, id, cfg.Drain); ok {
		first = append(first, o.Status)
	}
	for range afterFinalResends {
		if err := s.ResendDrop(id, x, y); err != nil {
			return Duplicates{}, err
		}
	}
	frames := collect(ctx, s, cfg.Wait)
	var mine []client.Frame
	for _, f := range frames {
		if f.ActionID == id {
			mine = append(mine, f)
		}
	}
	return BuildDuplicates(first, mine, afterFinalResends, immediateResends), ctx.Err()
}

func invalidPhase(ctx context.Context, cfg Config, s Session, pick func() (uint32, uint32)) (Invalid, error) {
	flush(s)
	if err := s.SendRaw([]byte("{this is not json")); err != nil {
		return Invalid{}, err
	}
	if err := s.SendRaw([]byte(`{"type":"teleport"}`)); err != nil {
		return Invalid{}, err
	}
	frames := collect(ctx, s, cfg.Wait)
	x, y := pick()
	ok := false
	if id, err := s.Drop(x, y); err == nil {
		o, got := awaitOutcome(ctx, s, id, cfg.Drain)
		ok = got && o.Status != "error"
	}
	return BuildInvalid(frames, ok), ctx.Err()
}

func floodPhase(ctx context.Context, cfg Config, s Session, pick func() (uint32, uint32)) (Flood, error) {
	flush(s)
	interval := time.Second / time.Duration(cfg.Rate)
	sendCtx, cancel := context.WithTimeout(ctx, cfg.Duration)
	defer cancel()

	var samples []FloodSample
	index := map[string]int{}
	resolved := 0
	record := func(o client.Outcome) {
		i, ok := index[o.ActionID]
		if !ok || samples[i].Resolved {
			return
		}
		samples[i].Status, samples[i].Reason, samples[i].Resolved = o.Status, o.Reason, !o.Local
		resolved++
	}
	start := time.Now()
	// Envío con horario fijo (inicio + i·intervalo): una espera tardía se
	// compensa y el ritmo medio no se desvía.
	timer := time.NewTimer(0)
	defer timer.Stop()
	for i := 0; sendCtx.Err() == nil; {
		select {
		case <-sendCtx.Done():
		case o := <-s.Outcomes():
			record(o)
		case <-timer.C:
			x, y := pick()
			offset := time.Since(start)
			if id, err := s.Drop(x, y); err == nil {
				index[id] = len(samples)
				samples = append(samples, FloodSample{Offset: offset})
			}
			i++
			timer.Reset(max(time.Until(start.Add(time.Duration(i)*interval)), 0))
		}
	}
	window := time.Since(start)
	deadline := time.NewTimer(cfg.Drain)
	defer deadline.Stop()
	for resolved < len(samples) {
		select {
		case o := <-s.Outcomes():
			record(o)
		case <-deadline.C:
			return BuildFlood(FloodMeasurement{Window: window, Samples: samples, Limit: cfg.Limit, Tolerance: cfg.Tolerance}), ctx.Err()
		case <-ctx.Done():
			return Flood{}, ctx.Err()
		}
	}
	return BuildFlood(FloodMeasurement{Window: window, Samples: samples, Limit: cfg.Limit, Tolerance: cfg.Tolerance}), ctx.Err()
}

// flush descarta lo que haya quedado en los canales de una fase anterior.
func flush(s Session) {
	for {
		select {
		case <-s.Outcomes():
		case <-s.Unmatched():
		default:
			return
		}
	}
}

// collect junta los marcos sueltos que lleguen durante d.
func collect(ctx context.Context, s Session, d time.Duration) []client.Frame {
	var out []client.Frame
	t := time.NewTimer(d)
	defer t.Stop()
	for {
		select {
		case f := <-s.Unmatched():
			out = append(out, f)
		case <-s.Outcomes():
		case <-t.C:
			return out
		case <-ctx.Done():
			return out
		}
	}
}

func awaitOutcome(ctx context.Context, s Session, id string, timeout time.Duration) (client.Outcome, bool) {
	t := time.NewTimer(timeout)
	defer t.Stop()
	for {
		select {
		case o := <-s.Outcomes():
			if o.ActionID == id {
				return o, true
			}
		case <-t.C:
			return client.Outcome{}, false
		case <-ctx.Done():
			return client.Outcome{}, false
		}
	}
}

func waitUntil(ctx context.Context, timeout time.Duration, cond func() bool) error {
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out after %v", timeout)
		}
		if err := sleepCtx(ctx, 10*time.Millisecond); err != nil {
			return err
		}
	}
	return nil
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Paquete neighbors: el escenario «vecinos». Unos pocos bots que juegan
// despacio, una gota cada pocos segundos, para que el valle se sienta vivo
// sin quitarles espacio a los humanos (documento inicial, «Los bots»).
package neighbors

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

// Config afina el ritmo de los vecinos.
type Config struct {
	// Interval es la espera media entre acciones (por defecto 4 s).
	Interval time.Duration
	// Jitter es la variación relativa de la espera, de 0 a 1 (por defecto 0,5).
	Jitter float64
	// BuildChance es la probabilidad, en cada turno con hojas suficientes,
	// de comprar un aspersor en vez de soltar una gota (por defecto 0,25).
	BuildChance float64
	// Seed fija las decisiones; cada bot deriva su propio generador.
	Seed uint64
}

func (c Config) withDefaults() Config {
	if c.Interval <= 0 {
		c.Interval = 4 * time.Second
	}
	if c.Jitter == 0 {
		c.Jitter = 0.5
	}
	if c.BuildChance == 0 {
		c.BuildChance = 0.25
	}
	return c
}

// Session es lo que el escenario necesita de client.Session; la interfaz
// vive aquí, en quien la usa.
type Session interface {
	Run(ctx context.Context) error
	States() <-chan client.State
	Outcomes() <-chan client.Outcome
	Drop(x, y uint32) (string, error)
	PlaceInstallation(kind string, x, y uint32) (string, error)
}

// Summary es el resumen de la ejecución, sumado de todos los bots.
type Summary struct {
	Bots int
	// Sent son las acciones enviadas; Results las cuenta por estado
	// ("applied", "duplicate", "rejected:<motivo>", "rate_limited", ...).
	Sent    int
	Results map[string]int
	// Unresolved son las enviadas sin resultado final al terminar.
	Unresolved int
	// SendErrors son turnos en que no hubo conexión para enviar.
	SendErrors int
	// Unauthorized son los bots detenidos por un token rechazado (4401).
	Unauthorized int
}

func newSummary() Summary { return Summary{Results: map[string]int{}} }

func (s *Summary) add(o Summary) {
	s.Bots += o.Bots
	s.Sent += o.Sent
	s.Unresolved += o.Unresolved
	s.SendErrors += o.SendErrors
	s.Unauthorized += o.Unauthorized
	for k, v := range o.Results {
		s.Results[k] += v
	}
}

// Run lanza un bot por sesión y espera a que todos terminen: cuando ctx
// termina (señal o duración) o cuando cada uno se detiene por su cuenta.
// log puede ser nil.
func Run(ctx context.Context, cfg Config, sessions []Session, log *slog.Logger) Summary {
	cfg = cfg.withDefaults()
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	var wg sync.WaitGroup
	results := make([]Summary, len(sessions))
	for i, s := range sessions {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := rand.New(rand.NewPCG(cfg.Seed, uint64(i)+1))
			results[i] = runBot(ctx, cfg, s, r, log.With("bot", i))
		}()
	}
	wg.Wait()
	total := newSummary()
	for _, r := range results {
		total.add(r)
	}
	return total
}

func runBot(ctx context.Context, cfg Config, s Session, r *rand.Rand, log *slog.Logger) Summary {
	sum := newSummary()
	sum.Bots = 1
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.Run(sctx) }()

	var st *client.State
	timer := time.NewTimer(NextDelay(r, cfg.Interval, cfg.Jitter))
	defer timer.Stop()
	record := func(o client.Outcome) {
		key := o.Status
		if o.Status == "rejected" || o.Status == "error" {
			key += ":" + o.Reason
		}
		sum.Results[key]++
	}
	finish := func() Summary {
		cancel()
		for {
			select {
			case o := <-s.Outcomes():
				record(o)
			default:
				resolved := 0
				for _, n := range sum.Results {
					resolved += n
				}
				sum.Unresolved = max(sum.Sent-resolved, 0)
				return sum
			}
		}
	}

	for {
		select {
		case <-ctx.Done():
			<-done
			return finish()
		case err := <-done:
			if errors.Is(err, client.ErrUnauthorized) {
				sum.Unauthorized = 1
				log.Warn("token rejected, stopping bot")
			}
			return finish()
		case v := <-s.States():
			st = &v
		case o := <-s.Outcomes():
			record(o)
		case <-timer.C:
			act(cfg, s, r, st, &sum, log)
			timer.Reset(NextDelay(r, cfg.Interval, cfg.Jitter))
		}
	}
}

func act(cfg Config, s Session, r *rand.Rand, st *client.State, sum *Summary, log *slog.Logger) {
	a := Decide(r, st, cfg)
	var err error
	switch a.Kind {
	case ActionDrop:
		_, err = s.Drop(a.X, a.Y)
	case ActionPlace:
		_, err = s.PlaceInstallation(a.Installation, a.X, a.Y)
	default:
		return
	}
	if err != nil {
		sum.SendErrors++
		log.Debug("send failed", "err", err)
		return
	}
	sum.Sent++
	log.Debug("sent", "kind", fmt.Sprint(a.Kind), "x", a.X, "y", a.Y)
}

// Package simulator es el servicio de aplicación: guarda el mundo, decide
// cuándo está listo y envía las instantáneas en segundo plano. Es el espejo de
// src/world/src/simulator.rs.
//
// No conoce gRPC más allá del puerto store.Store (instantáneas y catálogo): la
// capa de red (internal/server) traduce las peticiones y llama aquí.
package simulator

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/iadev2005/hydrovalley/src/world-go/internal/codec"
	"github.com/iadev2005/hydrovalley/src/world-go/internal/domain"
	"github.com/iadev2005/hydrovalley/src/world-go/internal/store"
	"github.com/iadev2005/hydrovalley/src/world-go/internal/uuid"
)

// ErrNotReady: el mundo todavía no se ha cargado; Step responde UNAVAILABLE.
var ErrNotReady = errors.New("el simulador todavía está cargando el mundo")

// Backoff son las esperas entre intentos de GetCatalog y LoadSnapshot al
// arrancar: 1 s, 2 s, 4 s y luego 5 s indefinidamente.
type Backoff struct {
	Initial time.Duration
	Max     time.Duration
}

// DefaultBackoff es el de CLAUDE.md (regla 8): de 1 s a 5 s.
func DefaultBackoff() Backoff { return Backoff{Initial: time.Second, Max: 5 * time.Second} }

// Next es la espera que sigue a current.
func (b Backoff) Next(current time.Duration) time.Duration { return min(current*2, b.Max) }

// defaultResetCatalogTimeout es el plazo para volver a pedir el catálogo al
// reiniciar el valle. Es la mitad del plazo de Step (200 ms): si la
// persistencia no contesta a tiempo, el reinicio se aplica con el catálogo
// vigente en vez de hacer fallar el tick.
const defaultResetCatalogTimeout = 100 * time.Millisecond

// Options configura el simulador.
type Options struct {
	// Seed es la semilla del generador de azar cuando el valle empieza de
	// cero. Si hay instantánea manda el estado guardado en ella, no esta
	// semilla.
	Seed uint64
	// Backoff son las esperas de la carga inicial; cero usa DefaultBackoff.
	Backoff Backoff
	// ResetCatalogTimeout es el plazo del catálogo del reinicio; cero usa
	// 100 ms.
	ResetCatalogTimeout time.Duration
	// Log puede ser nil.
	Log *slog.Logger
}

// Report es lo que devuelve un paso.
type Report struct {
	Outcome domain.StepOutcome
	// RestoredTick es el tick del que se recuperó este arranque (0 si empezó
	// vacío).
	RestoredTick uint64
}

// Simulator guarda el mundo y serializa sus pasos (regla 1: escritor único).
type Simulator struct {
	bootID       string
	seed         uint64
	store        store.Store
	backoff      Backoff
	resetTimeout time.Duration
	log          *slog.Logger

	// mu protege world y restoredTick, y serializa los pasos: dos Step nunca
	// se solapan.
	mu           sync.Mutex
	world        *domain.World // nil mientras carga
	restoredTick uint64

	// turn tiene un elemento mientras hay un guardado en curso (como mucho
	// uno).
	turn chan struct{}
	// savedTick es el tick de la última instantánea que está en la
	// persistencia (la cargada al arrancar o la última guardada con éxito).
	savedTick atomic.Uint64

	// runCtx es el contexto de los guardados en segundo plano; Shutdown lo
	// cancela al terminar.
	runCtx context.Context
	cancel context.CancelFunc
}

// New crea un simulador sin mundo: Load lo deja listo.
func New(st store.Store, opts Options) *Simulator {
	if opts.Backoff == (Backoff{}) {
		opts.Backoff = DefaultBackoff()
	}
	if opts.ResetCatalogTimeout <= 0 {
		opts.ResetCatalogTimeout = defaultResetCatalogTimeout
	}
	if opts.Log == nil {
		opts.Log = slog.New(slog.DiscardHandler)
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Simulator{
		bootID:       uuid.New(),
		seed:         opts.Seed,
		store:        st,
		backoff:      opts.Backoff,
		resetTimeout: opts.ResetCatalogTimeout,
		log:          opts.Log,
		turn:         make(chan struct{}, 1),
		runCtx:       ctx,
		cancel:       cancel,
	}
}

// BootID es el UUID v4 generado al crear el simulador; va en todas las
// respuestas.
func (s *Simulator) BootID() string { return s.bootID }

// IsReady dice si el mundo ya está cargado.
func (s *Simulator) IsReady() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.world != nil
}

// saving dice si hay un guardado en curso.
func (s *Simulator) saving() bool { return len(s.turn) == 1 }

// sleep espera d o hasta que ctx termine; devuelve false en ese caso.
func sleep(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// fetchCatalog pide el catálogo a la persistencia y reintenta sin fin hasta
// obtenerlo: sin cifras no se puede simular.
func (s *Simulator) fetchCatalog(ctx context.Context) (domain.Catalog, error) {
	delay := s.backoff.Initial
	for {
		catalog, err := s.store.GetCatalog(ctx)
		if err == nil {
			s.log.Info("catálogo cargado", "catalog_version", catalog.Version)
			return catalog, nil
		}
		s.log.Warn("GetCatalog falló; se reintenta", "err", err, "delay", delay)
		if !sleep(ctx, delay) {
			return domain.Catalog{}, ctx.Err()
		}
		delay = s.backoff.Next(delay)
	}
}

// Load carga el catálogo y la última instantánea, y deja el mundo listo.
// Solo con NOT_FOUND empieza vacío en el tick 0; ante cualquier otro fallo
// (del catálogo o de la instantánea) reintenta sin fin, así el tick nunca
// retrocede. El mundo aplica el catálogo vigente, también si la instantánea se
// tomó con otra versión. Devuelve un error solo si ctx termina antes.
func (s *Simulator) Load(ctx context.Context) error {
	catalog, err := s.fetchCatalog(ctx)
	if err != nil {
		return err
	}
	delay := s.backoff.Initial
	var (
		world        *domain.World
		restoredTick uint64
	)
	for world == nil {
		snapshot, err := s.store.Load(ctx)
		switch {
		case err != nil:
			s.log.Warn("LoadSnapshot falló; se reintenta", "err", err, "delay", delay)
		case snapshot == nil:
			s.log.Info("no hay instantáneas: el valle empieza en el tick 0 (con esta semilla se repite la partida)", "seed", s.seed)
			world = domain.NewWorld(catalog, s.seed)
		default:
			tick := snapshot.GetTick()
			decoded, decodeErr := codec.DecodeSnapshot(snapshot, catalog)
			if decodeErr != nil {
				s.log.Error("instantánea ilegible; se reintenta", "tick", tick, "err", decodeErr, "delay", delay)
				break
			}
			if saved := snapshot.GetCatalogVersion(); saved != catalog.Version {
				s.log.Info("la instantánea es de otra versión del catálogo: se aplica la vigente",
					"tick", tick, "saved", saved, "current", catalog.Version)
			}
			s.log.Info("instantánea cargada", "tick", tick)
			world, restoredTick = decoded, tick
		}
		if world == nil {
			if !sleep(ctx, delay) {
				return ctx.Err()
			}
			delay = s.backoff.Next(delay)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.world == nil {
		s.savedTick.Store(restoredTick)
		s.world, s.restoredTick = world, restoredTick
	}
	return nil
}

// CatalogFor devuelve un catálogo fresco para un tick que trae un
// ResetValley (RF-31): el reinicio vuelve a pedirlo para que un cambio de
// cifras en catalog/ surta efecto sin reiniciar el simulador. Hace un solo
// intento, con plazo, antes de tomar el bloqueo del mundo: si la persistencia
// falla o tarda, devuelve nil y el reinicio se aplica con el catálogo vigente
// (se registra el aviso). Sin reinicio (o con el mundo sin cargar) no llama a
// la persistencia.
func (s *Simulator) CatalogFor(ctx context.Context, commands []domain.Command) *domain.Catalog {
	resets := false
	for _, command := range commands {
		if _, ok := command.Kind.(domain.ResetValleyCommand); ok {
			resets = true
			break
		}
	}
	if !resets || !s.IsReady() {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, s.resetTimeout)
	defer cancel()
	catalog, err := s.store.GetCatalog(ctx)
	if err != nil {
		s.log.Warn("GetCatalog falló al reiniciar: se usa el catálogo vigente", "err", err, "timeout", s.resetTimeout)
		return nil
	}
	s.log.Info("catálogo recargado para el reinicio", "catalog_version", catalog.Version)
	return &catalog
}

// Step aplica un tick. catalog (lo devuelve CatalogFor) se ofrece al reinicio
// del tick: el mundo lo adopta solo si el ResetValley se aplica. view, si no
// es nil, lee el mundo resultante con el bloqueo tomado; debe ser breve (copiar
// lo que necesite).
func (s *Simulator) Step(commands []domain.Command, catalog *domain.Catalog, view func(*domain.World)) (Report, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.world == nil {
		return Report{}, ErrNotReady
	}
	outcome := s.world.StepWithResetCatalog(commands, catalog)
	if domain.IsSnapshotTick(outcome.Tick) {
		s.scheduleSnapshot(s.world)
	}
	if view != nil {
		view(s.world)
	}
	return Report{Outcome: outcome, RestoredTick: s.restoredTick}, nil
}

// Read ejecuta view sobre el mundo con el bloqueo tomado, sin modificarlo.
// view debe ser breve: copia lo que necesite y hace el trabajo pesado después,
// ya sin el bloqueo, para no retrasar Step.
func (s *Simulator) Read(view func(*domain.World)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.world == nil {
		return ErrNotReady
	}
	view(s.world)
	return nil
}

// scheduleSnapshot copia el mundo y lo guarda desde una goroutine: Step nunca
// espera a la persistencia. Si el guardado anterior sigue en curso, esta
// instantánea se salta y el siguiente tick de instantánea lo vuelve a
// intentar. Se llama con s.mu tomado.
func (s *Simulator) scheduleSnapshot(world *domain.World) {
	tick := world.Tick()
	select {
	case s.turn <- struct{}{}:
	default:
		s.log.Warn("guardado anterior en curso: se salta la instantánea", "tick", tick)
		return
	}
	parts := world.Parts()
	go func() {
		defer func() { <-s.turn }()
		s.save(s.runCtx, parts)
	}()
}

// save codifica la copia del mundo, la guarda y, si sale bien, anota su tick.
func (s *Simulator) save(ctx context.Context, parts domain.WorldParts) {
	snapshot, err := codec.EncodeParts(parts)
	if err != nil {
		s.log.Error("no se pudo codificar la instantánea", "tick", parts.Tick, "err", err)
		return
	}
	if err := s.store.Save(ctx, snapshot); err != nil {
		s.log.Error("SaveSnapshot falló", "tick", parts.Tick, "err", err)
		return
	}
	for {
		saved := s.savedTick.Load()
		if parts.Tick <= saved || s.savedTick.CompareAndSwap(saved, parts.Tick) {
			break
		}
	}
	s.log.Debug("instantánea guardada", "tick", parts.Tick)
}

// Shutdown es la parada ordenada, cuando el servidor ya no acepta
// peticiones: espera al guardado en curso y, si el mundo está cargado y el
// tick avanzó desde la última instantánea guardada, envía una instantánea
// final del tick actual (aunque no sea múltiplo de 50). Todo con un plazo
// total de timeout; devuelve false si se agotó.
func (s *Simulator) Shutdown(timeout time.Duration) bool {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	defer s.cancel()

	// Espera a que quede libre el turno de guardado y lo toma.
	select {
	case s.turn <- struct{}{}:
	case <-ctx.Done():
		s.log.Error("la instantánea final no terminó a tiempo", "timeout", timeout)
		return false
	}
	defer func() { <-s.turn }()

	s.mu.Lock()
	var parts domain.WorldParts
	pending := s.world != nil && s.world.Tick() > s.savedTick.Load()
	if pending {
		parts = s.world.Parts()
	}
	s.mu.Unlock()
	if !pending {
		return true
	}
	s.log.Info("instantánea final antes de parar", "tick", parts.Tick)
	s.save(ctx, parts)
	if ctx.Err() != nil {
		s.log.Error("la instantánea final no terminó a tiempo", "timeout", timeout)
		return false
	}
	return true
}

package simulator

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	persistencev1 "github.com/iadev2005/hydrovalley/gen/go/hydrovalley/persistence/v1"
	worldv1 "github.com/iadev2005/hydrovalley/gen/go/hydrovalley/world/v1"
	"google.golang.org/protobuf/proto"

	"github.com/iadev2005/hydrovalley/src/world-go/internal/codec"
	"github.com/iadev2005/hydrovalley/src/world-go/internal/domain"
	"github.com/iadev2005/hydrovalley/src/world-go/internal/fakepersist"
	"github.com/iadev2005/hydrovalley/src/world-go/internal/store"
)

// Portadas de las pruebas de src/world/src/simulator.rs.

const ana = "6f1c1a52-2d0e-4f4e-9c1e-0c6c2d9b8a01"

func d01(t testing.TB) domain.Catalog {
	t.Helper()
	catalog, err := codec.CatalogFromProto(fakepersist.Catalog())
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

// fakeStore es un store.Store en memoria con guiones de respuestas.
type fakeStore struct {
	t testing.TB

	mu       sync.Mutex
	catalogs []result[domain.Catalog] // respuestas de GetCatalog; agotadas, el catálogo de D-01
	loads    []result[*persistencev1.Snapshot]
	calls    []string // orden de las llamadas de carga: "catalog" y "load"
	saved    []*persistencev1.Snapshot
	saveErr  error
	// gate, si no es nil, hace que cada guardado espere un permiso.
	gate chan struct{}

	catalogCalls atomic.Int64
	loadCalls    atomic.Int64
	saveCalls    atomic.Int64
}

type result[T any] struct {
	value T
	err   error
}

func (f *fakeStore) GetCatalog(context.Context) (domain.Catalog, error) {
	f.catalogCalls.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "catalog")
	if len(f.catalogs) == 0 {
		return d01(f.t), nil
	}
	next := f.catalogs[0]
	f.catalogs = f.catalogs[1:]
	return next.value, next.err
}

func (f *fakeStore) Load(context.Context) (*persistencev1.Snapshot, error) {
	f.loadCalls.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "load")
	if len(f.loads) == 0 {
		return nil, nil
	}
	next := f.loads[0]
	f.loads = f.loads[1:]
	return next.value, next.err
}

func (f *fakeStore) Save(ctx context.Context, snapshot *persistencev1.Snapshot) error {
	f.saveCalls.Add(1)
	if f.gate != nil {
		select {
		case <-f.gate:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.saveErr != nil {
		return f.saveErr
	}
	f.saved = append(f.saved, snapshot)
	return nil
}

func (f *fakeStore) savedTicks() []uint64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	var ticks []uint64
	for _, s := range f.saved {
		ticks = append(ticks, s.GetTick())
	}
	return ticks
}

func (f *fakeStore) callOrder() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func unavailable() error { return &store.Error{Kind: store.KindUnavailable, Message: "down"} }

func fast() Backoff { return Backoff{Initial: time.Millisecond, Max: 2 * time.Millisecond} }

func newSim(t testing.TB, st store.Store, seed uint64) *Simulator {
	t.Helper()
	s := New(st, Options{Seed: seed, Backoff: fast()})
	t.Cleanup(func() { s.Shutdown(100 * time.Millisecond) })
	return s
}

func ready(t testing.TB, st *fakeStore) *Simulator {
	t.Helper()
	st.t = t
	s := newSim(t, st, 0)
	if err := s.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s
}

func step(t testing.TB, s *Simulator, count int) {
	t.Helper()
	for range count {
		if _, err := s.Step(nil, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
}

func eventually(t testing.TB, what string, condition func() bool) {
	t.Helper()
	for range 400 {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no se cumplió: %s", what)
}

func dropAt(n uint32, x, y uint32) domain.Command {
	return domain.Command{
		ActionID: "00000000-0000-4000-8000-" + padded(n),
		PlayerID: ana,
		Kind:     domain.DropCommand{X: x, Y: y},
	}
}

func padded(n uint32) string {
	const digits = "0123456789"
	out := []byte("000000000000")
	for i := len(out) - 1; n > 0 && i >= 0; i-- {
		out[i] = digits[n%10]
		n /= 10
	}
	return string(out)
}

func resetCommand() domain.Command {
	return domain.Command{ActionID: "00000000-0000-4000-8000-0000000000aa", PlayerID: ana, Kind: domain.ResetValleyCommand{}}
}

// snapshotAt es una instantánea con una gota en (1, 1), llevada al tick dado.
func snapshotAt(t testing.TB, tick uint64) *persistencev1.Snapshot {
	t.Helper()
	world := domain.NewWorld(d01(t), 0)
	world.Step([]domain.Command{dropAt(1, 1, 1)})
	parts := world.Parts()
	parts.Tick = tick
	restored, err := domain.Restore(d01(t), parts)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := codec.EncodeSnapshot(restored)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func found(snapshot *persistencev1.Snapshot) result[*persistencev1.Snapshot] {
	return result[*persistencev1.Snapshot]{value: snapshot}
}

func TestBackoffDoublesFromOneSecondUpToFive(t *testing.T) {
	backoff := DefaultBackoff()
	delay := backoff.Initial
	seen := []time.Duration{delay}
	for range 4 {
		delay = backoff.Next(delay)
		seen = append(seen, delay)
	}
	want := []time.Duration{1, 2, 4, 5, 5}
	for i, d := range want {
		if seen[i] != d*time.Second {
			t.Fatalf("esperas: %v", seen)
		}
	}
}

func TestBootIDIsAFreshUUIDv4(t *testing.T) {
	st := &fakeStore{t: t}
	a, b := newSim(t, st, 0), newSim(t, st, 0)
	if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(a.BootID()) {
		t.Fatalf("boot_id no es un UUID v4: %q", a.BootID())
	}
	if a.BootID() == b.BootID() {
		t.Fatal("cada arranque tiene su boot_id")
	}
}

func TestStepIsUnavailableUntilTheWorldIsLoaded(t *testing.T) {
	s := newSim(t, &fakeStore{t: t}, 0)
	if s.IsReady() {
		t.Fatal("recién creado no está listo")
	}
	if _, err := s.Step(nil, nil, nil); !errors.Is(err, ErrNotReady) {
		t.Fatalf("error: %v", err)
	}
	if err := s.Read(func(*domain.World) {}); !errors.Is(err, ErrNotReady) {
		t.Fatalf("Read: %v", err)
	}
}

func TestNotFoundStartsAnEmptyWorldAtTickZero(t *testing.T) {
	s := ready(t, &fakeStore{loads: []result[*persistencev1.Snapshot]{found(nil)}})
	players := -1
	report, err := s.Step(nil, nil, func(w *domain.World) { players = len(w.Players()) })
	if err != nil {
		t.Fatal(err)
	}
	if report.Outcome.Tick != 1 || report.RestoredTick != 0 || players != 0 {
		t.Fatalf("tick %d, restored %d, jugadores %d", report.Outcome.Tick, report.RestoredTick, players)
	}
}

func TestAnEmptyWorldStartsFromTheConfiguredSeedAndASnapshotKeepsItsOwn(t *testing.T) {
	seeded := func(seed uint64, loads ...result[*persistencev1.Snapshot]) []byte {
		st := &fakeStore{t: t, loads: loads}
		s := newSim(t, st, seed)
		if err := s.Load(context.Background()); err != nil {
			t.Fatal(err)
		}
		var rng []byte
		if _, err := s.Step(nil, nil, func(w *domain.World) { rng = w.RngState() }); err != nil {
			t.Fatal(err)
		}
		return rng
	}
	fresh := seeded(99, found(nil))
	if !slices.Equal(fresh, domain.NewSplitMix64(99).Bytes()) {
		t.Fatalf("estado del generador sin instantánea: %x", fresh)
	}
	// La instantánea trae su generador (semilla 0 y ningún sorteo, pues el
	// valle de la prueba no tiene casillas elegibles): la semilla de
	// configuración no lo pisa.
	restored := seeded(99, found(snapshotAt(t, 150)))
	if !slices.Equal(restored, domain.NewSplitMix64(0).Bytes()) {
		t.Fatalf("estado del generador restaurado: %x", restored)
	}
}

func TestAFoundSnapshotIsRestoredAndReportedOnEveryStep(t *testing.T) {
	s := ready(t, &fakeStore{loads: []result[*persistencev1.Snapshot]{found(snapshotAt(t, 150))}})
	for _, expected := range []uint64{151, 152} {
		var humidity uint64
		report, err := s.Step(nil, nil, func(w *domain.World) {
			tile, _ := w.Tile(1, 1)
			humidity = tile.Humidity
		})
		if err != nil {
			t.Fatal(err)
		}
		if report.Outcome.Tick != expected || report.RestoredTick != 150 || humidity != 25 {
			t.Fatalf("tick %d, restored %d, humedad %d", report.Outcome.Tick, report.RestoredTick, humidity)
		}
	}
}

func TestLoadKeepsRetryingUntilPersistenceAnswers(t *testing.T) {
	corrupt := snapshotAt(t, 50)
	corrupt.WorldState = []byte{0xff}
	st := &fakeStore{loads: []result[*persistencev1.Snapshot]{
		{err: unavailable()},
		{err: unavailable()},
		found(corrupt),
		{err: &store.Error{Kind: store.KindFailed, Message: "boom"}},
		found(snapshotAt(t, 100)),
	}}
	s := ready(t, st)
	if n := st.loadCalls.Load(); n != 5 {
		t.Fatalf("LoadSnapshot se llamó %d veces, quiero 5", n)
	}
	report, _ := s.Step(nil, nil, nil)
	if report.RestoredTick != 100 {
		t.Fatalf("restored_tick = %d", report.RestoredTick)
	}
}

func TestLoadStopsWhenItsContextEnds(t *testing.T) {
	st := &fakeStore{t: t, loads: slices.Repeat([]result[*persistencev1.Snapshot]{{err: unavailable()}}, 1000)}
	s := newSim(t, st, 0)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := s.Load(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Load: %v", err)
	}
	if s.IsReady() {
		t.Fatal("no debe quedar listo sin instantánea ni NOT_FOUND")
	}
}

func TestStepAppliesCommandsThroughTheDomain(t *testing.T) {
	s := ready(t, &fakeStore{})
	var contributed uint64
	report, err := s.Step([]domain.Command{dropAt(9, 4, 4)}, nil, func(w *domain.World) { contributed = w.HumidityContributed() })
	if err != nil {
		t.Fatal(err)
	}
	if report.Outcome.Results[0].Outcome.Kind != domain.Applied || contributed != 25 {
		t.Fatalf("resultado %+v, humedad %d", report.Outcome.Results[0], contributed)
	}
}

func TestASnapshotIsSavedEvery50Ticks(t *testing.T) {
	st := &fakeStore{}
	s := ready(t, st)
	step(t, s, 49)
	time.Sleep(20 * time.Millisecond)
	if n := st.saveCalls.Load(); n != 0 {
		t.Fatalf("no debe guardar antes del tick 50: %d", n)
	}
	step(t, s, 1)
	eventually(t, "guardado del tick 50", func() bool { return slices.Equal(st.savedTicks(), []uint64{50}) })
	step(t, s, 50)
	eventually(t, "guardado del tick 100", func() bool { return slices.Equal(st.savedTicks(), []uint64{50, 100}) })

	st.mu.Lock()
	row := st.saved[1]
	st.mu.Unlock()
	inner := &worldv1.WorldSnapshot{}
	if err := proto.Unmarshal(row.GetWorldState(), inner); err != nil {
		t.Fatal(err)
	}
	if inner.GetTick() != 100 || len(inner.GetTiles()) != 1024 {
		t.Fatalf("instantánea interna: tick %d, %d casillas", inner.GetTick(), len(inner.GetTiles()))
	}
}

func TestASnapshotIsSkippedWhileThePreviousOneIsInFlight(t *testing.T) {
	st := &fakeStore{gate: make(chan struct{}, 10)}
	s := ready(t, st)
	step(t, s, 50)
	eventually(t, "primer guardado en curso", func() bool { return st.saveCalls.Load() == 1 })

	step(t, s, 50) // tick 100: se salta.
	time.Sleep(20 * time.Millisecond)
	if n := st.saveCalls.Load(); n != 1 {
		t.Fatalf("la instantánea del tick 100 debía saltarse: %d guardados", n)
	}

	for range 10 {
		st.gate <- struct{}{}
	}
	eventually(t, "fin del primer guardado", func() bool { return slices.Equal(st.savedTicks(), []uint64{50}) })
	eventually(t, "guardado libre", func() bool { return !s.saving() })
	step(t, s, 50) // tick 150
	eventually(t, "guardado del tick 150", func() bool { return slices.Equal(st.savedTicks(), []uint64{50, 150}) })
}

func TestAFailedSaveNeverStopsTheGameAndIsRetriedNextTime(t *testing.T) {
	st := &fakeStore{saveErr: &store.Error{Kind: store.KindAlreadyExists, Message: "otra"}}
	s := ready(t, st)
	step(t, s, 50)
	eventually(t, "primer intento", func() bool { return st.saveCalls.Load() == 1 })
	eventually(t, "guardado libre", func() bool { return !s.saving() })

	st.mu.Lock()
	st.saveErr = nil
	st.mu.Unlock()
	step(t, s, 50)
	eventually(t, "segundo intento", func() bool { return slices.Equal(st.savedTicks(), []uint64{100}) })
	if report, _ := s.Step(nil, nil, nil); report.Outcome.Tick != 101 {
		t.Fatalf("tick %d", report.Outcome.Tick)
	}
}

func TestShutdownWaitsForTheSaveInFlightAndSavesTheLastTick(t *testing.T) {
	st := &fakeStore{gate: make(chan struct{}, 10)}
	s := ready(t, st)
	step(t, s, 50)
	eventually(t, "guardado del tick 50 en curso", func() bool { return st.saveCalls.Load() == 1 })
	step(t, s, 20) // tick 70

	go func() {
		time.Sleep(30 * time.Millisecond)
		for range 10 {
			st.gate <- struct{}{}
		}
	}()
	if len(st.savedTicks()) != 0 {
		t.Fatal("aún no debe haber guardados")
	}
	if !s.Shutdown(5 * time.Second) {
		t.Fatal("la parada debe terminar a tiempo")
	}
	if got := st.savedTicks(); !slices.Equal(got, []uint64{50, 70}) {
		t.Fatalf("guardados: %v", got)
	}
}

func TestShutdownSavesNothingWhenTheTickDidNotAdvance(t *testing.T) {
	st := &fakeStore{}
	s := ready(t, st)
	step(t, s, 50)
	eventually(t, "guardado del tick 50", func() bool { return slices.Equal(st.savedTicks(), []uint64{50}) })
	if !s.Shutdown(5 * time.Second) {
		t.Fatal("parada")
	}
	if n := st.saveCalls.Load(); n != 1 {
		t.Fatalf("guardados: %d", n)
	}

	// Recién restaurado, sin pasos: la instantánea cargada ya es la última.
	restoredStore := &fakeStore{loads: []result[*persistencev1.Snapshot]{found(snapshotAt(t, 150))}}
	restored := ready(t, restoredStore)
	if !restored.Shutdown(5 * time.Second) {
		t.Fatal("parada")
	}
	if n := restoredStore.saveCalls.Load(); n != 0 {
		t.Fatalf("guardados tras restaurar: %d", n)
	}

	// Sin mundo cargado no hay nada que guardar.
	loadingStore := &fakeStore{t: t}
	loading := newSim(t, loadingStore, 0)
	if !loading.Shutdown(5 * time.Second) {
		t.Fatal("parada")
	}
	if n := loadingStore.saveCalls.Load(); n != 0 {
		t.Fatalf("guardados sin mundo: %d", n)
	}
}

func TestShutdownIsBoundedWhenTheSaveNeverFinishes(t *testing.T) {
	st := &fakeStore{gate: make(chan struct{})}
	s := ready(t, st)
	step(t, s, 51)
	started := time.Now()
	if s.Shutdown(50 * time.Millisecond) {
		t.Fatal("con el guardado atascado la parada debe agotar su plazo")
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("la parada tardó %v", elapsed)
	}
}

func TestShutdownTwiceSavesTheFinalSnapshotOnce(t *testing.T) {
	st := &fakeStore{}
	s := ready(t, st)
	step(t, s, 3)
	if !s.Shutdown(5*time.Second) || !slices.Equal(st.savedTicks(), []uint64{3}) {
		t.Fatalf("instantánea final: %v", st.savedTicks())
	}
	// Una segunda parada no repite la instantánea del mismo tick.
	if !s.Shutdown(5*time.Second) || len(st.savedTicks()) != 1 {
		t.Fatalf("la segunda parada no debe guardar de nuevo: %v", st.savedTicks())
	}
}

func TestTheCatalogIsFetchedBeforeTheSnapshot(t *testing.T) {
	st := &fakeStore{}
	s := ready(t, st)
	if !s.IsReady() || !slices.Equal(st.callOrder(), []string{"catalog", "load"}) {
		t.Fatalf("orden de las llamadas: %v", st.callOrder())
	}
}

func TestAnUnavailableCatalogKeepsTheWorldNotReadyAndIsRetried(t *testing.T) {
	st := &fakeStore{t: t, catalogs: []result[domain.Catalog]{
		{err: unavailable()},
		{err: &store.Error{Kind: store.KindFailed, Message: "catálogo no válido"}},
	}}
	s := newSim(t, st, 0)
	if err := s.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := st.catalogCalls.Load(); n != 3 {
		t.Fatalf("GetCatalog se llamó %d veces, quiero 3", n)
	}
	// Sin catálogo no se consultó la instantánea.
	if got := st.callOrder(); !slices.Equal(got, []string{"catalog", "catalog", "catalog", "load"}) {
		t.Fatalf("orden de las llamadas: %v", got)
	}
	if !s.IsReady() {
		t.Fatal("debe quedar listo")
	}
}

func TestSnapshotsCarryTheCatalogVersion(t *testing.T) {
	st := &fakeStore{}
	s := ready(t, st)
	step(t, s, 50)
	eventually(t, "SaveSnapshot del tick 50", func() bool { return slices.Equal(st.savedTicks(), []uint64{50}) })
	st.mu.Lock()
	saved := st.saved[0]
	st.mu.Unlock()
	if saved.GetCatalogVersion() != fakepersist.CatalogVersion {
		t.Fatalf("versión de la fila: %q", saved.GetCatalogVersion())
	}
	inner := &worldv1.WorldSnapshot{}
	if err := proto.Unmarshal(saved.GetWorldState(), inner); err != nil {
		t.Fatal(err)
	}
	if inner.GetCatalogVersion() != fakepersist.CatalogVersion {
		t.Fatalf("versión interna: %q", inner.GetCatalogVersion())
	}
}

func TestARestoredWorldUsesTheCurrentCatalogVersion(t *testing.T) {
	current := d01(t)
	current.Version = "nuevo"
	current.Drop.Humidity = 40
	st := &fakeStore{
		catalogs: []result[domain.Catalog]{{value: current}},
		loads:    []result[*persistencev1.Snapshot]{found(snapshotAt(t, 50))},
	}
	s := ready(t, st)
	var version string
	var humidity uint64
	if _, err := s.Step([]domain.Command{dropAt(2, 0, 0)}, nil, func(w *domain.World) {
		version = w.CatalogVersion()
		tile, _ := w.Tile(0, 0)
		humidity = tile.Humidity
	}); err != nil {
		t.Fatal(err)
	}
	if version != "nuevo" || humidity != 40 {
		t.Fatalf("versión %q, humedad %d", version, humidity)
	}
}

func versionOf(into *string) func(*domain.World) {
	return func(w *domain.World) { *into = w.CatalogVersion() }
}

func TestAResetFetchesTheCatalogAgainAndTheWorldAppliesIt(t *testing.T) {
	next := d01(t)
	next.Version = "v2"
	st := &fakeStore{catalogs: []result[domain.Catalog]{{value: d01(t)}, {value: next}}}
	s := ready(t, st)
	if n := st.catalogCalls.Load(); n != 1 {
		t.Fatalf("GetCatalog: %d", n)
	}
	commands := []domain.Command{resetCommand()}
	fresh := s.CatalogFor(context.Background(), commands)
	if n := st.catalogCalls.Load(); n != 2 || fresh == nil {
		t.Fatalf("el reinicio debe pedir el catálogo: %d llamadas, fresh=%v", n, fresh)
	}
	var version string
	report, err := s.Step(commands, fresh, versionOf(&version))
	if err != nil {
		t.Fatal(err)
	}
	if version != "v2" || report.Outcome.Results[0].Outcome.Kind != domain.Applied {
		t.Fatalf("versión %q, resultado %+v", version, report.Outcome.Results[0])
	}
}

func TestWithoutAResetTheCatalogIsNotFetched(t *testing.T) {
	st := &fakeStore{}
	s := ready(t, st)
	if s.CatalogFor(context.Background(), []domain.Command{dropAt(1, 1, 1)}) != nil || s.CatalogFor(context.Background(), nil) != nil {
		t.Fatal("sin reinicio no hay catálogo nuevo")
	}
	if n := st.catalogCalls.Load(); n != 1 {
		t.Fatalf("GetCatalog: %d", n)
	}
}

func TestACatalogIsNotFetchedForAWorldThatIsNotLoaded(t *testing.T) {
	st := &fakeStore{t: t}
	s := newSim(t, st, 0)
	if s.CatalogFor(context.Background(), []domain.Command{resetCommand()}) != nil || st.catalogCalls.Load() != 0 {
		t.Fatal("sin mundo no se llama a la persistencia")
	}
}

func TestAResetStillAppliesWhenTheCatalogCannotBeFetched(t *testing.T) {
	st := &fakeStore{catalogs: []result[domain.Catalog]{{value: d01(t)}, {err: unavailable()}}}
	s := ready(t, st)
	commands := []domain.Command{resetCommand()}
	fresh := s.CatalogFor(context.Background(), commands)
	if fresh != nil {
		t.Fatal("conserva el catálogo vigente")
	}
	var version string
	report, err := s.Step(commands, fresh, versionOf(&version))
	if err != nil {
		t.Fatal(err)
	}
	if version != fakepersist.CatalogVersion || report.Outcome.Results[0].Outcome.Kind != domain.Applied {
		t.Fatalf("versión %q, resultado %+v", version, report.Outcome.Results[0])
	}
}

// hangingCatalog es una persistencia cuyo catálogo nunca responde.
type hangingCatalog struct{ fakeStore }

func (h *hangingCatalog) GetCatalog(ctx context.Context) (domain.Catalog, error) {
	if h.catalogCalls.Add(1) == 1 {
		return d01(h.t), nil
	}
	<-ctx.Done()
	return domain.Catalog{}, ctx.Err()
}

func TestASlowCatalogDoesNotHoldTheStepPastItsTimeout(t *testing.T) {
	st := &hangingCatalog{fakeStore{t: t}}
	s := newSim(t, st, 0)
	if err := s.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	if defaultResetCatalogTimeout >= 200*time.Millisecond {
		t.Fatalf("el plazo del catálogo del reinicio debe ser menor que el de Step: %v", defaultResetCatalogTimeout)
	}
	started := time.Now()
	fresh := s.CatalogFor(context.Background(), []domain.Command{resetCommand()})
	elapsed := time.Since(started)
	if fresh != nil {
		t.Fatal("al vencer el plazo se usa el catálogo vigente")
	}
	if elapsed < defaultResetCatalogTimeout || elapsed > defaultResetCatalogTimeout+time.Second {
		t.Fatalf("la espera debe ser el plazo (%v), fue %v", defaultResetCatalogTimeout, elapsed)
	}
}

func TestADuplicateOrRejectedResetDoesNotSwapTheCatalog(t *testing.T) {
	next := d01(t)
	next.Version = "v2"
	st := &fakeStore{catalogs: []result[domain.Catalog]{{value: d01(t)}, {value: next}, {value: next}}}
	s := ready(t, st)

	applied := []domain.Command{resetCommand()}
	if _, err := s.Step(applied, nil, nil); err != nil {
		t.Fatal(err)
	}

	duplicate := s.CatalogFor(context.Background(), applied)
	if duplicate == nil {
		t.Fatal("el reinicio duplicado también pide el catálogo")
	}
	var version string
	report, _ := s.Step(applied, duplicate, versionOf(&version))
	if report.Outcome.Results[0].Outcome.Kind != domain.Duplicate || version != fakepersist.CatalogVersion {
		t.Fatalf("duplicado: %+v, versión %q", report.Outcome.Results[0], version)
	}

	bad := resetCommand()
	bad.ActionID = "not-a-uuid"
	rejected := []domain.Command{bad}
	fresh := s.CatalogFor(context.Background(), rejected)
	if fresh == nil {
		t.Fatal("el reinicio rechazado también pide el catálogo")
	}
	report, _ = s.Step(rejected, fresh, versionOf(&version))
	if report.Outcome.Results[0].Outcome.Kind != domain.Rejected || version != fakepersist.CatalogVersion {
		t.Fatalf("rechazado: %+v, versión %q", report.Outcome.Results[0], version)
	}
}

func TestConcurrentStepsNeverOverlap(t *testing.T) {
	s := ready(t, &fakeStore{})
	var inside atomic.Int32
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 20 {
				if _, err := s.Step(nil, nil, func(*domain.World) {
					if inside.Add(1) != 1 {
						t.Error("dos Step a la vez dentro del mundo")
					}
					time.Sleep(50 * time.Microsecond)
					inside.Add(-1)
				}); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
	report, _ := s.Step(nil, nil, nil)
	if report.Outcome.Tick != 161 {
		t.Fatalf("tick %d, quiero 161 (cada Step avanza uno)", report.Outcome.Tick)
	}
}

func TestStandbyModeDoesNotLoadSnapshotUntilFirstStepPromotion(t *testing.T) {
	st := &fakeStore{t: t, loads: []result[*persistencev1.Snapshot]{found(snapshotAt(t, 200))}}
	s := New(st, Options{Seed: 0, Standby: true, Backoff: fast()})
	t.Cleanup(func() { s.Shutdown(100 * time.Millisecond) })

	if !s.IsStandby() || s.IsReady() {
		t.Fatalf("debe arrancar en standby y no listo: standby=%v ready=%v", s.IsStandby(), s.IsReady())
	}
	if n := st.loadCalls.Load(); n != 0 {
		t.Fatalf("no debe llamar a LoadSnapshot en standby: %d llamadas", n)
	}

	// El primer intento promueve Standby -> Loading y devuelve ErrNotReady.
	if err := s.EnsureActiveAndFresh(0); !errors.Is(err, ErrNotReady) {
		t.Fatalf("EnsureActiveAndFresh en standby debe devolver ErrNotReady, dio %v", err)
	}
	eventually(t, "promoción de standby completada", s.IsReady)
	if n := st.loadCalls.Load(); n != 1 {
		t.Fatalf("LoadSnapshot tras promoción = %d, quiero 1", n)
	}

	report, err := s.Step(nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if report.RestoredTick != 200 || report.Outcome.Tick != 201 {
		t.Fatalf("restored=%d tick=%d, quiero 200 y 201", report.RestoredTick, report.Outcome.Tick)
	}
}

func TestWaitActiveAndFreshPromotesStandbyOnFirstCallWhenStoreIsFast(t *testing.T) {
	st := &fakeStore{t: t, loads: []result[*persistencev1.Snapshot]{found(snapshotAt(t, 200))}}
	s := New(st, Options{Seed: 0, Standby: true, Backoff: fast()})
	t.Cleanup(func() { s.Shutdown(100 * time.Millisecond) })

	if err := s.WaitActiveAndFresh(context.Background(), 0); err != nil {
		t.Fatalf("WaitActiveAndFresh(0) = %v, quiero nil", err)
	}
	if !s.IsReady() || st.loadCalls.Load() != 1 {
		t.Fatalf("ready=%v loadCalls=%d", s.IsReady(), st.loadCalls.Load())
	}
	report, err := s.Step(nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if report.RestoredTick != 200 || report.Outcome.Tick != 201 {
		t.Fatalf("restored=%d tick=%d, quiero 200 y 201", report.RestoredTick, report.Outcome.Tick)
	}
}

func TestMinExpectedTickHigherThanLocalTickForcesReloadAndRotatesBootID(t *testing.T) {
	st := &fakeStore{
		t: t,
		loads: []result[*persistencev1.Snapshot]{
			found(snapshotAt(t, 100)),
			found(snapshotAt(t, 250)),
		},
	}
	s := ready(t, st)
	first, err := s.Step(nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.Outcome.Tick != 101 {
		t.Fatalf("tick = %d, quiero 101", first.Outcome.Tick)
	}
	originalBootID := first.BootID

	// Estuvo pausado mientras otro simulador avanzó hasta el tick 260 y guardó
	// la instantánea del tick 250.
	if err := s.EnsureActiveAndFresh(260); !errors.Is(err, ErrNotReady) {
		t.Fatalf("EnsureActiveAndFresh(260) = %v, quiero ErrNotReady", err)
	}
	eventually(t, "recarga tras despausado completada", s.IsReady)

	// El siguiente sondeo del hub sigue trayendo min_expected_tick = 260 hasta
	// que un Step completo devuelva el nuevo boot_id; no debe volver a recargar.
	if err := s.EnsureActiveAndFresh(260); err != nil {
		t.Fatalf("EnsureActiveAndFresh(260) tras recarga = %v", err)
	}
	resumed, err := s.Step(nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.RestoredTick != 250 || resumed.Outcome.Tick != 251 {
		t.Fatalf("restored=%d tick=%d, quiero 250 y 251", resumed.RestoredTick, resumed.Outcome.Tick)
	}
	if resumed.BootID == originalBootID {
		t.Fatalf("boot_id no rotó tras recarga: %q", resumed.BootID)
	}
}

func TestFreshlyLoadedSimulatorAfterCrashAcceptsMinExpectedTickInSameSnapshotWindow(t *testing.T) {
	// Tras un SIGKILL en tick 1410, la última instantánea periódica en PostgreSQL
	// es la del tick 1400 (ventana de 50 ticks). Al reiniciar, el primer Step
	// trae min_expected_tick = 1410 y debe aceptarse sin recargar en bucle.
	st := &fakeStore{
		t:     t,
		loads: []result[*persistencev1.Snapshot]{found(snapshotAt(t, 1400))},
	}
	s := ready(t, st)
	if err := s.EnsureActiveAndFresh(1410); err != nil {
		t.Fatalf("EnsureActiveAndFresh(1410) = %v, quiero nil", err)
	}
	if n := st.loadCalls.Load(); n != 1 {
		t.Fatalf("LoadSnapshot = %d, quiero 1 (sin recarga redundante)", n)
	}
	report, err := s.Step(nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if report.RestoredTick != 1400 || report.Outcome.Tick != 1401 {
		t.Fatalf("restored=%d tick=%d, quiero 1400 y 1401", report.RestoredTick, report.Outcome.Tick)
	}
}

func TestCheckHubGenerationFencesLowerGenerationWithoutTouchingWorld(t *testing.T) {
	s := ready(t, &fakeStore{})
	// hub_generation = 0 (clientes antiguos) no altera ni se rechaza.
	if err := s.CheckHubGeneration(0); err != nil {
		t.Fatalf("CheckHubGeneration(0) = %v", err)
	}
	// Primera generación explícita (1) y luego promoción a 2.
	if err := s.CheckHubGeneration(1); err != nil {
		t.Fatalf("CheckHubGeneration(1) = %v", err)
	}
	if err := s.CheckHubGeneration(1); err != nil {
		t.Fatalf("CheckHubGeneration(1 repetida) = %v", err)
	}
	if err := s.CheckHubGeneration(2); err != nil {
		t.Fatalf("CheckHubGeneration(2) = %v", err)
	}
	// Si el Hub 1 vuelve de un docker pause, su generación 1 es rechazada con ErrFenced.
	if err := s.CheckHubGeneration(1); !errors.Is(err, ErrFenced) {
		t.Fatalf("CheckHubGeneration(1 tras 2) = %v, quiero ErrFenced", err)
	}
	// El Hub 2 sigue avanzando normalmente.
	if err := s.CheckHubGeneration(2); err != nil {
		t.Fatalf("CheckHubGeneration(2) = %v", err)
	}
}

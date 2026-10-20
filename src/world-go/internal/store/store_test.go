package store

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	persistencev1 "github.com/iadev2005/hydrovalley/gen/go/hydrovalley/persistence/v1"
	"google.golang.org/grpc/codes"

	"github.com/iadev2005/hydrovalley/src/world-go/internal/fakepersist"
	"github.com/iadev2005/hydrovalley/src/world-go/internal/requestid"
)

const (
	deadline = 100 * time.Millisecond
	retry    = 5 * time.Millisecond
)

func policy() CallPolicy {
	return CallPolicy{Deadline: deadline, RetryDelays: []time.Duration{retry, retry, retry}}
}

func start(t *testing.T, steps ...fakepersist.Step) (*GRPCStore, *fakepersist.Fake) {
	t.Helper()
	fake := fakepersist.New().Script(steps...)
	fake.SetSnapshot(&persistencev1.Snapshot{Tick: 7})
	store, err := NewGRPC(fakepersist.Serve(t, fake), policy(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store, fake
}

func snapshot() *persistencev1.Snapshot { return &persistencev1.Snapshot{Tick: 50} }

func repeat(step fakepersist.Step, n int) []fakepersist.Step {
	steps := make([]fakepersist.Step, n)
	for i := range steps {
		steps[i] = step
	}
	return steps
}

func total(fake *fakepersist.Fake) int {
	return fake.Count("GetCatalog") + fake.Count("LoadSnapshot") + fake.Count("SaveSnapshot")
}

func TestDefaultPolicyFollowsThePortabilityRule(t *testing.T) {
	got := DefaultCallPolicy()
	want := CallPolicy{
		Deadline:    2 * time.Second,
		RetryDelays: []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond},
	}
	if got.Deadline != want.Deadline || !slices.Equal(got.RetryDelays, want.RetryDelays) {
		t.Fatalf("política por omisión: %+v", got)
	}
}

func TestOnlyTransientCodesAreRetried(t *testing.T) {
	for _, code := range []codes.Code{codes.Unavailable, codes.DeadlineExceeded} {
		if !retryable(code) {
			t.Errorf("%v debería reintentarse", code)
		}
	}
	for _, code := range []codes.Code{codes.OK, codes.NotFound, codes.AlreadyExists, codes.InvalidArgument, codes.Internal, codes.Unimplemented, codes.Canceled} {
		if retryable(code) {
			t.Errorf("%v no debería reintentarse", code)
		}
	}
}

func TestStatusCodesMapToStoreErrors(t *testing.T) {
	cases := map[codes.Code]Kind{
		codes.Unavailable:      KindUnavailable,
		codes.DeadlineExceeded: KindUnavailable,
		codes.AlreadyExists:    KindAlreadyExists,
		codes.Internal:         KindFailed,
		codes.Canceled:         KindFailed,
	}
	for code, want := range cases {
		if got := KindOf(newError(code, "x")); got != want {
			t.Errorf("%v: %v, quiero %v", code, got, want)
		}
	}
	if KindOf(errors.New("otro")) != KindFailed {
		t.Error("un error ajeno es KindFailed")
	}
	if KindOf(nil) != KindNone {
		t.Error("sin error no hay tipo")
	}
}

func TestTransientFailuresAreRetriedUntilSuccess(t *testing.T) {
	store, fake := start(t,
		fakepersist.Step{Code: codes.Unavailable},
		fakepersist.Step{Code: codes.DeadlineExceeded},
		fakepersist.Step{Code: codes.Unavailable},
	)
	loaded, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if loaded.GetTick() != 7 {
		t.Fatalf("tick cargado: %d", loaded.GetTick())
	}
	if n := total(fake); n != 4 {
		t.Fatalf("1 intento + 3 reintentos, hubo %d", n)
	}
}

func TestAtMostThreeRetriesThenUnavailable(t *testing.T) {
	store, fake := start(t, repeat(fakepersist.Step{Code: codes.Unavailable}, 10)...)
	err := store.Save(context.Background(), snapshot())
	if KindOf(err) != KindUnavailable {
		t.Fatalf("error: %v", err)
	}
	if n := total(fake); n != 4 {
		t.Fatalf("1 intento + 3 reintentos, hubo %d", n)
	}
}

func TestNonTransientCodesAreNotRetried(t *testing.T) {
	for code, want := range map[codes.Code]Kind{
		codes.Internal: KindFailed, codes.InvalidArgument: KindFailed,
		codes.AlreadyExists: KindAlreadyExists, codes.Canceled: KindFailed,
	} {
		store, fake := start(t, repeat(fakepersist.Step{Code: code}, 2)...)
		err := store.Save(context.Background(), snapshot())
		if KindOf(err) != want {
			t.Errorf("%v: %v", code, err)
		}
		if n := total(fake); n != 1 {
			t.Errorf("%v no se reintenta, hubo %d llamadas", code, n)
		}
	}
}

func TestNotFoundOnLoadIsAnEmptyStoreWithoutRetry(t *testing.T) {
	store, fake := start(t)
	fake.SetSnapshot(nil)
	loaded, err := store.Load(context.Background())
	if err != nil || loaded != nil {
		t.Fatalf("quiero (nil, nil), obtuve (%v, %v)", loaded, err)
	}
	if n := total(fake); n != 1 {
		t.Fatalf("llamadas: %d", n)
	}
}

func TestASlowAttemptIsCutAtTheDeadlineAndRetried(t *testing.T) {
	store, fake := start(t, fakepersist.Step{Delay: deadline * 10})
	started := time.Now()
	loaded, err := store.Load(context.Background())
	if err != nil || loaded.GetTick() != 7 {
		t.Fatalf("(%v, %v)", loaded, err)
	}
	if n := total(fake); n != 2 {
		t.Fatalf("llamadas: %d", n)
	}
	if elapsed := time.Since(started); elapsed > deadline*5 {
		t.Fatalf("el intento lento no se cortó en su plazo: %v", elapsed)
	}
}

func TestAttemptsThatAlwaysTimeOutEndAsUnavailableAfterThreeRetries(t *testing.T) {
	store, fake := start(t, repeat(fakepersist.Step{Delay: deadline * 10}, 10)...)
	started := time.Now()
	err := store.Save(context.Background(), snapshot())
	if KindOf(err) != KindUnavailable {
		t.Fatalf("error: %v", err)
	}
	if n := total(fake); n != 4 {
		t.Fatalf("1 intento + 3 reintentos, hubo %d", n)
	}
	// 4 plazos y 3 esperas, con holgura; muy por debajo de 4 × 1 s.
	if elapsed := time.Since(started); elapsed > deadline*8 {
		t.Fatalf("cada intento debe cortarse en su plazo: %v", elapsed)
	}
}

func TestACancelledCallerStopsTheRetries(t *testing.T) {
	store, fake := start(t, repeat(fakepersist.Step{Code: codes.Unavailable}, 10)...)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.Save(ctx, snapshot()); err == nil {
		t.Fatal("con el contexto cancelado debe fallar")
	}
	if n := total(fake); n > 1 {
		t.Fatalf("no debe reintentar con el contexto cancelado: %d llamadas", n)
	}
}

func TestSaveSendsTheSnapshot(t *testing.T) {
	store, fake := start(t)
	if err := store.Save(context.Background(), snapshot()); err != nil {
		t.Fatal(err)
	}
	if got := fake.SavedTicks(); !slices.Equal(got, []uint64{50}) {
		t.Fatalf("guardado: %v", got)
	}
}

func TestEveryCallCarriesItsOwnRequestIDAndRetriesKeepIt(t *testing.T) {
	store, fake := start(t, fakepersist.Step{Code: codes.Unavailable}) // el primer intento falla
	ctx := context.Background()
	if _, err := store.GetCatalog(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(ctx, snapshot()); err != nil {
		t.Fatal(err)
	}
	catalog := fake.Calls("GetCatalog")
	if len(catalog) != 2 || catalog[0].RequestID != catalog[1].RequestID {
		t.Fatalf("el reintento debe conservar el id: %+v", catalog)
	}
	seen := map[string]bool{}
	for _, method := range []string{"GetCatalog", "LoadSnapshot", "SaveSnapshot"} {
		calls := fake.Calls(method)
		if len(calls) == 0 {
			t.Fatalf("%s no llegó", method)
		}
		id := calls[0].RequestID
		if !strings.HasPrefix(id, "world-") || !requestid.IsSafe(id) {
			t.Errorf("%s: id %q", method, id)
		}
		if seen[id] {
			t.Errorf("id repetido entre llamadas: %s", id)
		}
		seen[id] = true
	}
}

func TestGetCatalogReturnsTheValidatedCatalog(t *testing.T) {
	store, fake := start(t)
	catalog, err := store.GetCatalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if catalog.Version != fakepersist.CatalogVersion || catalog.Drop.Humidity != 25 ||
		catalog.Tank.Capacity != 20 || catalog.OfflineInstallationSeconds != 7200 {
		t.Fatalf("catálogo: %+v", catalog)
	}
	if n := fake.Count("GetCatalog"); n != 1 {
		t.Fatalf("llamadas: %d", n)
	}
}

func TestGetCatalogRetriesTransientFailuresAndGivesUp(t *testing.T) {
	store, fake := start(t, fakepersist.Step{Code: codes.Unavailable}, fakepersist.Step{Code: codes.DeadlineExceeded})
	if _, err := store.GetCatalog(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := fake.Count("GetCatalog"); n != 3 {
		t.Fatalf("llamadas: %d", n)
	}

	store, fake = start(t, repeat(fakepersist.Step{Code: codes.Unavailable}, 4)...)
	if _, err := store.GetCatalog(context.Background()); KindOf(err) != KindUnavailable {
		t.Fatalf("error: %v", err)
	}
	if n := fake.Count("GetCatalog"); n != 4 {
		t.Fatalf("1 intento + 3 reintentos, hubo %d", n)
	}
}

func TestGetCatalogDoesNotRetryOtherErrors(t *testing.T) {
	store, fake := start(t, fakepersist.Step{Code: codes.Internal})
	if _, err := store.GetCatalog(context.Background()); KindOf(err) != KindFailed {
		t.Fatalf("error: %v", err)
	}
	if n := fake.Count("GetCatalog"); n != 1 {
		t.Fatalf("llamadas: %d", n)
	}
}

func TestGetCatalogRejectsAnUnusableCatalog(t *testing.T) {
	store, fake := start(t)
	bad := fakepersist.Catalog()
	bad.Growth = nil
	fake.SetCatalog(bad)
	_, err := store.GetCatalog(context.Background())
	if KindOf(err) != KindFailed || !strings.Contains(err.Error(), "growth") {
		t.Fatalf("error: %v", err)
	}
	fake.SetCatalog(nil)
	if _, err := store.GetCatalog(context.Background()); KindOf(err) != KindFailed {
		t.Fatalf("sin catálogo: %v", err)
	}
}

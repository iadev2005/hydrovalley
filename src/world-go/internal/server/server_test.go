package server

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	persistencev1 "github.com/iadev2005/hydrovalley/gen/go/hydrovalley/persistence/v1"
	worldv1 "github.com/iadev2005/hydrovalley/gen/go/hydrovalley/world/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"github.com/iadev2005/hydrovalley/src/world-go/internal/fakepersist"
	"github.com/iadev2005/hydrovalley/src/world-go/internal/requestid"
	"github.com/iadev2005/hydrovalley/src/world-go/internal/simulator"
	"github.com/iadev2005/hydrovalley/src/world-go/internal/store"
)

// Portadas de src/world/tests/grpc_server.rs y de las pruebas de
// src/world/src/grpc.rs: el servidor gRPC en el mismo proceso, contra una
// persistencia falsa servida también con gRPC.

const (
	ana  = "6f1c1a52-2d0e-4f4e-9c1e-0c6c2d9b8a01"
	bob  = "0a0a0a0a-2d0e-4f4e-9c1e-0c6c2d9b8a02"
	carl = "ffffffff-2d0e-4f4e-9c1e-0c6c2d9b8a03"
)

// logBuffer recoge lo que escribe el registro.
type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *logBuffer) text() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type harness struct {
	t       *testing.T
	world   worldv1.WorldServiceClient
	health  healthpb.HealthClient
	fake    *fakepersist.Fake
	logs    *logBuffer
	stop    context.CancelFunc
	done    chan error
	stopped bool
}

func start(t *testing.T, fake *fakepersist.Fake, level slog.Level) *harness {
	t.Helper()
	persistenceAddr := fakepersist.Serve(t, fake)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	logs := &logBuffer{}
	settings := Settings{
		PersistenceAddr: persistenceAddr,
		CallPolicy:      store.CallPolicy{Deadline: 500 * time.Millisecond, RetryDelays: []time.Duration{10 * time.Millisecond}},
		LoadBackoff:     simulator.Backoff{Initial: 10 * time.Millisecond, Max: 50 * time.Millisecond},
		ShutdownTimeout: 5 * time.Second,
		Seed:            7,
		Log:             slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: level})),
	}
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, settings, listener) }()

	conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{
		t: t, world: worldv1.NewWorldServiceClient(conn), health: healthpb.NewHealthClient(conn),
		fake: fake, logs: logs, stop: stop, done: done,
	}
	t.Cleanup(func() {
		_ = conn.Close()
		h.shutdown()
	})
	return h
}

// shutdown envía la señal de parada y espera a que Serve termine.
func (h *harness) shutdown() {
	h.t.Helper()
	if h.stopped {
		return
	}
	h.stopped = true
	h.stop()
	select {
	case err := <-h.done:
		if err != nil {
			h.t.Errorf("Serve: %v", err)
		}
	case <-time.After(10 * time.Second):
		h.t.Error("Serve debe terminar")
	}
}

func (h *harness) step(commands ...*worldv1.Command) (*worldv1.StepResponse, error) {
	return h.stepWithID("", commands...)
}

func (h *harness) stepWithID(id string, commands ...*worldv1.Command) (*worldv1.StepResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if id != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, requestid.Key, id)
	}
	return h.world.Step(ctx, &worldv1.StepRequest{Commands: commands})
}

func (h *harness) ranking(limit uint32) (*worldv1.GetRankingResponse, error) {
	return h.rankingWithID("", limit)
}

func (h *harness) rankingWithID(id string, limit uint32) (*worldv1.GetRankingResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if id != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, requestid.Key, id)
	}
	return h.world.GetRanking(ctx, &worldv1.GetRankingRequest{Limit: limit})
}

func (h *harness) healthOf(service string) healthpb.HealthCheckResponse_ServingStatus {
	h.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	response, err := h.health.Check(ctx, &healthpb.HealthCheckRequest{Service: service})
	if err != nil {
		h.t.Fatalf("Health.Check(%q): %v", service, err)
	}
	return response.GetStatus()
}

// waitServing espera a que la salud diga SERVING (el mundo ya está cargado) y
// solo entonces sigue, así las comprobaciones posteriores no compiten con la
// goroutine que la actualiza.
func (h *harness) waitServing() {
	h.t.Helper()
	eventually(h.t, "la salud nunca llegó a SERVING", func() bool {
		return h.healthOf(WorldServiceName) == healthpb.HealthCheckResponse_SERVING &&
			h.healthOf("") == healthpb.HealthCheckResponse_SERVING
	})
}

func (h *harness) firstStep() *worldv1.StepResponse {
	h.t.Helper()
	h.waitServing()
	response, err := h.step()
	if err != nil {
		h.t.Fatalf("Step debe responder con la salud en SERVING: %v", err)
	}
	return response
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

func dropCommand(player string, n uint32, x, y uint32) *worldv1.Command {
	return &worldv1.Command{
		ActionId: fmt.Sprintf("00000000-0000-4000-8000-%012d", n),
		PlayerId: player,
		Kind:     &worldv1.Command_Drop{Drop: &worldv1.DropCommand{X: x, Y: y}},
	}
}

func dropsBy(player string, first, count, x, y uint32) []*worldv1.Command {
	var commands []*worldv1.Command
	for n := first; n < first+count; n++ {
		commands = append(commands, dropCommand(player, n, x, y))
	}
	return commands
}

func adminCommand(n uint32, kind any) *worldv1.Command {
	command := &worldv1.Command{ActionId: fmt.Sprintf("00000000-0000-4000-8000-%012d", n), PlayerId: ana}
	switch kind := kind.(type) {
	case *worldv1.Command_SetDemoMode:
		command.Kind = kind
	case *worldv1.Command_ResetValley:
		command.Kind = kind
	}
	return command
}

func TestStartsEmptyAtTickZeroWhenThereIsNoSnapshot(t *testing.T) {
	h := start(t, fakepersist.New(), slog.LevelInfo)
	response := h.firstStep()
	if response.GetTick() != 1 || response.GetRestoredTick() != 0 || len(response.GetResults()) != 0 {
		t.Fatalf("respuesta: %v", response)
	}
	state := response.GetState()
	if state.GetWidth() != 32 || state.GetHeight() != 32 || len(state.GetTiles()) != 1024 || len(state.GetPlayers()) != 0 {
		t.Fatalf("estado: %dx%d, %d casillas, %d jugadores", state.GetWidth(), state.GetHeight(), len(state.GetTiles()), len(state.GetPlayers()))
	}
	if h.healthOf(WorldServiceName) != healthpb.HealthCheckResponse_SERVING || h.healthOf("") != healthpb.HealthCheckResponse_SERVING {
		t.Fatal("la salud debe estar en SERVING")
	}
}

func TestStepAppliesADrop(t *testing.T) {
	h := start(t, fakepersist.New(), slog.LevelInfo)
	h.firstStep()
	response, err := h.step(dropCommand(ana, 1, 14, 9), dropCommand(ana, 2, 32, 0))
	if err != nil {
		t.Fatal(err)
	}
	if response.GetTick() != 2 {
		t.Fatalf("tick %d", response.GetTick())
	}
	got := []string{}
	for _, result := range response.GetResults() {
		got = append(got, result.GetStatus().String()+"/"+result.GetReason().String())
	}
	want := []string{"COMMAND_STATUS_APPLIED/REJECT_REASON_UNSPECIFIED", "COMMAND_STATUS_REJECTED/REJECT_REASON_OUT_OF_BOUNDS"}
	if !slices.Equal(got, want) {
		t.Fatalf("resultados: %v", got)
	}
	state := response.GetState()
	if state.GetTiles()[9*32+14].GetHumidity() != 25 || state.GetHumidityContributed() != 25 ||
		len(state.GetPlayers()) != 1 || state.GetPlayers()[0].GetWater() != 19 {
		t.Fatalf("estado: %v", state)
	}
	again, err := h.step(dropCommand(ana, 1, 14, 9))
	if err != nil || again.GetResults()[0].GetStatus() != worldv1.CommandStatus_COMMAND_STATUS_DUPLICATE {
		t.Fatalf("(%v, %v)", again, err)
	}
}

func TestBootIDIsAUUIDAndStableAcrossSteps(t *testing.T) {
	h := start(t, fakepersist.New(), slog.LevelInfo)
	first := h.firstStep()
	second, err := h.step()
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[0-9a-f-]{36}$`).MatchString(first.GetBootId()) {
		t.Fatalf("boot_id: %q", first.GetBootId())
	}
	if first.GetBootId() != second.GetBootId() || second.GetRestoredTick() != first.GetRestoredTick() {
		t.Fatalf("boot_id o restored_tick cambiaron: %v / %v", first, second)
	}
}

// RNF-07: cada llamada del simulador a la persistencia lleva su propio
// x-request-id (world-<uuid>), para correlacionar los dos registros.
func TestEveryPersistenceCallCarriesItsOwnRequestID(t *testing.T) {
	fake := fakepersist.New()
	h := start(t, fake, slog.LevelInfo)
	h.firstStep()
	if _, err := h.step(dropCommand(ana, 1, 0, 0)); err != nil {
		t.Fatal(err)
	}
	for range 48 { // hasta el tick 50
		if _, err := h.step(); err != nil {
			t.Fatal(err)
		}
	}
	eventually(t, "SaveSnapshot del tick 50", func() bool { return fake.Count("SaveSnapshot") > 0 })

	seen := map[string]bool{}
	for _, method := range []string{"GetCatalog", "LoadSnapshot", "SaveSnapshot"} {
		calls := fake.Calls(method)
		if len(calls) == 0 {
			t.Fatalf("%s no llegó", method)
		}
		for _, call := range calls {
			if !strings.HasPrefix(call.RequestID, "world-") || !requestid.IsSafe(call.RequestID) {
				t.Errorf("%s: id %q", method, call.RequestID)
			}
			if seen[call.RequestID] {
				t.Errorf("id repetido: %s", call.RequestID)
			}
			seen[call.RequestID] = true
		}
	}
}

func decodeInner(t *testing.T, snapshot *persistencev1.Snapshot) *worldv1.WorldSnapshot {
	t.Helper()
	inner := &worldv1.WorldSnapshot{}
	if err := proto.Unmarshal(snapshot.GetWorldState(), inner); err != nil {
		t.Fatal(err)
	}
	return inner
}

func TestASnapshotIsSavedAtTick50(t *testing.T) {
	fake := fakepersist.New()
	h := start(t, fake, slog.LevelInfo)
	h.firstStep()
	if _, err := h.step(dropCommand(ana, 1, 0, 0)); err != nil {
		t.Fatal(err)
	}
	for range 47 { // ticks 3 a 49
		if _, err := h.step(); err != nil {
			t.Fatal(err)
		}
	}
	if len(fake.SavedTicks()) != 0 {
		t.Fatal("aún no debe haber guardados")
	}
	response, err := h.step()
	if err != nil || response.GetTick() != 50 {
		t.Fatalf("(%v, %v)", response, err)
	}
	eventually(t, "SaveSnapshot del tick 50", func() bool { return slices.Equal(fake.SavedTicks(), []uint64{50}) })

	saved := fake.Saved()[0]
	inner := decodeInner(t, saved)
	if saved.GetEpoch() != 0 || saved.GetCatalogVersion() != fakepersist.CatalogVersion || len(saved.GetPlayers()) != 1 ||
		inner.GetTick() != 50 || inner.GetCatalogVersion() != fakepersist.CatalogVersion ||
		inner.GetHumidityContributed() != 25 || len(inner.GetPlayers()[0].GetRecentActions()) != 1 {
		t.Fatalf("instantánea: %v / %v", saved, inner)
	}
}

func TestRestoresTheLatestSnapshotAtStartup(t *testing.T) {
	firstFake := fakepersist.New()
	first := start(t, firstFake, slog.LevelInfo)
	first.firstStep()
	if _, err := first.step(dropCommand(ana, 1, 3, 3)); err != nil {
		t.Fatal(err)
	}
	var last *worldv1.StepResponse
	for range 48 {
		var err error
		if last, err = first.step(); err != nil {
			t.Fatal(err)
		}
	}
	eventually(t, "SaveSnapshot del tick 50", func() bool { return slices.Equal(firstFake.SavedTicks(), []uint64{50}) })
	snapshot := firstFake.Saved()[0]

	secondFake := fakepersist.New()
	secondFake.SetSnapshot(snapshot)
	second := start(t, secondFake, slog.LevelInfo)
	response := second.firstStep()
	if response.GetTick() != 51 || response.GetRestoredTick() != 50 || response.GetBootId() == last.GetBootId() {
		t.Fatalf("tick %d, restored %d, boot_id %s (primero: %s)", response.GetTick(), response.GetRestoredTick(), response.GetBootId(), last.GetBootId())
	}
	// 25 de la gota menos 5 evaporaciones de 2 (ticks 10, 20, 30, 40 y 50).
	if humidity := response.GetState().GetTiles()[3*32+3].GetHumidity(); humidity != 15 {
		t.Fatalf("humedad %d", humidity)
	}
	again, err := second.step(dropCommand(ana, 1, 3, 3))
	if err != nil || again.GetResults()[0].GetStatus() != worldv1.CommandStatus_COMMAND_STATUS_DUPLICATE {
		t.Fatalf("(%v, %v)", again, err)
	}
}

func TestStepIsUnavailableUntilPersistenceAnswersThenRecovers(t *testing.T) {
	fake := fakepersist.New()
	fake.SetLoadUnavailable(true)
	h := start(t, fake, slog.LevelInfo)
	eventually(t, "varios LoadSnapshot", func() bool { return fake.Count("LoadSnapshot") >= 3 })

	if _, err := h.step(); status.Code(err) != codes.Unavailable {
		t.Fatalf("Step: %v", err)
	}
	if h.healthOf(WorldServiceName) != healthpb.HealthCheckResponse_NOT_SERVING || h.healthOf("") != healthpb.HealthCheckResponse_NOT_SERVING {
		t.Fatal("la salud debe estar en NOT_SERVING")
	}

	fake.SetLoadUnavailable(false)
	response := h.firstStep()
	if response.GetTick() != 1 || response.GetRestoredTick() != 0 || h.healthOf(WorldServiceName) != healthpb.HealthCheckResponse_SERVING {
		t.Fatalf("respuesta: %v", response)
	}
}

func TestShutdownSavesAFinalSnapshotOfTheLastTick(t *testing.T) {
	fake := fakepersist.New()
	h := start(t, fake, slog.LevelInfo)
	h.firstStep()                                                // tick 1
	if _, err := h.step(dropCommand(ana, 1, 2, 2)); err != nil { // tick 2
		t.Fatal(err)
	}
	if _, err := h.step(); err != nil { // tick 3
		t.Fatal(err)
	}
	if len(fake.SavedTicks()) != 0 {
		t.Fatal("aún no debe haber guardados")
	}
	h.shutdown()
	if got := fake.SavedTicks(); !slices.Equal(got, []uint64{3}) {
		t.Fatalf("guardados: %v", got)
	}
	if inner := decodeInner(t, fake.Saved()[0]); inner.GetHumidityContributed() != 25 {
		t.Fatalf("humedad aportada: %d", inner.GetHumidityContributed())
	}
}

func TestTheWorldIsNotReadyUntilTheCatalogIsAvailable(t *testing.T) {
	fake := fakepersist.New()
	fake.SetCatalogUnavailable(true)
	h := start(t, fake, slog.LevelInfo)
	eventually(t, "varios GetCatalog", func() bool { return fake.Count("GetCatalog") >= 3 })

	if _, err := h.step(); status.Code(err) != codes.Unavailable {
		t.Fatalf("Step: %v", err)
	}
	if h.healthOf("") != healthpb.HealthCheckResponse_NOT_SERVING {
		t.Fatal("la salud debe estar en NOT_SERVING")
	}
	// Sin catálogo no se toca la instantánea: el tick nunca retrocede.
	if n := fake.Count("LoadSnapshot"); n != 0 {
		t.Fatalf("LoadSnapshot se llamó %d veces sin catálogo", n)
	}

	fake.SetCatalogUnavailable(false)
	response := h.firstStep()
	if response.GetTick() != 1 || fake.Count("LoadSnapshot") != 1 {
		t.Fatalf("tick %d, LoadSnapshot %d", response.GetTick(), fake.Count("LoadSnapshot"))
	}
}

func TestAdminCommandsSwitchDemoModeAndResetTheValleyRefetchingTheCatalog(t *testing.T) {
	fake := fakepersist.New()
	h := start(t, fake, slog.LevelInfo)
	h.firstStep()
	if n := fake.Count("GetCatalog"); n != 1 {
		t.Fatalf("GetCatalog: %d", n)
	}

	demo, err := h.step(
		adminCommand(1, &worldv1.Command_SetDemoMode{SetDemoMode: &worldv1.SetDemoModeCommand{Enabled: true}}),
		dropCommand(ana, 2, 3, 3),
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range demo.GetResults() {
		if result.GetStatus() != worldv1.CommandStatus_COMMAND_STATUS_APPLIED {
			t.Fatalf("resultado: %v", result)
		}
	}
	state := demo.GetState()
	if !state.GetDemoMode() || state.GetTiles()[3*32+3].GetHumidity() != 1 || state.GetHumidityContributed() != 1 {
		t.Fatalf("estado en modo demo: %v", state)
	}
	if n := fake.Count("GetCatalog"); n != 1 {
		t.Fatalf("sin reinicio no se pide el catálogo: %d", n)
	}

	reset, err := h.step(adminCommand(3, &worldv1.Command_ResetValley{ResetValley: &worldv1.ResetValleyCommand{}}))
	if err != nil {
		t.Fatal(err)
	}
	if reset.GetResults()[0].GetStatus() != worldv1.CommandStatus_COMMAND_STATUS_APPLIED || reset.GetTick() != demo.GetTick()+1 {
		t.Fatalf("reinicio: %v (el tick sigue creciendo)", reset)
	}
	state = reset.GetState()
	if state.GetEpoch() != 1 || state.GetTiles()[3*32+3].GetHumidity() != 0 {
		t.Fatalf("estado tras el reinicio: epoch %d", state.GetEpoch())
	}
	if n := fake.Count("GetCatalog"); n != 2 {
		t.Fatalf("el reinicio pide el catálogo otra vez: %d", n)
	}
}

func TestRankingIsUnavailableUntilTheWorldIsLoaded(t *testing.T) {
	fake := fakepersist.New()
	fake.SetLoadUnavailable(true)
	h := start(t, fake, slog.LevelInfo)
	if _, err := h.ranking(0); status.Code(err) != codes.Unavailable {
		t.Fatalf("GetRanking: %v", err)
	}
}

func TestRankingOfAnEmptyValleyIsEmptyAndCarriesTheTick(t *testing.T) {
	h := start(t, fakepersist.New(), slog.LevelInfo)
	h.firstStep()
	response, err := h.ranking(0)
	if err != nil || response.GetTick() != 1 || len(response.GetEntries()) != 0 {
		t.Fatalf("(%v, %v)", response, err)
	}
}

type row struct {
	rank   uint32
	player string
	leaves uint64
}

func rows(response *worldv1.GetRankingResponse) []row {
	var out []row
	for _, e := range response.GetEntries() {
		out = append(out, row{e.GetRank(), e.GetPlayerId(), e.GetLeavesEarned()})
	}
	return out
}

func TestRankingOrdersByLeavesEarnedWithTiesSharingTheRank(t *testing.T) {
	h := start(t, fakepersist.New(), slog.LevelInfo)
	h.firstStep()
	commands := dropsBy(ana, 1, 8, 0, 0) // 4 gotas: pasto (+1 hoja), 8: arbusto (+2)
	commands = append(commands, dropsBy(bob, 100, 4, 5, 5)...)
	commands = append(commands, dropsBy(carl, 200, 1, 9, 9)...)
	if _, err := h.step(commands...); err != nil {
		t.Fatal(err)
	}
	response, err := h.ranking(0)
	if err != nil || response.GetTick() != 2 {
		t.Fatalf("(%v, %v)", response, err)
	}
	if got, want := rows(response), []row{{1, ana, 3}, {2, bob, 1}, {3, carl, 0}}; !slices.Equal(got, want) {
		t.Fatalf("ranking: %v", got)
	}
}

func TestRankingAppliesTheLimitAndDoesNotMutateTheWorld(t *testing.T) {
	h := start(t, fakepersist.New(), slog.LevelInfo)
	h.firstStep()
	commands := dropsBy(ana, 1, 4, 0, 0)
	commands = append(commands, dropsBy(bob, 100, 4, 5, 5)...)
	commands = append(commands, dropsBy(carl, 200, 1, 9, 9)...)
	if _, err := h.step(commands...); err != nil {
		t.Fatal(err)
	}
	top, err := h.ranking(2)
	if err != nil {
		t.Fatal(err)
	}
	// Empate ana/bob con 1 hoja: ambos primeros; manda el player_id (bob < ana).
	if got, want := rows(top), []row{{1, bob, 1}, {1, ana, 1}}; !slices.Equal(got, want) {
		t.Fatalf("ranking: %v", got)
	}
	// Leer el ranking no avanza el tick.
	if again, _ := h.ranking(0); again.GetTick() != top.GetTick() {
		t.Fatalf("el ranking avanzó el tick: %d", again.GetTick())
	}
	if next, _ := h.step(); next.GetTick() != top.GetTick()+1 {
		t.Fatalf("tick %d", next.GetTick())
	}
}

func TestStepLogsTheRequestIDAndEachActionAtDebug(t *testing.T) {
	h := start(t, fakepersist.New(), slog.LevelDebug)
	h.firstStep()
	if _, err := h.stepWithID("tick-5", dropCommand(ana, 1, 0, 0)); err != nil {
		t.Fatal(err)
	}
	text := h.logs.text()
	for _, want := range []string{"request_id=tick-5", "00000000-0000-4000-8000-000000000001", "APPLIED"} {
		if !strings.Contains(text, want) {
			t.Errorf("falta %q en el registro:\n%s", want, text)
		}
	}
}

func TestStepWithoutAUsableIDLogsADash(t *testing.T) {
	h := start(t, fakepersist.New(), slog.LevelDebug)
	h.firstStep()
	if _, err := h.stepWithID("", dropCommand(ana, 1, 0, 0)); err != nil {
		t.Fatal(err)
	}
	// Un id con espacio no pasa el filtro de seguridad y no se escribe.
	if _, err := h.stepWithID("con espacio", dropCommand(ana, 2, 0, 0)); err != nil {
		t.Fatal(err)
	}
	text := h.logs.text()
	if strings.Count(text, "request_id=-") < 2 || strings.Contains(text, "con espacio") {
		t.Fatalf("registro:\n%s", text)
	}
}

func TestStepLogsNothingPerCommandAtInfo(t *testing.T) {
	h := start(t, fakepersist.New(), slog.LevelInfo)
	h.firstStep()
	before := h.logs.text()
	if _, err := h.stepWithID("tick-6", dropCommand(ana, 1, 0, 0)); err != nil {
		t.Fatal(err)
	}
	if after := h.logs.text(); after != before {
		t.Fatalf("el camino caliente no registra a nivel info:\n%s", strings.TrimPrefix(after, before))
	}
}

func TestGetRankingLogsTheRequestIDAtInfo(t *testing.T) {
	h := start(t, fakepersist.New(), slog.LevelInfo)
	h.firstStep()
	id := "3f2b8c1e-5a47-4d0e-9b6a-1c2d3e4f5a6b"
	if _, err := h.rankingWithID(id, 5); err != nil {
		t.Fatal(err)
	}
	var line string
	for _, l := range strings.Split(h.logs.text(), "\n") {
		if strings.Contains(l, "GetRanking") {
			line = l
		}
	}
	if !strings.Contains(line, "request_id="+id) {
		t.Fatalf("la línea de GetRanking debe llevar el id: %q", line)
	}
}

package load

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/iadev2005/hydrovalley/src/bots/internal/client"
)

// valley simula el hub y el mundo: /play con token "admin" o "bot", estado
// cada 15 ms con los contadores del modo demo y un resultado por acción.
type valley struct {
	mu       sync.Mutex
	demo     bool
	humidity uint64
	accepted uint64
	drops    int
	cmds     []string
	tick     uint64

	// perDrop es la humedad que suma cada gota (1 en modo demo).
	perDrop uint64
	// silent no responde a las gotas; denyAdmin rechaza los comandos de
	// administración con `forbidden`.
	silent    bool
	denyAdmin bool
}

func newValley(t *testing.T) (*valley, *httptest.Server) {
	t.Helper()
	v := &valley{perDrop: 1}
	srv := httptest.NewServer(http.HandlerFunc(v.serve))
	t.Cleanup(srv.Close)
	return v, srv
}

func (v *valley) stateFrame() []byte {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.tick++
	return fmt.Appendf(nil, `{"type":"state","tick":"%d","demo_mode":%t,"width":32,"height":32,"humidity_contributed":"%d","accepted_drops":"%d"}`,
		v.tick, v.demo, v.humidity, v.accepted)
}

func (v *valley) serve(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer c.CloseNow()
	ctx := r.Context()
	go func() {
		for {
			if c.Write(ctx, websocket.MessageText, v.stateFrame()) != nil {
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(15 * time.Millisecond):
			}
		}
	}()
	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			return
		}
		var f struct {
			Type     string `json:"type"`
			ActionID string `json:"action_id"`
			Enabled  bool   `json:"enabled"`
		}
		_ = json.Unmarshal(data, &f)
		status := v.apply(f.Type, f.Enabled)
		if status == "" {
			continue
		}
		_ = c.Write(ctx, websocket.MessageText, []byte(`{"type":"result","action_id":"`+f.ActionID+`","status":"`+status+`"}`))
	}
}

// apply aplica un comando y devuelve el estado a responder ("" = callar).
func (v *valley) apply(kind string, enabled bool) string {
	v.mu.Lock()
	defer v.mu.Unlock()
	switch kind {
	case "drop":
		if v.silent {
			return ""
		}
		v.drops++
		v.accepted++
		v.humidity += v.perDrop
	case "set_demo_mode":
		v.cmds = append(v.cmds, fmt.Sprintf("demo:%t", enabled))
		if v.denyAdmin {
			return "forbidden"
		}
		v.demo = enabled
	case "reset_valley":
		v.cmds = append(v.cmds, "reset")
		if v.denyAdmin {
			return "forbidden"
		}
		v.humidity, v.accepted = 0, 0
	}
	return "applied"
}

func (v *valley) commands() []string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return slices.Clone(v.cmds)
}

func (v *valley) dropCount() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.drops
}

func newSession(t *testing.T, srv *httptest.Server, token string) *client.Session {
	t.Helper()
	s, err := client.NewSession(client.SessionConfig{BaseURL: srv.URL, Token: token})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func fastConfig() Config {
	return Config{
		Duration: 300 * time.Millisecond, Interval: 20 * time.Millisecond, Jitter: 5 * time.Millisecond,
		Drain: 300 * time.Millisecond, Settle: 60 * time.Millisecond,
		ConnectTimeout: 2 * time.Second, StepTimeout: 2 * time.Second, Reset: true, Seed: 7,
	}
}

func run(t *testing.T, srv *httptest.Server, cfg Config, bots int) (Report, error) {
	t.Helper()
	var sessions []Session
	for range bots {
		sessions = append(sessions, newSession(t, srv, "bot"))
	}
	return Run(context.Background(), cfg, newSession(t, srv, "admin"), sessions, nil)
}

func TestRunMeasuresAndRestoresTheValley(t *testing.T) {
	v, srv := newValley(t)
	rep, err := run(t, srv, fastConfig(), 3)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Bots != 3 || rep.Sent < 20 || rep.Sent != v.dropCount() {
		t.Fatalf("sent %d, valley saw %d: %+v", rep.Sent, v.dropCount(), rep)
	}
	if rep.Results["applied"] != rep.Sent || rep.Unresolved != 0 || rep.BotsApplied != rep.Sent {
		t.Fatalf("every drop must be applied: %+v", rep)
	}
	if rep.AcceptedDrops != uint64(rep.Sent) || rep.HumidityContributed != uint64(rep.Sent) || !rep.InvariantOK {
		t.Fatalf("invariant: %+v", rep)
	}
	if rep.LatencyMS.Max <= 0 || rep.DurationS < 0.29 || rep.DurationS > 0.6 {
		t.Fatalf("latency %+v, window %v s", rep.LatencyMS, rep.DurationS)
	}
	if want := []string{"demo:true", "demo:false", "reset"}; !slices.Equal(v.commands(), want) {
		t.Fatalf("admin commands = %v, want %v", v.commands(), want)
	}
}

func TestRunWithoutResetStillTurnsDemoModeOff(t *testing.T) {
	v, srv := newValley(t)
	cfg := fastConfig()
	cfg.Reset = false
	if _, err := run(t, srv, cfg, 1); err != nil {
		t.Fatal(err)
	}
	if want := []string{"demo:true", "demo:false"}; !slices.Equal(v.commands(), want) {
		t.Fatalf("admin commands = %v, want %v", v.commands(), want)
	}
}

func TestRunCountsDropsWithoutAResultAsUnresolved(t *testing.T) {
	v, srv := newValley(t)
	v.silent = true
	cfg := fastConfig()
	cfg.Drain = 100 * time.Millisecond
	rep, err := run(t, srv, cfg, 2)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Sent == 0 || rep.Unresolved != rep.Sent || len(rep.Results) != 0 {
		t.Fatalf("every silent drop is unresolved: %+v", rep)
	}
	if rep.RNF02OK || rep.Passed() {
		t.Fatalf("a run with no answers must fail: %+v", rep)
	}
}

func TestRunReportsAnInvariantThatDoesNotHold(t *testing.T) {
	v, srv := newValley(t)
	v.perDrop = 2
	rep, err := run(t, srv, fastConfig(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if rep.InvariantOK || rep.HumidityContributed != 2*rep.AcceptedDrops {
		t.Fatalf("humidity must be double the drops: %+v", rep)
	}
}

func TestRunStopsIfTheAdminCannotEnableDemoMode(t *testing.T) {
	v, srv := newValley(t)
	v.denyAdmin = true
	_, err := run(t, srv, fastConfig(), 2)
	if err == nil {
		t.Fatal("want an error when set_demo_mode is forbidden")
	}
	if v.dropCount() != 0 {
		t.Fatalf("no drops may be sent outside demo mode, got %d", v.dropCount())
	}
	if want := []string{"demo:true"}; !slices.Equal(v.commands(), want) {
		t.Fatalf("nothing to restore: commands = %v", v.commands())
	}
}

func TestRunRestoresTheValleyWhenInterrupted(t *testing.T) {
	v, srv := newValley(t)
	ctx, cancel := context.WithCancel(context.Background())
	cfg := fastConfig()
	cfg.Duration = 5 * time.Second
	time.AfterFunc(200*time.Millisecond, cancel)
	var sessions []Session
	for range 2 {
		sessions = append(sessions, newSession(t, srv, "bot"))
	}
	start := time.Now()
	rep, err := Run(ctx, cfg, newSession(t, srv, "admin"), sessions, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if time.Since(start) > 3*time.Second || rep.Sent == 0 {
		t.Fatalf("interrupt must stop sending early, took %v, sent %d", time.Since(start), rep.Sent)
	}
	if want := []string{"demo:true", "demo:false", "reset"}; !slices.Equal(v.commands(), want) {
		t.Fatalf("an interrupted run must still restore the valley: %v", v.commands())
	}
}

func TestRunFailsWhenTheAdminNeverConnects(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	cfg := fastConfig()
	cfg.ConnectTimeout = 200 * time.Millisecond
	if _, err := run(t, srv, cfg, 1); err == nil {
		t.Fatal("want an error when the admin session cannot connect")
	}
}

type fakeRankingClient struct {
	mu    sync.Mutex
	calls int
}

func (f *fakeRankingClient) GetRanking(_ context.Context, _ string, _ int) (client.RankingResponse, time.Duration, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return client.RankingResponse{Tick: "10"}, 5 * time.Millisecond, nil
}

func TestRunWithRankingClient(t *testing.T) {
	_, srv := newValley(t)
	cfg := fastConfig()
	fakeRC := &fakeRankingClient{}
	cfg.RankingClient = fakeRC
	cfg.RankingToken = "test-token"
	// Con 10 ms salen unas 30 consultas en 300 ms: de sobra para el mínimo de RNF-03.
	cfg.RankingInterval = 10 * time.Millisecond
	cfg.BaselineP95 = 50 * time.Millisecond

	rep, err := run(t, srv, cfg, 2)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Ranking == nil {
		t.Fatal("expected rep.Ranking to be non-nil")
	}
	if rep.Ranking.QueriesSent == 0 || rep.Ranking.QueriesOK == 0 {
		t.Errorf("expected ranking queries to be executed: %+v", rep.Ranking)
	}
	if !rep.Ranking.RNF03OK {
		t.Errorf("expected RNF03OK to be true: %+v", rep.Ranking)
	}
}

// blockingRankingClient responde bien las dos primeras consultas y deja la
// tercera esperando hasta que se cancele el contexto, como una petición en
// vuelo cuando termina la ventana de envío.
type blockingRankingClient struct {
	mu    sync.Mutex
	calls int
	// started avisa de que la consulta en vuelo ya espera.
	started chan struct{}
}

func (b *blockingRankingClient) GetRanking(ctx context.Context, _ string, _ int) (client.RankingResponse, time.Duration, error) {
	b.mu.Lock()
	b.calls++
	n := b.calls
	b.mu.Unlock()
	if n <= 2 {
		return client.RankingResponse{}, 5 * time.Millisecond, nil
	}
	close(b.started)
	<-ctx.Done()
	return client.RankingResponse{}, 0, fmt.Errorf("get ranking: %w", ctx.Err())
}

func TestRankingLoopDoesNotCountTheQueryCancelledByTheEndOfTheWindowAsAnError(t *testing.T) {
	bc := &blockingRankingClient{started: make(chan struct{})}
	cfg := Config{RankingClient: bc, RankingToken: "t", RankingInterval: 5 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan *RankingMeasurement)
	go func() { done <- runRankingLoop(ctx, cfg, slog.New(slog.DiscardHandler)) }()
	<-bc.started
	cancel()
	rm := <-done
	if rm.Errors != 0 {
		t.Errorf("la consulta cancelada por el fin de la ventana no es un error: %+v", rm)
	}
	if rm.Sent != 2 || rm.Success != 2 || len(rm.Latencies) != 2 {
		t.Errorf("solo cuentan las consultas completadas (2): %+v", rm)
	}
}

type failingRankingClient struct{}

func (failingRankingClient) GetRanking(context.Context, string, int) (client.RankingResponse, time.Duration, error) {
	return client.RankingResponse{}, time.Millisecond, &client.HTTPError{Status: http.StatusTooManyRequests, Message: "slow down"}
}

func TestRankingLoopCountsRealFailuresAsErrors(t *testing.T) {
	cfg := Config{RankingClient: failingRankingClient{}, RankingToken: "t", RankingInterval: 5 * time.Millisecond}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	rm := runRankingLoop(ctx, cfg, slog.New(slog.DiscardHandler))
	if rm.Errors == 0 || rm.Success != 0 || rm.Errors != rm.Sent {
		t.Errorf("los fallos reales (429) cuentan como errores: %+v", rm)
	}
}

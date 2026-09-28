package transport

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	worldv1 "github.com/iadev2005/hydrovalley/gen/go/hydrovalley/world/v1"
	"github.com/iadev2005/hydrovalley/src/hub/internal/hub"
)

const (
	ana      = "11111111-1111-4111-8111-111111111111"
	actionID = "0b7c9a52-5f3e-4f5b-9d7e-2a1f3c4d5e6f"
)

// applyAll aplica todos los comandos; fail simula un simulador caído.
type applyAll struct {
	mu   sync.Mutex
	tick uint64
	fail bool
}

func (w *applyAll) Step(_ context.Context, req *worldv1.StepRequest) (*worldv1.StepResponse, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.fail {
		return nil, errors.New("down")
	}
	w.tick++
	resp := &worldv1.StepResponse{Tick: w.tick, BootId: "boot", State: &worldv1.WorldState{
		Width: 1, Height: 1, Tiles: []*worldv1.Tile{{Stage: worldv1.TileStage_TILE_STAGE_DRY_SOIL}},
		Players: []*worldv1.PlayerState{{PlayerId: ana, Water: 19, TankCapacity: 20}},
	}}
	for _, c := range req.GetCommands() {
		resp.Results = append(resp.Results, &worldv1.CommandResult{
			ActionId: c.GetActionId(), PlayerId: c.GetPlayerId(), Status: worldv1.CommandStatus_COMMAND_STATUS_APPLIED,
		})
	}
	return resp, nil
}

type env struct {
	t      *testing.T
	hub    *hub.Hub
	world  *applyAll
	server *httptest.Server
	cancel context.CancelFunc
}

func newEnv(t *testing.T, originPatterns ...string) *env {
	t.Helper()
	world := &applyAll{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := hub.New(world, hub.SystemClock{}, hub.DefaultConfig(), logger)
	ctx, cancel := context.WithCancel(context.Background())
	srv := httptest.NewServer(NewHandler(ctx, h, logger, Options{OriginPatterns: originPatterns}))
	t.Cleanup(func() {
		cancel()
		srv.Close()
	})
	return &env{t: t, hub: h, world: world, server: srv, cancel: cancel}
}

func (e *env) get(path string) *http.Response {
	e.t.Helper()
	resp, err := http.Get(e.server.URL + path)
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func (e *env) dial(query string) *websocket.Conn {
	e.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	url := "ws" + strings.TrimPrefix(e.server.URL, "http") + "/play?" + query
	conn, resp, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		e.t.Fatalf("dial %s: %v", url, err)
	}
	if resp.Body != nil {
		resp.Body.Close()
	}
	e.t.Cleanup(func() { conn.CloseNow() })
	return conn
}

func (e *env) tick() { e.hub.Tick(context.Background()) }

// waitQueue espera a que la conexión haya encolado n comandos.
func (e *env) waitQueue(n int) {
	e.t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for e.hub.Status().QueueLen != n {
		if time.Now().After(deadline) {
			e.t.Fatalf("queue length = %d, want %d", e.hub.Status().QueueLen, n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func send(t *testing.T, conn *websocket.Conn, frame string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText, []byte(frame)); err != nil {
		t.Fatal(err)
	}
}

// next lee el siguiente mensaje cuyo type sea want, saltando los demás.
func next(t *testing.T, conn *websocket.Conn, want string) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("waiting for %q: %v", want, err)
		}
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatalf("invalid JSON %s", data)
		}
		if m["type"] == want {
			return m
		}
	}
}

func TestHealthz(t *testing.T) {
	e := newEnv(t)
	if resp := e.get("/healthz"); resp.StatusCode != http.StatusOK {
		t.Fatalf("/healthz = %d", resp.StatusCode)
	}
}

func TestReadyzFollowsTheWorld(t *testing.T) {
	e := newEnv(t)
	if resp := e.get("/readyz"); resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("/readyz before any Step = %d, want 503", resp.StatusCode)
	}
	e.tick()
	resp := e.get("/readyz")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/readyz after a successful Step = %d, want 200", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil || body["breaker"] != "closed" || body["ready"] != true || body["paused"] != false {
		t.Fatalf("/readyz body = %v (%v)", body, err)
	}
	e.world.fail = true
	e.tick()
	resp = e.get("/readyz")
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("/readyz after a failed Step = %d, want 503", resp.StatusCode)
	}
	body = nil
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil || body["ready"] != false || body["paused"] != true {
		t.Fatalf("/readyz body after a failed Step = %v (%v)", body, err)
	}
}

func TestPlayRejectsInvalidQuery(t *testing.T) {
	e := newEnv(t)
	for _, q := range []string{"", "player_id=", "player_id=ana", "player_id=" + ana + "&map=maybe"} {
		if resp := e.get("/play?" + q); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("/play?%s = %d, want 400", q, resp.StatusCode)
		}
	}
}

func TestDropEndToEnd(t *testing.T) {
	e := newEnv(t)
	conn := e.dial("player_id=" + ana)
	send(t, conn, `{"type":"drop","action_id":"`+actionID+`","x":0,"y":0}`)
	e.waitQueue(1)
	e.tick()

	result := next(t, conn, "result")
	if result["action_id"] != actionID || result["status"] != "applied" {
		t.Fatalf("result = %v", result)
	}
	// Reenvío de una acción confirmada: "duplicate" al instante.
	send(t, conn, `{"type":"drop","action_id":"`+actionID+`","x":0,"y":0}`)
	if dup := next(t, conn, "result"); dup["status"] != "duplicate" {
		t.Fatalf("resend = %v", dup)
	}
}

func TestStateBroadcastWithAndWithoutMap(t *testing.T) {
	e := newEnv(t)
	withMap := e.dial("player_id=" + ana)
	noMap := e.dial("player_id=" + ana + "&map=false")
	// Las dos conexiones deben estar registradas antes del tick.
	deadline := time.Now().Add(2 * time.Second)
	for e.hub.Sessions() != 2 {
		if time.Now().After(deadline) {
			t.Fatalf("sessions = %d, want 2", e.hub.Sessions())
		}
		time.Sleep(5 * time.Millisecond)
	}
	e.tick()
	full := next(t, withMap, "state")
	if full["tick"] != "1" || full["stages"] == nil || full["player"] == nil {
		t.Fatalf("state = %v", full)
	}
	light := next(t, noMap, "state")
	if _, ok := light["stages"]; ok {
		t.Fatalf("map=false state carries the map: %v", light)
	}
}

func TestMalformedAndUnknownFramesGetErrors(t *testing.T) {
	e := newEnv(t)
	conn := e.dial("player_id=" + ana)
	cases := []struct{ frame, code string }{
		{`not json`, "invalid_message"},
		{`{"type":"drop","action_id":"nope"}`, "invalid_message"},
		{`{"type":"fly","action_id":"` + actionID + `"}`, "unknown_type"},
	}
	for _, tc := range cases {
		send(t, conn, tc.frame)
		if got := next(t, conn, "error"); got["code"] != tc.code {
			t.Fatalf("frame %s → %v, want code %s", tc.frame, got, tc.code)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageBinary, []byte{1, 2}); err != nil {
		t.Fatal(err)
	}
	if got := next(t, conn, "error"); got["code"] != "invalid_message" {
		t.Fatalf("binary frame → %v", got)
	}
	if n := e.hub.Status().QueueLen; n != 0 {
		t.Fatalf("errors must not queue anything, queue = %d", n)
	}
}

func TestShutdownClosesConnections(t *testing.T) {
	e := newEnv(t)
	conn := e.dial("player_id=" + ana)
	e.cancel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _, err := conn.Read(ctx)
	if websocket.CloseStatus(err) != websocket.StatusGoingAway {
		t.Fatalf("read after shutdown: %v, want close status going away", err)
	}
}

func dialWithOrigin(e *env, origin string) (*http.Response, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	url := "ws" + strings.TrimPrefix(e.server.URL, "http") + "/play?player_id=" + ana
	conn, resp, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {origin}}})
	if err == nil {
		conn.CloseNow()
	}
	return resp, err
}

func TestCrossOriginIsRejectedByDefault(t *testing.T) {
	e := newEnv(t)
	resp, err := dialWithOrigin(e, "http://evil.example")
	if err == nil || resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin dial: resp=%v err=%v, want 403", resp, err)
	}
}

func TestAllowedOriginPattern(t *testing.T) {
	e := newEnv(t, "front.example:8080")
	if _, err := dialWithOrigin(e, "https://front.example:8080"); err != nil {
		t.Fatalf("allowed origin rejected: %v", err)
	}
}

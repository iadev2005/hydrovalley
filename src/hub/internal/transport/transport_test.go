package transport

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/golang-jwt/jwt/v5"
	worldv1 "github.com/iadev2005/hydrovalley/gen/go/hydrovalley/world/v1"
	"github.com/iadev2005/hydrovalley/src/hub/internal/auth"
	"github.com/iadev2005/hydrovalley/src/hub/internal/hub"
)

const (
	ana      = "11111111-1111-4111-8111-111111111111"
	bob      = "22222222-2222-4222-8222-222222222222"
	actionID = "0b7c9a52-5f3e-4f5b-9d7e-2a1f3c4d5e6f"
	uuid2    = "6c1f0d3a-8b7e-4c2d-9a10-3f4e5d6c7b8a"
)

// applyAll aplica todos los comandos; fail simula un simulador caído.
type applyAll struct {
	mu   sync.Mutex
	tick uint64
	fail bool
	last []*worldv1.Command
}

func (w *applyAll) Step(_ context.Context, req *worldv1.StepRequest) (*worldv1.StepResponse, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.fail {
		return nil, errors.New("down")
	}
	w.tick++
	w.last = req.GetCommands()
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
	priv   ed25519.PrivateKey
	// now es el reloj con el que el verificador comprueba `exp`.
	now func() time.Time
	// offset adelanta ese reloj.
	offset *atomic.Int64
}

func newEnv(t *testing.T, originPatterns ...string) *env {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var offset atomic.Int64
	now := func() time.Time { return time.Now().Add(time.Duration(offset.Load())) }
	world := &applyAll{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := hub.New(world, hub.SystemClock{}, hub.DefaultConfig(), logger)
	ctx, cancel := context.WithCancel(context.Background())
	srv := httptest.NewServer(NewHandler(ctx, h, logger, Options{
		OriginPatterns: originPatterns,
		Verifier:       auth.NewVerifier(pub, now),
	}))
	t.Cleanup(func() {
		cancel()
		srv.Close()
	})
	return &env{t: t, hub: h, world: world, server: srv, cancel: cancel, priv: priv, now: now, offset: &offset}
}

// tokenFor firma un token válido de 12 h para el jugador con el rol dado.
func (e *env) tokenFor(playerID string, role auth.Role) string {
	return e.tokenExpiring(playerID, role, e.now().Add(12*time.Hour))
}

func (e *env) tokenExpiring(playerID string, role auth.Role, exp time.Time) string {
	e.t.Helper()
	return e.sign(e.priv, jwt.MapClaims{
		"sub": playerID, "player_id": playerID, "username": "user", "role": string(role),
		"iss": auth.Issuer, "aud": auth.Audience,
		"iat": e.now().Unix(), "exp": exp.Unix(),
	})
}

func (e *env) sign(key ed25519.PrivateKey, c jwt.MapClaims) string {
	e.t.Helper()
	s, err := jwt.NewWithClaims(jwt.SigningMethodEdDSA, c).SignedString(key)
	if err != nil {
		e.t.Fatal(err)
	}
	return s
}

// as devuelve la consulta de /play de un jugador con su token.
func (e *env) as(playerID string, extra ...string) string {
	q := "token=" + e.tokenFor(playerID, auth.RolePlayer)
	for _, param := range extra {
		q += "&" + param
	}
	return q
}

// asAdmin devuelve la consulta de /play de un administrador.
func (e *env) asAdmin(playerID string) string {
	return "token=" + e.tokenFor(playerID, auth.RoleAdmin)
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

// waitSessions espera a que haya n conexiones registradas.
func (e *env) waitSessions(n int) {
	e.t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for e.hub.Sessions() != n {
		if time.Now().After(deadline) {
			e.t.Fatalf("sessions = %d, want %d", e.hub.Sessions(), n)
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

func TestPlayRejectsInvalidMapParameter(t *testing.T) {
	e := newEnv(t)
	if resp := e.get("/play?" + e.as(ana, "map=maybe")); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("map=maybe = %d, want 400", resp.StatusCode)
	}
}

// expectClose espera que el servidor cierre la conexión con el código dado.
func expectClose(t *testing.T, conn *websocket.Conn, want websocket.StatusCode) websocket.CloseError {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _, err := conn.Read(ctx)
	var ce websocket.CloseError
	if !errors.As(err, &ce) || ce.Code != want {
		t.Fatalf("read: %v, want close code %d", err, want)
	}
	return ce
}

func TestPlayClosesWith4401WhenTheTokenIsNotValid(t *testing.T) {
	e := newEnv(t)
	_, otherKey, _ := ed25519.GenerateKey(rand.Reader)
	valid := e.tokenFor(ana, auth.RolePlayer)
	cases := map[string]string{
		"no token":        "map=false",
		"empty token":     "token=",
		"only player_id":  "player_id=" + ana,
		"garbage":         "token=abc.def.ghi",
		"expired":         "token=" + e.tokenExpiring(ana, auth.RolePlayer, e.now().Add(-time.Hour)),
		"foreign key":     "token=" + e.sign(otherKey, jwt.MapClaims{"sub": ana, "player_id": ana, "username": "u", "role": "player", "exp": e.now().Add(time.Hour).Unix()}),
		"tampered":        "token=" + flipAt(valid, len(valid)-20),
		"unknown role":    "token=" + e.tokenFor(ana, auth.Role("root")),
		"token twice bad": "token=&token=" + valid[:10],
	}
	for name, q := range cases {
		t.Run(name, func(t *testing.T) {
			conn := e.dial(q)
			ce := expectClose(t, conn, 4401)
			if ce.Reason == "" || strings.Contains(ce.Reason, valid[:20]) {
				t.Fatalf("close reason = %q, want a short reason without the token", ce.Reason)
			}
			if n := e.hub.Sessions(); n != 0 {
				t.Fatalf("sessions = %d, an invalid token must never register a session", n)
			}
		})
	}
}

// flipAt cambia un carácter (en el interior de la firma, donde todos los
// bits cuentan).
func flipAt(s string, i int) string {
	c := byte('A')
	if s[i] == 'A' {
		c = 'B'
	}
	return s[:i] + string(c) + s[i+1:]
}

func TestSessionIdentityComesOnlyFromTheToken(t *testing.T) {
	e := newEnv(t)
	// player_id en la consulta ya no significa nada: manda el token.
	conn := e.dial(e.as(ana, "player_id="+bob))
	send(t, conn, `{"type":"drop","action_id":"`+actionID+`","x":0,"y":0}`)
	e.waitQueue(1)
	e.tick()
	next(t, conn, "result")
	e.world.mu.Lock()
	defer e.world.mu.Unlock()
	if len(e.world.last) != 1 || e.world.last[0].GetPlayerId() != ana {
		t.Fatalf("Step commands = %v, want one command for %s", e.world.last, ana)
	}
}

func TestExpiryDoesNotCutAnOpenConnectionButBlocksReconnecting(t *testing.T) {
	e := newEnv(t)
	token := e.tokenExpiring(ana, auth.RolePlayer, e.now().Add(time.Minute))
	conn := e.dial("token=" + token)
	e.waitSessions(1) // el token se comprueba justo después del upgrade
	// El reloj del verificador pasa el vencimiento (y el margen de 30 s).
	e.offset.Store(int64(10 * time.Minute))

	send(t, conn, `{"type":"drop","action_id":"`+actionID+`","x":0,"y":0}`)
	e.waitQueue(1)
	e.tick()
	if r := next(t, conn, "result"); r["status"] != "applied" {
		t.Fatalf("result on the open connection = %v, want applied", r)
	}
	expectClose(t, e.dial("token="+token), 4401)
}

func TestAdminCommandsNeedTheAdminRole(t *testing.T) {
	e := newEnv(t)
	player := e.dial(e.as(ana))
	send(t, player, `{"type":"set_demo_mode","action_id":"`+actionID+`","enabled":true}`)
	if r := next(t, player, "result"); r["status"] != "forbidden" || r["action_id"] != actionID {
		t.Fatalf("player result = %v, want forbidden", r)
	}
	send(t, player, `{"type":"reset_valley","action_id":"`+actionID+`"}`)
	if r := next(t, player, "result"); r["status"] != "forbidden" {
		t.Fatalf("player result = %v, want forbidden", r)
	}
	if n := e.hub.Status().QueueLen; n != 0 {
		t.Fatalf("queue = %d, forbidden commands are never queued", n)
	}

	admin := e.dial(e.asAdmin(bob))
	send(t, admin, `{"type":"set_demo_mode","action_id":"`+actionID+`","enabled":true}`)
	send(t, admin, `{"type":"reset_valley","action_id":"`+uuid2+`"}`)
	e.waitQueue(2)
	e.tick()
	e.world.mu.Lock()
	cmds := e.world.last
	e.world.mu.Unlock()
	if len(cmds) != 2 || cmds[0].GetSetDemoMode() == nil || !cmds[0].GetSetDemoMode().GetEnabled() ||
		cmds[1].GetResetValley() == nil || cmds[0].GetPlayerId() != bob {
		t.Fatalf("Step commands = %v", cmds)
	}
	for range 2 {
		if r := next(t, admin, "result"); r["status"] != "applied" {
			t.Fatalf("admin result = %v, want applied", r)
		}
	}
}

func TestDropEndToEnd(t *testing.T) {
	e := newEnv(t)
	conn := e.dial(e.as(ana))
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
	withMap := e.dial(e.as(ana))
	noMap := e.dial(e.as(ana, "map=false"))
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
	conn := e.dial(e.as(ana))
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
	conn := e.dial(e.as(ana))
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
	url := "ws" + strings.TrimPrefix(e.server.URL, "http") + "/play?" + e.as(ana)
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

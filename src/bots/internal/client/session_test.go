package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// fakeHub es un /play falso: acepta el WebSocket y le entrega cada conexión
// numerada (desde 1) al manejador de la prueba.
type fakeHub struct {
	srv   *httptest.Server
	conns atomic.Int32
	query chan string
}

func newFakeHub(t *testing.T, handle func(ctx context.Context, c *websocket.Conn, n int)) *fakeHub {
	t.Helper()
	h := &fakeHub{query: make(chan string, 16)}
	h.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/play" {
			http.NotFound(w, r)
			return
		}
		h.query <- r.URL.RawQuery
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		handle(r.Context(), c, int(h.conns.Add(1)))
	}))
	t.Cleanup(h.srv.Close)
	return h
}

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func startSession(t *testing.T, h *fakeHub, clock *fakeClock, mapFlag bool) (*Session, context.CancelFunc, chan error) {
	t.Helper()
	s, err := NewSession(SessionConfig{
		BaseURL: h.srv.URL,
		Token:   "tok",
		Map:     mapFlag,
		Now:     clock.Now,
		Backoff: func(int) time.Duration { return 5 * time.Millisecond },
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	return s, cancel, done
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for condition")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func readFrame(t *testing.T, ctx context.Context, c *websocket.Conn) map[string]any {
	t.Helper()
	_, data, err := c.Read(ctx)
	if err != nil {
		t.Errorf("fake hub read: %v", err)
		return nil
	}
	var m map[string]any
	_ = json.Unmarshal(data, &m)
	return m
}

func recvOutcome(t *testing.T, s *Session) Outcome {
	t.Helper()
	select {
	case o := <-s.Outcomes():
		return o
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for outcome")
		return Outcome{}
	}
}

func TestSessionConnectsWithTokenAndMapFlagAndDeliversStates(t *testing.T) {
	h := newFakeHub(t, func(ctx context.Context, c *websocket.Conn, _ int) {
		_ = c.Write(ctx, websocket.MessageText, []byte(`{"type":"state","tick":"7","width":1,"height":1,"stages":[1],"humidity":["0"]}`))
		<-ctx.Done()
	})
	for _, want := range []bool{true, false} {
		clock := &fakeClock{t: time.Unix(1000, 0)}
		s, cancel, done := startSession(t, h, clock, want)
		select {
		case st := <-s.States():
			if st.Tick != 7 || len(st.Stages) != 1 {
				t.Fatalf("unexpected state %+v", st)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("no state")
		}
		q := <-h.query
		wantMap := "map=false"
		if want {
			wantMap = "map=true"
		}
		if !strings.Contains(q, "token=tok") || !strings.Contains(q, wantMap) {
			t.Fatalf("query %q lacks token or %s", q, wantMap)
		}
		cancel()
		if err := <-done; err != nil {
			t.Fatalf("Run after cancel = %v", err)
		}
	}
}

func TestSessionTracksPendingAndMeasuresLatency(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1000, 0)}
	got := make(chan map[string]any, 1)
	release := make(chan struct{})
	h := newFakeHub(t, func(ctx context.Context, c *websocket.Conn, _ int) {
		f := readFrame(t, ctx, c)
		got <- f
		<-release
		_ = c.Write(ctx, websocket.MessageText, []byte(`{"type":"result","action_id":"`+f["action_id"].(string)+`","status":"applied"}`))
		<-ctx.Done()
	})
	s, _, _ := startSession(t, h, clock, false)
	waitFor(t, s.Connected)

	id, err := s.Drop(3, 4)
	if err != nil {
		t.Fatal(err)
	}
	f := <-got
	if f["type"] != "drop" || f["action_id"] != id || f["x"] != float64(3) || f["y"] != float64(4) {
		t.Fatalf("unexpected frame %v", f)
	}
	if s.PendingCount() != 1 {
		t.Fatalf("pending = %d, want 1", s.PendingCount())
	}
	clock.Advance(120 * time.Millisecond)
	close(release)
	o := recvOutcome(t, s)
	if o.ActionID != id || o.Status != "applied" || o.Latency != 120*time.Millisecond || !o.Applied() {
		t.Fatalf("unexpected outcome %+v", o)
	}
	if s.PendingCount() != 0 {
		t.Fatalf("pending = %d after result", s.PendingCount())
	}
}

func TestDuplicateCountsAsAppliedAndRejectedKeepsReason(t *testing.T) {
	h := newFakeHub(t, func(ctx context.Context, c *websocket.Conn, _ int) {
		a := readFrame(t, ctx, c)
		b := readFrame(t, ctx, c)
		_ = c.Write(ctx, websocket.MessageText, []byte(`{"type":"result","action_id":"`+a["action_id"].(string)+`","status":"duplicate"}`))
		_ = c.Write(ctx, websocket.MessageText, []byte(`{"type":"result","action_id":"`+b["action_id"].(string)+`","status":"rejected","reason":"insufficient_water"}`))
		<-ctx.Done()
	})
	s, _, _ := startSession(t, h, &fakeClock{t: time.Unix(1, 0)}, false)
	waitFor(t, s.Connected)
	_, _ = s.Drop(0, 0)
	_, _ = s.Drop(1, 1)
	o1, o2 := recvOutcome(t, s), recvOutcome(t, s)
	if !o1.Applied() || o1.Status != "duplicate" {
		t.Fatalf("duplicate must count as applied: %+v", o1)
	}
	if o2.Applied() || o2.Reason != "insufficient_water" {
		t.Fatalf("unexpected rejected outcome %+v", o2)
	}
}

func TestServerErrorFrameResolvesPending(t *testing.T) {
	h := newFakeHub(t, func(ctx context.Context, c *websocket.Conn, _ int) {
		a := readFrame(t, ctx, c)
		_ = c.Write(ctx, websocket.MessageText, []byte(`{"type":"error","action_id":"`+a["action_id"].(string)+`","code":"invalid_message"}`))
		<-ctx.Done()
	})
	s, _, _ := startSession(t, h, &fakeClock{t: time.Unix(1, 0)}, false)
	waitFor(t, s.Connected)
	_, _ = s.Drop(0, 0)
	o := recvOutcome(t, s)
	if o.Status != "error" || o.Reason != "invalid_message" || s.PendingCount() != 0 {
		t.Fatalf("unexpected %+v pending=%d", o, s.PendingCount())
	}
}

func TestClose4401StopsWithoutReconnecting(t *testing.T) {
	h := newFakeHub(t, func(ctx context.Context, c *websocket.Conn, _ int) {
		_ = c.Close(websocket.StatusCode(4401), "invalid or expired token")
	})
	_, _, done := startSession(t, h, &fakeClock{t: time.Unix(1, 0)}, false)
	select {
	case err := <-done:
		if !errors.Is(err, ErrUnauthorized) {
			t.Fatalf("want ErrUnauthorized, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not stop on 4401")
	}
	time.Sleep(50 * time.Millisecond)
	if n := h.conns.Load(); n != 1 {
		t.Fatalf("reconnected after 4401: %d connections", n)
	}
}

func TestReconnectResendsFreshPendingButNotStaleOrAdmin(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1000, 0)}
	first := make(chan struct{})
	resent := make(chan map[string]any, 4)
	h := newFakeHub(t, func(ctx context.Context, c *websocket.Conn, n int) {
		if n == 1 {
			// Recibe tres comandos y cae sin responder.
			for range 3 {
				readFrame(t, ctx, c)
			}
			<-first
			_ = c.Close(websocket.StatusGoingAway, "restart")
			return
		}
		for {
			_, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			var m map[string]any
			_ = json.Unmarshal(data, &m)
			resent <- m
		}
	})
	s, _, _ := startSession(t, h, clock, false)
	waitFor(t, s.Connected)

	staleID, _ := s.Drop(1, 1)
	clock.Advance(31 * time.Second)
	adminID, _ := s.SetDemoMode(true)
	freshID, _ := s.Drop(2, 2)
	clock.Advance(time.Second)
	close(first)

	var m map[string]any
	select {
	case m = <-resent:
	case <-time.After(3 * time.Second):
		t.Fatal("nothing resent after reconnect")
	}
	if m["action_id"] != freshID {
		t.Fatalf("resent %v, want the fresh drop %s", m, freshID)
	}
	select {
	case extra := <-resent:
		t.Fatalf("unexpected extra resend %v", extra)
	case <-time.After(100 * time.Millisecond):
	}

	// El obsoleto se descarta con un resultado local "expired"; el de
	// administrador sigue pendiente (no se reenvía) hasta que envejezca.
	o := recvOutcome(t, s)
	if o.ActionID != staleID || o.Status != "expired" || !o.Local {
		t.Fatalf("unexpected outcome %+v", o)
	}
	if s.PendingCount() != 2 {
		t.Fatalf("pending = %d, want fresh drop + admin command", s.PendingCount())
	}
	_ = adminID
}

func TestResultAfterReconnectResolvesOriginalActionWithOriginalSendTime(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1000, 0)}
	dropped := make(chan struct{})
	h := newFakeHub(t, func(ctx context.Context, c *websocket.Conn, n int) {
		if n == 1 {
			readFrame(t, ctx, c)
			<-dropped
			_ = c.Close(websocket.StatusGoingAway, "restart")
			return
		}
		f := readFrame(t, ctx, c)
		_ = c.Write(ctx, websocket.MessageText, []byte(`{"type":"result","action_id":"`+f["action_id"].(string)+`","status":"duplicate"}`))
		<-ctx.Done()
	})
	s, _, _ := startSession(t, h, clock, false)
	waitFor(t, s.Connected)
	id, _ := s.Drop(0, 0)
	clock.Advance(2 * time.Second)
	close(dropped)
	o := recvOutcome(t, s)
	if o.ActionID != id || !o.Applied() || o.Latency != 2*time.Second {
		t.Fatalf("unexpected outcome %+v", o)
	}
}

func TestSendWhileDisconnectedFails(t *testing.T) {
	s, err := NewSession(SessionConfig{BaseURL: "http://127.0.0.1:1", Token: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Drop(0, 0); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("want ErrNotConnected, got %v", err)
	}
	if s.PendingCount() != 0 {
		t.Fatal("a failed send must not stay pending")
	}
}

func TestPlayURLSchemeMapping(t *testing.T) {
	cases := map[string]string{
		"http://nginx:8080":      "ws://nginx:8080/play?map=true&token=t",
		"https://host/":          "wss://host/play?map=true&token=t",
		"http://host:8080/sub/x": "ws://host:8080/sub/x/play?map=true&token=t",
	}
	for in, want := range cases {
		got, err := playURL(in, "t", true)
		if err != nil || got != want {
			t.Errorf("playURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
}

func TestUnmatchedDeliversResultsAndErrorsWithoutAPendingAction(t *testing.T) {
	h := newFakeHub(t, func(ctx context.Context, c *websocket.Conn, _ int) {
		_ = c.Write(ctx, websocket.MessageText, []byte(`{"type":"result","action_id":"old","status":"duplicate"}`))
		_ = c.Write(ctx, websocket.MessageText, []byte(`{"type":"error","code":"unknown_type"}`))
		<-ctx.Done()
	})
	s, _, _ := startSession(t, h, &fakeClock{t: time.Unix(1, 0)}, false)
	var got []Frame
	for len(got) < 2 {
		select {
		case f := <-s.Unmatched():
			got = append(got, f)
		case <-time.After(3 * time.Second):
			t.Fatalf("timed out, got %+v", got)
		}
	}
	if got[0] != (Frame{Type: "result", ActionID: "old", Status: "duplicate"}) {
		t.Fatalf("unexpected first frame %+v", got[0])
	}
	if got[1] != (Frame{Type: "error", Reason: "unknown_type"}) {
		t.Fatalf("unexpected second frame %+v", got[1])
	}
}

func TestSendRawAndResendDropWriteWithoutRegisteringPending(t *testing.T) {
	frames := make(chan map[string]any, 4)
	raw := make(chan string, 1)
	h := newFakeHub(t, func(ctx context.Context, c *websocket.Conn, _ int) {
		_, data, err := c.Read(ctx)
		if err == nil {
			raw <- string(data)
		}
		frames <- readFrame(t, ctx, c)
		<-ctx.Done()
	})
	s, _, _ := startSession(t, h, &fakeClock{t: time.Unix(1, 0)}, false)
	waitFor(t, s.Connected)
	if err := s.SendRaw([]byte("{not json")); err != nil {
		t.Fatal(err)
	}
	if err := s.ResendDrop("abc", 2, 5); err != nil {
		t.Fatal(err)
	}
	if got := <-raw; got != "{not json" {
		t.Fatalf("raw frame = %q", got)
	}
	f := <-frames
	if f["type"] != "drop" || f["action_id"] != "abc" || f["x"] != float64(2) || f["y"] != float64(5) {
		t.Fatalf("unexpected resend %v", f)
	}
	if s.PendingCount() != 0 {
		t.Fatalf("pending = %d, want 0", s.PendingCount())
	}
}

func TestSendRawWhileDisconnectedFails(t *testing.T) {
	s, err := NewSession(SessionConfig{BaseURL: "http://127.0.0.1:1", Token: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SendRaw([]byte("x")); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("SendRaw = %v, want ErrNotConnected", err)
	}
}

func TestSessionRotatesAcrossCommaSeparatedBaseURLsAndDetectsReadSilence(t *testing.T) {
	// El primer hub acepta la conexión y se queda mudo (como un contenedor en docker pause);
	// el segundo hub envía el estado esperado tras la rotación por ReadTimeout.
	silentHub := newFakeHub(t, func(ctx context.Context, _ *websocket.Conn, _ int) {
		<-ctx.Done()
	})
	backupHub := newFakeHub(t, func(ctx context.Context, c *websocket.Conn, _ int) {
		_ = c.Write(ctx, websocket.MessageText, []byte(`{"type":"state","tick":"42","width":1,"height":1,"stages":[1],"humidity":["0"]}`))
		<-ctx.Done()
	})

	s, err := NewSession(SessionConfig{
		BaseURL:     silentHub.srv.URL + "," + backupHub.srv.URL,
		Token:       "tok",
		ReadTimeout: 40 * time.Millisecond,
		Backoff:     func(int) time.Duration { return 5 * time.Millisecond },
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()

	select {
	case st := <-s.States():
		if st.Tick != 42 {
			t.Fatalf("unexpected state tick %d, want 42", st.Tick)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for state from backup hub after silence timeout")
	}
	if silentHub.conns.Load() < 1 || backupHub.conns.Load() < 1 {
		t.Fatalf("expected both hubs to be contacted, got silent=%d backup=%d", silentHub.conns.Load(), backupHub.conns.Load())
	}
	cancel()
	<-done
}

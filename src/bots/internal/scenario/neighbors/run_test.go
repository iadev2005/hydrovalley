package neighbors

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/iadev2005/hydrovalley/src/bots/internal/client"
)

// fakeValley acepta /play, difunde un estado con mapa y responde a cada gota.
func fakeValley(t *testing.T, drops *atomic.Int32, status string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("map") != "true" {
			t.Errorf("neighbors must open /play with map=true")
		}
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		ctx := r.Context()
		_ = c.Write(ctx, websocket.MessageText, []byte(`{"type":"state","tick":"1","width":2,"height":2,"stages":[1,1,1,1],"humidity":["0","0","0","0"],"player":{"water":"100","leaves":"0"}}`))
		for {
			_, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			var f struct {
				Type     string `json:"type"`
				ActionID string `json:"action_id"`
			}
			_ = json.Unmarshal(data, &f)
			if f.Type == "drop" {
				drops.Add(1)
			}
			reply := `{"type":"result","action_id":"` + f.ActionID + `","status":"` + status + `"}`
			if status == "rejected" {
				reply = `{"type":"result","action_id":"` + f.ActionID + `","status":"rejected","reason":"insufficient_water"}`
			}
			_ = c.Write(ctx, websocket.MessageText, []byte(reply))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func sessions(t *testing.T, srv *httptest.Server, n int) []Session {
	t.Helper()
	var out []Session
	for range n {
		s, err := client.NewSession(client.SessionConfig{BaseURL: srv.URL, Token: "t", Map: true})
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

func TestRunDropsUntilContextEndsAndSummarises(t *testing.T) {
	var drops atomic.Int32
	srv := fakeValley(t, &drops, "applied")
	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancel()
	cfg := Config{Interval: 30 * time.Millisecond, Jitter: 0.5, Seed: 1}
	sum := Run(ctx, cfg, sessions(t, srv, 3), nil)

	if sum.Bots != 3 || sum.Sent < 6 {
		t.Fatalf("unexpected summary %+v", sum)
	}
	if int32(sum.Sent) < drops.Load() {
		t.Fatalf("summary sent %d < server drops %d", sum.Sent, drops.Load())
	}
	if sum.Results["applied"] == 0 {
		t.Fatalf("no applied results: %+v", sum.Results)
	}
	if sum.Unresolved < 0 || sum.Unresolved > 3 {
		t.Fatalf("unresolved %d implausible", sum.Unresolved)
	}
}

func TestRunCountsRejectionsByReason(t *testing.T) {
	var drops atomic.Int32
	srv := fakeValley(t, &drops, "rejected")
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	sum := Run(ctx, Config{Interval: 30 * time.Millisecond, Seed: 2}, sessions(t, srv, 1), nil)
	if sum.Results["rejected:insufficient_water"] == 0 {
		t.Fatalf("expected rejected:insufficient_water, got %+v", sum.Results)
	}
}

func TestRunStopsABotWhoseTokenIsRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		_ = c.Close(websocket.StatusCode(4401), "invalid or expired token")
	}))
	defer srv.Close()
	done := make(chan Summary, 1)
	go func() {
		done <- Run(context.Background(), Config{Interval: 20 * time.Millisecond}, sessions(t, srv, 2), nil)
	}()
	select {
	case sum := <-done:
		if sum.Unauthorized != 2 {
			t.Fatalf("want 2 unauthorized bots, got %+v", sum)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run must return once every bot stopped")
	}
}

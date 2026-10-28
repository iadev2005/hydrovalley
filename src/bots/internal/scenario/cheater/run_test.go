package cheater

import (
	"context"
	"encoding/json"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/iadev2005/hydrovalley/src/bots/internal/client"
)

// hub simula la admisión del hub real: pending (se ignora), confirmed
// (`duplicate`), ventana deslizante de 1 s y errores para lo inválido.
type hub struct {
	mu        sync.Mutex
	limit     int // 0 = sin límite
	times     []time.Time
	pending   map[string]bool
	confirmed map[string]bool
	applied   map[string]int
	// Fallos que se simulan.
	noDuplicateCheck bool // aplica de nuevo un action_id repetido
	silentInvalid    bool // no responde a los mensajes inválidos
	dropsSeen        int
}

func newHub(limit int) *hub {
	return &hub{limit: limit, pending: map[string]bool{}, confirmed: map[string]bool{}, applied: map[string]int{}}
}

func (h *hub) permits(now time.Time) bool {
	if h.limit == 0 {
		return true
	}
	kept := h.times[:0]
	for _, t := range h.times {
		if now.Sub(t) < time.Second {
			kept = append(kept, t)
		}
	}
	h.times = kept
	return len(h.times) < h.limit
}

func (h *hub) serve(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer c.CloseNow()
	ctx := r.Context()
	var wmu sync.Mutex
	write := func(v string) {
		wmu.Lock()
		defer wmu.Unlock()
		_ = c.Write(ctx, websocket.MessageText, []byte(v))
	}
	go func() {
		for ctx.Err() == nil {
			write(`{"type":"state","tick":"1","width":32,"height":32}`)
			time.Sleep(20 * time.Millisecond)
		}
	}()
	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			return
		}
		var m struct {
			Type     string `json:"type"`
			ActionID string `json:"action_id"`
		}
		if json.Unmarshal(data, &m) != nil || m.Type == "" {
			if !h.silentInvalid {
				write(`{"type":"error","code":"invalid_message"}`)
			}
			continue
		}
		if m.Type != "drop" {
			if !h.silentInvalid {
				write(`{"type":"error","code":"unknown_type"}`)
			}
			continue
		}
		h.mu.Lock()
		h.dropsSeen++
		now := time.Now()
		var reply string
		switch {
		case h.pending[m.ActionID]:
		case h.confirmed[m.ActionID] && !h.noDuplicateCheck:
			reply = `duplicate`
		case !h.permits(now):
			reply = `rate_limited`
		default:
			h.times = append(h.times, now)
			h.pending[m.ActionID] = true
			go func() {
				time.Sleep(30 * time.Millisecond)
				h.mu.Lock()
				delete(h.pending, m.ActionID)
				h.confirmed[m.ActionID] = true
				h.applied[m.ActionID]++
				h.mu.Unlock()
				write(`{"type":"result","action_id":"` + m.ActionID + `","status":"applied"}`)
			}()
		}
		h.mu.Unlock()
		if reply != "" {
			write(`{"type":"result","action_id":"` + m.ActionID + `","status":"` + reply + `"}`)
		}
	}
}

func fastConfig() Config {
	return Config{Duration: 2200 * time.Millisecond, Rate: 50, Settle: 50 * time.Millisecond, Drain: time.Second, Wait: 300 * time.Millisecond, ConnectTimeout: 2 * time.Second, Seed: 3}
}

func runWith(t *testing.T, h *hub, cfg Config) (Report, error) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(h.serve))
	t.Cleanup(srv.Close)
	s, err := client.NewSession(client.SessionConfig{BaseURL: srv.URL, Token: "t"})
	if err != nil {
		t.Fatal(err)
	}
	return Run(context.Background(), cfg, s)
}

func TestRunPassesAgainstAHubThatEnforcesTheRules(t *testing.T) {
	h := newHub(15)
	rep, err := runWith(t, h, fastConfig())
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Passed() {
		t.Fatalf("expected a pass: %s", FormatSummary(rep))
	}
	if rep.Flood.Sent < 100 || rep.Flood.RateLimited == 0 {
		t.Fatalf("unexpected flood %+v", rep.Flood)
	}
	if rep.Duplicates.Applied != 1 || rep.Duplicates.DuplicateFrames != 2 {
		t.Fatalf("unexpected duplicates %+v", rep.Duplicates)
	}
}

func TestRunFailsFloodWhenTheHubHasNoLimit(t *testing.T) {
	rep, err := runWith(t, newHub(0), fastConfig())
	if err != nil {
		t.Fatal(err)
	}
	if rep.Flood.OK || rep.Passed() {
		t.Fatalf("a hub without limit must fail the flood: %+v", rep.Flood)
	}
}

func TestRunFailsDuplicatesWhenTheHubAppliesTheSameActionTwice(t *testing.T) {
	h := newHub(15)
	h.noDuplicateCheck = true
	cfg := fastConfig()
	cfg.Duration = 1100 * time.Millisecond
	rep, err := runWith(t, h, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Duplicates.OK || rep.Duplicates.Applied < 2 {
		t.Fatalf("a repeated action_id applied twice must fail: %+v", rep.Duplicates)
	}
}

func TestRunFailsInvalidWhenTheHubStaysSilent(t *testing.T) {
	h := newHub(15)
	h.silentInvalid = true
	cfg := fastConfig()
	cfg.Duration = 1100 * time.Millisecond
	rep, err := runWith(t, h, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Invalid.OK || rep.Invalid.MalformedOK || !rep.Invalid.ConnectionOK {
		t.Fatalf("unexpected %+v", rep.Invalid)
	}
}

func TestRunFailsWhenTheSessionNeverConnects(t *testing.T) {
	s, err := client.NewSession(client.SessionConfig{BaseURL: "http://127.0.0.1:1", Token: "t"})
	if err != nil {
		t.Fatal(err)
	}
	cfg := fastConfig()
	cfg.ConnectTimeout = 150 * time.Millisecond
	if _, err := Run(context.Background(), cfg, s); err == nil {
		t.Fatal("expected an error")
	}
}

// La acción repetida (RF-09) necesita una gota aplicada: el bot elige una
// casilla que la acepte para que el terreno no la rechace con `tile_occupied`.
func TestTilePickerOnlyOffersTilesThatAcceptDrops(t *testing.T) {
	st := client.State{Width: 3, Height: 2,
		Stages:    []uint32{6, 7, 1, 1, 6, 7},
		Buildings: []bool{false, false, true, false, false, false},
	}
	pick := tilePicker(rand.New(rand.NewPCG(1, 1)), st)
	for range 50 {
		if x, y := pick(); x != 0 || y != 1 {
			t.Fatalf("got (%d,%d), want (0,1)", x, y)
		}
	}
	// Sin mapa elige cualquier casilla del valle.
	any := tilePicker(rand.New(rand.NewPCG(1, 1)), client.State{Width: 4, Height: 4})
	for range 50 {
		if x, y := any(); x >= 4 || y >= 4 {
			t.Fatalf("(%d,%d) is outside the valley", x, y)
		}
	}
}

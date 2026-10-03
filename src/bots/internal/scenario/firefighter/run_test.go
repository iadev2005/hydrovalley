package firefighter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/iadev2005/hydrovalley/src/bots/internal/client"
)

const (
	need  = 3 // gotas para apagar (lo decide el mundo simulado)
	bonus = 5
	fx    = 5
	fy    = 7
)

// world simula el hub y el simulador con un solo incendio: aparece en el
// estado número igniteAt, se apaga con `need` gotas aplicadas (y da el bono
// al jugador de la última) o se consume si pasan burnAfter estados.
type world struct {
	mu       sync.Mutex
	states   int
	igniteAt int // 0 = nunca
	burning  bool
	burnt    bool // se consumió: la casilla vuelve a tierra seca
	received int
	drops    []string // casillas "x,y" de todas las gotas
	leaves   map[string]uint64

	ignoreDrops bool // las gotas no apagan nada
	burnAfter   int  // estados que tarda en consumirse; 0 = nunca
	bonusToAll  bool // error simulado: el bono llega a todos
	burnedAt    int
}

func newWorld(igniteAt int) *world {
	return &world{igniteAt: igniteAt, leaves: map[string]uint64{}}
}

func (w *world) serve(rw http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	c, err := websocket.Accept(rw, r, nil)
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
			write(w.stateFrame(token))
			time.Sleep(25 * time.Millisecond)
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
			X, Y     uint32
		}
		if json.Unmarshal(data, &m) != nil || m.Type != "drop" {
			continue
		}
		w.mu.Lock()
		w.drops = append(w.drops, fmt.Sprintf("%d,%d", m.X, m.Y))
		if w.burning && m.X == fx && m.Y == fy && !w.ignoreDrops {
			w.received++
			if w.received >= need {
				w.burning = false
				if w.bonusToAll {
					for k := range w.leaves {
						w.leaves[k] += bonus
					}
				} else {
					w.leaves[token] += bonus
				}
			}
		}
		w.mu.Unlock()
		write(`{"type":"result","action_id":"` + m.ActionID + `","status":"applied"}`)
	}
}

func (w *world) stateFrame(token string) string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, ok := w.leaves[token]; !ok {
		w.leaves[token] = 10
	}
	w.states++
	if w.igniteAt > 0 && w.states == w.igniteAt {
		w.burning, w.burnedAt = true, w.states
	}
	if w.burning && w.burnAfter > 0 && w.states-w.burnedAt >= w.burnAfter {
		w.burning, w.burnt = false, true
	}
	stages := make([]string, 32*32)
	for i := range stages {
		stages[i] = "2"
	}
	if w.burnt {
		stages[fy*32+fx] = "1"
	}
	fires := "[]"
	if w.burning {
		fires = fmt.Sprintf(`[{"x":%d,"y":%d,"drops_received":%d}]`, fx, fy, w.received)
	}
	return fmt.Sprintf(`{"type":"state","tick":"%d","width":32,"height":32,"stages":[%s],"fires":%s,"player":{"leaves":"%d"},"humidity_contributed":"%d","accepted_drops":"%d"}`,
		w.states, strings.Join(stages, ","), fires, w.leaves[token], len(w.drops)*25, len(w.drops))
}

func (w *world) dropsOnFire() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := 0
	for _, d := range w.drops {
		if d == fmt.Sprintf("%d,%d", fx, fy) {
			n++
		}
	}
	return n
}

func fastConfig() Config {
	return Config{Duration: time.Second, Bonus: bonus, Grace: 400 * time.Millisecond, RoundTimeout: 300 * time.Millisecond, ConnectTimeout: 2 * time.Second, Seed: 1}
}

func runWith(t *testing.T, w *world, cfg Config, bots int) Report {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(w.serve))
	t.Cleanup(srv.Close)
	var sessions []Session
	for i := range bots {
		s, err := client.NewSession(client.SessionConfig{BaseURL: srv.URL, Token: fmt.Sprintf("bot-%d", i), Map: true})
		if err != nil {
			t.Fatal(err)
		}
		sessions = append(sessions, s)
	}
	rep, err := Run(context.Background(), cfg, sessions, nil)
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func TestThreeBotsPutOutOneFireTogetherWithASingleBonus(t *testing.T) {
	w := newWorld(8)
	rep := runWith(t, w, fastConfig(), 3)
	if rep.FiresSeen != 1 || rep.Extinguished != 1 || !rep.Passed() {
		t.Fatalf("unexpected report: %+v", rep)
	}
	f := rep.Fires[0]
	if f.Rounds != 1 || f.Drops != 3 || f.Applied != 3 || len(f.BonusBots) != 1 {
		t.Fatalf("unexpected fire %+v", f)
	}
	if n := w.dropsOnFire(); n != 3 {
		t.Fatalf("drops on the burning tile = %d, want exactly one per bot", n)
	}
	if rep.AcceptedDrops == 0 || rep.HumidityContributed == 0 {
		t.Fatalf("counters not reported: %+v", rep)
	}
}

func TestASingleBotKeepsDroppingUntilTheFireIsOut(t *testing.T) {
	w := newWorld(8)
	cfg := fastConfig()
	cfg.Duration = 3 * time.Second
	rep := runWith(t, w, cfg, 1)
	if rep.Extinguished != 1 || rep.Fires[0].Rounds != need || !rep.Passed() {
		t.Fatalf("unexpected report: %+v", rep)
	}
}

func TestWithoutFiresItPassesAndSaysSo(t *testing.T) {
	rep := runWith(t, newWorld(0), fastConfig(), 2)
	if rep.FiresSeen != 0 || !rep.Passed() || !strings.Contains(FormatSummary(rep), "no hubo incendios en la ventana") {
		t.Fatalf("unexpected report: %+v", rep)
	}
}

func TestAFireThatBurnsOutAfterBeingAttackedFails(t *testing.T) {
	w := newWorld(8)
	w.ignoreDrops, w.burnAfter = true, 12
	cfg := fastConfig()
	cfg.Duration = 1500 * time.Millisecond
	rep := runWith(t, w, cfg, 3)
	if rep.BurnedOut != 1 || rep.Passed() {
		t.Fatalf("a fire that burned out must fail: %+v", rep)
	}
}

func TestAFireStillBurningAtTheEndFails(t *testing.T) {
	w := newWorld(8)
	w.ignoreDrops = true
	rep := runWith(t, w, fastConfig(), 3)
	if rep.Unresolved != 1 || rep.Passed() {
		t.Fatalf("a fire that never goes out must fail: %+v", rep)
	}
}

func TestABonusGivenToEveryBotFails(t *testing.T) {
	w := newWorld(8)
	w.bonusToAll = true
	rep := runWith(t, w, fastConfig(), 3)
	if rep.SingleBonusOK || rep.Passed() {
		t.Fatalf("a repeated bonus must fail: %+v", rep)
	}
}

func TestSeedPlantsDropsOnItsOwnTilesFirst(t *testing.T) {
	w := newWorld(0)
	cfg := fastConfig()
	cfg.SeedPlants = true
	cfg.SeedTiles, cfg.SeedDrops, cfg.SeedPace = 2, 4, 5*time.Millisecond
	rep := runWith(t, w, cfg, 2)
	w.mu.Lock()
	n := len(w.drops)
	w.mu.Unlock()
	if n != 2*2*4 {
		t.Fatalf("seed drops = %d, want 16", n)
	}
	if !rep.Passed() {
		t.Fatalf("unexpected report: %+v", rep)
	}
}

func TestRunFailsWhenNoBotConnects(t *testing.T) {
	s, err := client.NewSession(client.SessionConfig{BaseURL: "http://127.0.0.1:1", Token: "t", Map: true})
	if err != nil {
		t.Fatal(err)
	}
	cfg := fastConfig()
	cfg.ConnectTimeout = 150 * time.Millisecond
	if _, err := Run(context.Background(), cfg, []Session{s}, nil); err == nil {
		t.Fatal("expected an error")
	}
}

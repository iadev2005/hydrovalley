package neighbors

import (
	"math/rand/v2"
	"testing"
	"time"

	"github.com/iadev2005/hydrovalley/src/bots/internal/client"
)

func rng(seed uint64) *rand.Rand { return rand.New(rand.NewPCG(seed, 1)) }

// valley arma un valle w x h con la etapa y la humedad dadas por casilla.
func valley(w, h uint32, stage uint32, hum uint64) client.State {
	st := client.State{Width: w, Height: h}
	for range w * h {
		st.Stages = append(st.Stages, stage)
		st.Humidity = append(st.Humidity, client.U64(hum))
	}
	return st
}

func TestNextDelayStaysWithinJitterAndIsSeeded(t *testing.T) {
	r := rng(1)
	for range 200 {
		d := NextDelay(r, 4*time.Second, 0.5)
		if d < 2*time.Second || d > 6*time.Second {
			t.Fatalf("delay %v outside [2s, 6s]", d)
		}
	}
	a, b := rng(7), rng(7)
	for range 20 {
		if NextDelay(a, time.Second, 0.5) != NextDelay(b, time.Second, 0.5) {
			t.Fatal("same seed must give the same delays")
		}
	}
	if d := NextDelay(rng(1), time.Second, 0); d != time.Second {
		t.Fatalf("no jitter must give the interval, got %v", d)
	}
}

func TestPickTilePrefersDryAndGrowingOverTrees(t *testing.T) {
	st := valley(4, 4, 5, 0) // todo árboles
	st.Stages[5] = 1         // una casilla de tierra seca, índice 5 = (1, 1)
	r := rng(3)
	hits := 0
	const n = 2000
	for range n {
		x, y, ok := PickTile(r, st, false)
		if !ok {
			t.Fatal("no tile")
		}
		if x == 1 && y == 1 {
			hits++
		}
	}
	// Peso 4 contra 1 en 15 más: esperado ~4/19 = 21 %, muy por encima del 6 % uniforme.
	if hits < n*15/100 {
		t.Fatalf("dry tile picked %d/%d times, expected clear preference", hits, n)
	}
}

func TestPickTileFavoursMoistTilesThatAreStillGrowing(t *testing.T) {
	st := valley(4, 4, 2, 0)
	st.Humidity[10] = 3 // (2, 2)
	r := rng(5)
	hits := 0
	for range 2000 {
		x, y, _ := PickTile(r, st, false)
		if x == 2 && y == 2 {
			hits++
		}
	}
	// Peso 6 contra 2 en 15 más: esperado 6/36 = 17 %; parejo sería 6 %.
	if hits < 2000*12/100 {
		t.Fatalf("moist tile picked only %d times", hits)
	}
}

func TestPickTileAvoidsInstalledTilesAndHandlesNoMap(t *testing.T) {
	st := valley(2, 1, 1, 0)
	st.Installations = []client.Installation{{X: 0, Y: 0, Installation: "sprinkler"}}
	for range 50 {
		x, y, ok := PickTile(rng(9), st, true)
		if !ok || x != 1 || y != 0 {
			t.Fatalf("got (%d,%d,%v), want (1,0,true)", x, y, ok)
		}
	}
	if _, _, ok := PickTile(rng(1), client.State{}, false); ok {
		t.Fatal("a state without map must not yield a tile")
	}
	st.Installations = append(st.Installations, client.Installation{X: 1, Y: 0})
	if _, _, ok := PickTile(rng(1), st, true); ok {
		t.Fatal("all tiles installed must not yield a tile")
	}
}

func TestPickTileIsDeterministicForASeed(t *testing.T) {
	st := valley(8, 8, 1, 0)
	a, b := rng(11), rng(11)
	for range 30 {
		x1, y1, _ := PickTile(a, st, false)
		x2, y2, _ := PickTile(b, st, false)
		if x1 != x2 || y1 != y2 {
			t.Fatal("same seed must pick the same tiles")
		}
	}
}

func TestDecideWaitsWithoutMapOrWater(t *testing.T) {
	cfg := Config{BuildChance: 0}
	if a := Decide(rng(1), nil, cfg); a.Kind != ActionNone {
		t.Fatalf("no state: %+v", a)
	}
	if a := Decide(rng(1), &client.State{}, cfg); a.Kind != ActionNone {
		t.Fatalf("no map: %+v", a)
	}
	st := valley(2, 2, 1, 0)
	st.Player = &client.Player{Water: 0}
	if a := Decide(rng(1), &st, cfg); a.Kind != ActionNone {
		t.Fatalf("empty tank: %+v", a)
	}
}

func TestDecideDropsWhenPlayerNotYetInWorldOrHasWater(t *testing.T) {
	st := valley(2, 2, 1, 0)
	if a := Decide(rng(1), &st, Config{}); a.Kind != ActionDrop {
		t.Fatalf("player absent: %+v", a)
	}
	st.Player = &client.Player{Water: 500}
	if a := Decide(rng(1), &st, Config{}); a.Kind != ActionDrop {
		t.Fatalf("with water: %+v", a)
	}
}

func TestDecideBuildsSprinklerOnlyWithEnoughLeaves(t *testing.T) {
	st := valley(3, 3, 1, 0)
	st.Player = &client.Player{Water: 500, Leaves: 10, NextSprinklerPrice: 10}
	a := Decide(rng(1), &st, Config{BuildChance: 1})
	if a.Kind != ActionPlace || a.Installation != "sprinkler" {
		t.Fatalf("want sprinkler placement, got %+v", a)
	}
	st.Player.Leaves = 9
	if a := Decide(rng(1), &st, Config{BuildChance: 1}); a.Kind != ActionDrop {
		t.Fatalf("not enough leaves must drop, got %+v", a)
	}
	st.Player.Leaves, st.Player.NextSprinklerPrice = 10, 0
	if a := Decide(rng(1), &st, Config{BuildChance: 1}); a.Kind != ActionDrop {
		t.Fatalf("unknown price must drop, got %+v", a)
	}
	st.Player.NextSprinklerPrice = 10
	if a := Decide(rng(1), &st, Config{BuildChance: 0}); a.Kind != ActionDrop {
		t.Fatalf("zero build chance must drop, got %+v", a)
	}
}

func TestDecideCanBuildEvenWithEmptyTank(t *testing.T) {
	st := valley(3, 3, 1, 0)
	st.Player = &client.Player{Water: 0, Leaves: 10, NextSprinklerPrice: 10}
	if a := Decide(rng(1), &st, Config{BuildChance: 1}); a.Kind != ActionPlace {
		t.Fatalf("buying needs leaves, not water: %+v", a)
	}
}

package firefighter

import (
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/iadev2005/hydrovalley/src/bots/internal/client"
)

func fire(x, y uint32) client.Fire { return client.Fire{X: x, Y: y} }

func TestNextTargetKeepsTheCurrentFireWhileItBurns(t *testing.T) {
	fires := []client.Fire{fire(9, 9), fire(2, 3), fire(5, 1)}
	cur := Tile{X: 9, Y: 9}
	got, ok := NextTarget(fires, &cur)
	if !ok || got != cur {
		t.Fatalf("got %+v %v, want the current tile", got, ok)
	}
}

func TestNextTargetPicksTheFirstByRowThenColumnWhenThereIsNoCurrent(t *testing.T) {
	fires := []client.Fire{fire(9, 9), fire(2, 3), fire(5, 1), fire(1, 3)}
	got, ok := NextTarget(fires, nil)
	if !ok || got != (Tile{X: 5, Y: 1}) {
		t.Fatalf("got %+v %v", got, ok)
	}
	gone := Tile{X: 0, Y: 0}
	got, _ = NextTarget(fires, &gone)
	if got != (Tile{X: 5, Y: 1}) {
		t.Fatalf("a current tile that no longer burns must be replaced, got %+v", got)
	}
	if _, ok := NextTarget(nil, nil); ok {
		t.Fatal("no fires, no target")
	}
}

func TestClassifyUsesTheTileStage(t *testing.T) {
	const w = 2
	stages := []uint32{1, 2, 3, 1} // 1 = tierra seca
	cases := []struct {
		t    Tile
		want string
	}{
		{Tile{X: 0, Y: 0}, OutcomeBurnedOut},
		{Tile{X: 1, Y: 0}, OutcomeExtinguished},
		{Tile{X: 0, Y: 1}, OutcomeExtinguished},
	}
	for _, c := range cases {
		if got := Classify(stages, w, c.t); got != c.want {
			t.Errorf("Classify(%+v) = %s, want %s", c.t, got, c.want)
		}
	}
	if got := Classify(nil, w, Tile{}); got != OutcomeUnknown {
		t.Errorf("without a map the outcome is unknown, got %s", got)
	}
}

func TestBonusBots(t *testing.T) {
	got := BonusBots([]uint64{10, 10, 10}, []uint64{10, 15, 22}, 5)
	if !slices.Equal(got, []int{1, 2}) {
		t.Fatalf("got %v", got)
	}
	if got := BonusBots([]uint64{10}, []uint64{4}, 5); len(got) != 0 {
		t.Fatalf("a drop in leaves is not a bonus: %v", got)
	}
	if got := BonusBots([]uint64{1, 2}, []uint64{3}, 1); len(got) != 1 {
		t.Fatalf("mismatched lengths use the shorter: %v", got)
	}
}

func TestSeedTilesAreDistinctAndInsideTheValley(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	tiles := SeedTiles(r, 32, 32, 3)
	if len(tiles) != 3 {
		t.Fatalf("got %d tiles", len(tiles))
	}
	seen := map[Tile]bool{}
	for _, tl := range tiles {
		if tl.X >= 32 || tl.Y >= 32 || seen[tl] {
			t.Fatalf("bad tile %+v in %v", tl, tiles)
		}
		seen[tl] = true
	}
	if got := SeedTiles(r, 1, 1, 3); len(got) != 1 {
		t.Fatalf("a 1x1 valley has one tile, got %v", got)
	}
}

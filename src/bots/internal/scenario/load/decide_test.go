package load

import (
	"math/rand/v2"
	"testing"
	"time"
)

func TestNextDelayStaysWithinTheJitterBand(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	lo, hi := 250*time.Millisecond, 250*time.Millisecond
	for range 5000 {
		d := NextDelay(r, 250*time.Millisecond, 50*time.Millisecond)
		if d < 200*time.Millisecond || d > 300*time.Millisecond {
			t.Fatalf("delay %v outside 250ms ± 50ms", d)
		}
		lo, hi = min(lo, d), max(hi, d)
	}
	if lo == hi {
		t.Fatal("jitter must vary the delay")
	}
}

func TestNextDelayWithoutJitterIsConstantAndNeverNegative(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	if d := NextDelay(r, 250*time.Millisecond, 0); d != 250*time.Millisecond {
		t.Fatalf("no jitter = %v", d)
	}
	for range 100 {
		if d := NextDelay(r, 10*time.Millisecond, 50*time.Millisecond); d <= 0 {
			t.Fatalf("delay %v must stay positive", d)
		}
	}
}

func TestPickTileCoversTheWholeValleyAndNeverLeavesIt(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	seen := map[[2]uint32]bool{}
	for range 20000 {
		x, y := PickTile(r, 4, 3)
		if x >= 4 || y >= 3 {
			t.Fatalf("tile (%d,%d) outside 4x3", x, y)
		}
		seen[[2]uint32{x, y}] = true
	}
	if len(seen) != 12 {
		t.Fatalf("only %d of 12 tiles were picked", len(seen))
	}
	if x, y := PickTile(r, 0, 0); x != 0 || y != 0 {
		t.Fatalf("empty valley = (%d,%d)", x, y)
	}
}

func TestSameSeedSameTiles(t *testing.T) {
	a, b := rand.New(rand.NewPCG(9, 1)), rand.New(rand.NewPCG(9, 1))
	for range 50 {
		ax, ay := PickTile(a, 32, 32)
		bx, by := PickTile(b, 32, 32)
		if ax != bx || ay != by {
			t.Fatal("same seed must give the same tiles")
		}
	}
}

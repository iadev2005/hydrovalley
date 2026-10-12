package ratelimit

import (
	"testing"
	"time"
)

var t0 = time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)

func TestAllowsUpToLimitInsideWindow(t *testing.T) {
	w := NewSlidingWindow(15, time.Second)
	for i := range 15 {
		if !w.Allow(t0.Add(time.Duration(i) * time.Millisecond)) {
			t.Fatalf("drop %d rejected, want allowed", i+1)
		}
	}
	if w.Allow(t0.Add(20 * time.Millisecond)) {
		t.Fatal("16th drop inside the window allowed, want rejected")
	}
}

func TestWindowSlides(t *testing.T) {
	w := NewSlidingWindow(15, time.Second)
	for range 15 {
		w.Allow(t0)
	}
	if w.Allow(t0.Add(999 * time.Millisecond)) {
		t.Fatal("allowed before the oldest entry left the window")
	}
	if !w.Allow(t0.Add(time.Second)) {
		t.Fatal("rejected once the oldest entry left the window")
	}
}

func TestRejectedAttemptsDoNotConsumeCapacity(t *testing.T) {
	w := NewSlidingWindow(2, time.Second)
	w.Allow(t0)
	w.Allow(t0)
	for i := range 10 {
		w.Allow(t0.Add(time.Duration(i+1) * 50 * time.Millisecond))
	}
	if !w.Allow(t0.Add(time.Second)) {
		t.Fatal("rejected attempts must not delay the window")
	}
}

// CP-03: 50 gotas por segundo durante 10 s → se aceptan 15 por segundo.
func TestAutoclickerAcceptsFifteenPerSecond(t *testing.T) {
	w := NewSlidingWindow(15, time.Second)
	accepted := 0
	for i := range 500 {
		if w.Allow(t0.Add(time.Duration(i) * 20 * time.Millisecond)) {
			accepted++
		}
	}
	if accepted != 150 {
		t.Fatalf("accepted %d drops in 10 s, want 150", accepted)
	}
}

func TestNeverMoreThanLimitInAnyWindow(t *testing.T) {
	w := NewSlidingWindow(15, time.Second)
	var allowed []time.Time
	for i := range 3000 {
		now := t0.Add(time.Duration(i*7) * time.Millisecond)
		if w.Allow(now) {
			allowed = append(allowed, now)
		}
	}
	for i := 15; i < len(allowed); i++ {
		if allowed[i].Sub(allowed[i-15]) < time.Second {
			t.Fatalf("16 drops within %v", allowed[i].Sub(allowed[i-15]))
		}
	}
}

func TestPermitsDoesNotRecord(t *testing.T) {
	w := NewSlidingWindow(1, time.Second)
	for range 5 {
		if !w.Permits(t0) {
			t.Fatal("Permits on an empty window must be true")
		}
	}
	w.Record(t0)
	if w.Permits(t0.Add(999 * time.Millisecond)) {
		t.Fatal("Permits must be false while the window is full")
	}
	if !w.Permits(t0.Add(time.Second)) {
		t.Fatal("Permits must be true once the oldest entry left the window")
	}
}

func TestActive(t *testing.T) {
	w := NewSlidingWindow(3, time.Second)
	if w.Active(t0) {
		t.Fatal("an empty window is not active")
	}
	w.Allow(t0)
	w.Allow(t0.Add(500 * time.Millisecond))
	if !w.Active(t0.Add(1400 * time.Millisecond)) {
		t.Fatal("the newest entry is still inside the window")
	}
	if w.Active(t0.Add(1500 * time.Millisecond)) {
		t.Fatal("every entry left the window")
	}
}

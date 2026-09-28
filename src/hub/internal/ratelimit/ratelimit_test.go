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

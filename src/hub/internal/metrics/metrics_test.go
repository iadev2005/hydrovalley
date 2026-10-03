package metrics

import (
	"testing"
	"time"
)

var t0 = time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)

func TestRateAveragesOverTheWindow(t *testing.T) {
	r := NewRate(10)
	for i := 0; i < 20; i++ {
		r.Add(t0.Add(time.Duration(i) * 100 * time.Millisecond)) // 20 eventos en 2 s
	}
	if got := r.PerSecond(t0.Add(2 * time.Second)); got != 2.0 {
		t.Errorf("PerSecond = %v, want 2 (20 events / 10 s)", got)
	}
}

func TestRateForgetsEventsOutsideTheWindow(t *testing.T) {
	r := NewRate(10)
	r.Add(t0)
	if got := r.PerSecond(t0.Add(9 * time.Second)); got != 0.1 {
		t.Errorf("inside the window = %v, want 0.1", got)
	}
	if got := r.PerSecond(t0.Add(10 * time.Second)); got != 0 {
		t.Errorf("after the window = %v, want 0", got)
	}
}

func TestRateReusesBucketsAfterAWrap(t *testing.T) {
	r := NewRate(10)
	r.Add(t0)
	r.Add(t0.Add(10 * time.Second)) // mismo hueco del anillo, otro segundo
	if got := r.PerSecond(t0.Add(10 * time.Second)); got != 0.1 {
		t.Errorf("PerSecond = %v, want 0.1 (only the recent event)", got)
	}
}

func TestSamplesPercentilesUseNearestRank(t *testing.T) {
	s := NewSamples(100, 0)
	for i := 1; i <= 100; i++ {
		s.Add(t0, time.Duration(i)*time.Millisecond)
	}
	sum := s.Summary(t0)
	if sum.Count != 100 || sum.P50 != 50*time.Millisecond || sum.P95 != 95*time.Millisecond {
		t.Errorf("summary = %+v, want count 100, p50 50ms, p95 95ms", sum)
	}
	if sum.Last != 100*time.Millisecond {
		t.Errorf("Last = %v, want 100ms", sum.Last)
	}
}

func TestSamplesEmpty(t *testing.T) {
	if sum := NewSamples(10, time.Second).Summary(t0); sum != (Summary{}) {
		t.Errorf("empty summary = %+v, want zero", sum)
	}
}

func TestSamplesKeepOnlyTheLastCapacity(t *testing.T) {
	s := NewSamples(3, 0)
	for i := 1; i <= 5; i++ {
		s.Add(t0, time.Duration(i)*time.Second)
	}
	sum := s.Summary(t0)
	if sum.Count != 3 || sum.P50 != 4*time.Second || sum.P95 != 5*time.Second {
		t.Errorf("summary = %+v, want the last 3 samples (3s, 4s, 5s)", sum)
	}
}

func TestSamplesDropTheOnesOutsideTheWindow(t *testing.T) {
	s := NewSamples(10, 10*time.Second)
	s.Add(t0, time.Second)
	s.Add(t0.Add(8*time.Second), 3*time.Second)
	sum := s.Summary(t0.Add(11 * time.Second))
	if sum.Count != 1 || sum.P50 != 3*time.Second {
		t.Errorf("summary = %+v, want only the 3s sample", sum)
	}
	if sum.Last != 3*time.Second {
		t.Errorf("Last = %v", sum.Last)
	}
}

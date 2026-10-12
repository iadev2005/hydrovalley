package breaker

import (
	"testing"
	"time"
)

var t0 = time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)

func newBreaker() *Breaker { return New(3, time.Second) }

func TestClosedAllowsCalls(t *testing.T) {
	b := newBreaker()
	if !b.Allow(t0) {
		t.Fatal("closed breaker must allow calls")
	}
	if b.State() != Closed {
		t.Fatalf("state = %v, want closed", b.State())
	}
}

func TestOpensAfterThreeConsecutiveFailures(t *testing.T) {
	b := newBreaker()
	for i := range 2 {
		b.Allow(t0)
		if opened := b.Failure(t0); opened {
			t.Fatalf("failure %d opened the breaker", i+1)
		}
	}
	if b.State() != Closed {
		t.Fatal("two failures must keep it closed")
	}
	b.Allow(t0)
	if opened := b.Failure(t0); !opened {
		t.Fatal("third consecutive failure must report the opening")
	}
	if b.State() != Open {
		t.Fatalf("state = %v, want open", b.State())
	}
	if b.Allow(t0.Add(999 * time.Millisecond)) {
		t.Fatal("open breaker allowed a call before the probe interval")
	}
}

func TestSuccessResetsConsecutiveFailures(t *testing.T) {
	b := newBreaker()
	b.Failure(t0)
	b.Failure(t0)
	b.Success()
	b.Failure(t0)
	b.Failure(t0)
	if b.State() != Closed {
		t.Fatal("failures separated by a success are not consecutive")
	}
}

func TestProbeEverySecondWhileOpen(t *testing.T) {
	b := newBreaker()
	for range 3 {
		b.Failure(t0)
	}
	probeAt := t0.Add(time.Second)
	if !b.Allow(probeAt) {
		t.Fatal("open breaker must allow a probe after 1 s")
	}
	if b.State() != HalfOpen {
		t.Fatalf("state = %v, want half-open", b.State())
	}
	if b.Allow(probeAt.Add(time.Millisecond)) {
		t.Fatal("only one probe at a time")
	}
	if opened := b.Failure(probeAt); !opened {
		t.Fatal("failed probe must reopen the breaker")
	}
	if b.Allow(probeAt.Add(500 * time.Millisecond)) {
		t.Fatal("reopened breaker must wait another second")
	}
	if !b.Allow(probeAt.Add(time.Second)) {
		t.Fatal("second probe must be allowed 1 s after the failed one")
	}
	b.Success()
	if b.State() != Closed {
		t.Fatalf("state = %v, want closed after a successful probe", b.State())
	}
	if !b.Allow(probeAt.Add(time.Second)) {
		t.Fatal("closed breaker must allow calls")
	}
}

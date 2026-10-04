package hub

import (
	"context"
	"testing"
	"time"

	worldv1 "github.com/iadev2005/hydrovalley/gen/go/hydrovalley/world/v1"
	"github.com/iadev2005/hydrovalley/src/hub/internal/auth"
)

func connectAna(h *harness) *Session { return h.hub.Connect(ana, auth.RolePlayer, false) }

func TestStatsCountsAdmittedAndAppliedPerSecond(t *testing.T) {
	h := newHarness(t)
	s := connectAna(h)
	for i := 1; i <= 3; i++ {
		h.hub.SubmitDrop(s, drop(i))
	}
	h.hub.SubmitDrop(s, drop(1)) // ya pendiente: no se admite otra vez
	h.tick()
	st := h.hub.Stats()
	if st.AdmittedPerSecond != 0.3 || st.AppliedPerSecond != 0.3 {
		t.Errorf("rates = admitted %v, applied %v, want 0.3 and 0.3", st.AdmittedPerSecond, st.AppliedPerSecond)
	}
	if st.Sessions != 1 || st.AcceptedDrops != 3 || st.Tick != 1 || st.QueueLen != 0 || st.Breaker != "closed" || st.Paused {
		t.Errorf("stats = %+v", st)
	}
}

func TestStatsLatencyGoesFromAdmissionToFinalResult(t *testing.T) {
	h := newHarness(t)
	s := connectAna(h)
	h.world.set(func(w *fakeWorld) {
		w.hook = func(context.Context, *worldv1.StepRequest) { h.clock.Advance(20 * time.Millisecond) }
	})
	h.hub.SubmitDrop(s, drop(1))
	h.tick() // +100 ms antes del Step, +20 ms dentro
	st := h.hub.Stats()
	if st.Latency.Count != 1 || st.Latency.P50 != 120*time.Millisecond {
		t.Errorf("latency = %+v, want 1 sample of 120ms", st.Latency)
	}
	if st.Step.Last != 20*time.Millisecond || st.Step.P95 != 20*time.Millisecond || st.Step.FailuresTotal != 0 {
		t.Errorf("step = %+v, want 20ms and no failures", st.Step)
	}
}

func TestStatsRejectedCountsLatencyButNotApplied(t *testing.T) {
	h := newHarness(t)
	s := connectAna(h)
	h.world.set(func(w *fakeWorld) {
		w.statuses[uuidN(1)] = worldv1.CommandStatus_COMMAND_STATUS_REJECTED
	})
	h.hub.SubmitDrop(s, drop(1))
	h.tick()
	st := h.hub.Stats()
	if st.Latency.Count != 1 || st.AppliedPerSecond != 0 || st.AdmittedPerSecond != 0.1 {
		t.Errorf("stats = %+v", st)
	}
}

func TestStatsExpiredCountsLatency(t *testing.T) {
	h := newHarness(t)
	s := connectAna(h)
	h.hub.SubmitDrop(s, drop(1))
	h.clock.Advance(31 * time.Second)
	h.tick()
	st := h.hub.Stats()
	if st.Latency.Count != 1 || st.Latency.P50 < 31*time.Second {
		t.Errorf("latency = %+v, want one sample over 31s", st.Latency)
	}
	if st.AppliedPerSecond != 0 {
		t.Errorf("applied = %v, want 0", st.AppliedPerSecond)
	}
}

func TestStatsDuplicateTreatedAsAppliedCountsAsApplied(t *testing.T) {
	h := newHarness(t)
	s := connectAna(h)
	h.world.set(func(w *fakeWorld) { w.fail = true })
	h.hub.SubmitDrop(s, drop(1))
	h.tick() // Step fallido: el comando vuelve a la cola
	h.world.set(func(w *fakeWorld) {
		w.fail = false
		w.statuses[uuidN(1)] = worldv1.CommandStatus_COMMAND_STATUS_DUPLICATE
	})
	h.tick()
	st := h.hub.Stats()
	if st.AppliedPerSecond != 0.1 {
		t.Errorf("applied = %v, want 0.1 (DUPLICATE after a failed Step counts as applied)", st.AppliedPerSecond)
	}
}

func TestStatsPlainDuplicateIsNotApplied(t *testing.T) {
	h := newHarness(t)
	s := connectAna(h)
	h.world.set(func(w *fakeWorld) { w.statuses[uuidN(1)] = worldv1.CommandStatus_COMMAND_STATUS_DUPLICATE })
	h.hub.SubmitDrop(s, drop(1))
	h.tick()
	st := h.hub.Stats()
	if st.AppliedPerSecond != 0 || st.Latency.Count != 1 {
		t.Errorf("stats = %+v, want applied 0 and one latency sample", st)
	}
}

func TestStatsRatesSlideOutOfTheWindow(t *testing.T) {
	h := newHarness(t)
	s := connectAna(h)
	h.hub.SubmitDrop(s, drop(1))
	h.tick()
	h.clock.Advance(11 * time.Second)
	st := h.hub.Stats()
	if st.AdmittedPerSecond != 0 || st.AppliedPerSecond != 0 || st.Latency.Count != 0 {
		t.Errorf("stats after 11 s = %+v, want empty window", st)
	}
}

func TestStatsStepFailuresAndDuration(t *testing.T) {
	h := newHarness(t)
	h.world.set(func(w *fakeWorld) {
		w.fail = true
		w.hook = func(context.Context, *worldv1.StepRequest) { h.clock.Advance(50 * time.Millisecond) }
	})
	h.tick()
	h.tick()
	st := h.hub.Stats()
	if st.Step.FailuresTotal != 2 || st.Step.Last != 50*time.Millisecond || !st.Paused {
		t.Errorf("step = %+v paused=%v, want 2 failures, last 50ms, paused", st.Step, st.Paused)
	}
}

func TestStatsWorldFiguresComeFromTheLatestState(t *testing.T) {
	h := newHarness(t)
	h.world.set(func(w *fakeWorld) {
		w.humidity = 77
		w.mangle = func(resp *worldv1.StepResponse) {
			resp.State.Tiles = []*worldv1.Tile{
				{Humidity: 10}, {Humidity: 5, Fire: &worldv1.Fire{}}, {Fire: &worldv1.Fire{}}, {},
			}
		}
	})
	h.tick()
	st := h.hub.Stats()
	if st.HumidityTotal != 15 || st.HumidityContributed != 77 || st.ActiveFires != 2 || st.BootID != "boot-a" {
		t.Errorf("stats = %+v", st)
	}
	if !st.At.Equal(h.clock.Now()) {
		t.Errorf("At = %v, want the hub clock %v", st.At, h.clock.Now())
	}
}

func TestStatsBeforeAnyStepHasNoWorld(t *testing.T) {
	h := newHarness(t)
	st := h.hub.Stats()
	if st.HumidityTotal != 0 || st.ActiveFires != 0 || st.BootID != "" || st.Sessions != 0 {
		t.Errorf("stats = %+v", st)
	}
}

func TestStatsRestoredTickFollowsTheLatestStep(t *testing.T) {
	h := newHarness(t)
	if st := h.hub.Stats(); st.RestoredTick != 0 {
		t.Fatalf("restored tick before any Step = %d, want 0", st.RestoredTick)
	}
	h.world.set(func(w *fakeWorld) { w.restored = 50 })
	h.tick()
	if st := h.hub.Stats(); st.RestoredTick != 50 || st.BootID != "boot-a" {
		t.Fatalf("stats = %+v, want restored tick 50 with boot-a", st)
	}
	// Reinicio de Rust: el nuevo arranque informa otro tick restaurado.
	h.world.set(func(w *fakeWorld) { w.bootID, w.restored, w.tick = "boot-b", 100, 100 })
	h.tick()
	if st := h.hub.Stats(); st.RestoredTick != 100 || st.BootID != "boot-b" {
		t.Fatalf("stats = %+v, want restored tick 100 with boot-b", st)
	}
}

package hub

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	worldv1 "github.com/iadev2005/hydrovalley/gen/go/hydrovalley/world/v1"
	"github.com/iadev2005/hydrovalley/src/hub/internal/breaker"
)

func actionIDs(cmds []*worldv1.Command) []string {
	out := make([]string, len(cmds))
	for i, c := range cmds {
		out[i] = c.GetActionId()
	}
	return out
}

func TestEmptyTickStillCallsStep(t *testing.T) {
	h := newHarness(t)
	h.tick()
	if n := len(h.world.calls()); n != 1 {
		t.Fatalf("Step calls = %d, want 1 (the valley advances with no commands, RF-28)", n)
	}
	if st := h.hub.Status(); st.Tick != 1 || st.Paused {
		t.Fatalf("status = %+v", st)
	}
}

func TestDrainsInBatchesOf500InAdmissionOrder(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.DropsPerSecond = 2000 })
	s := h.hub.Connect(ana, true)
	var want []string
	for i := range 1200 {
		h.hub.SubmitDrop(s, drop(i))
		want = append(want, uuidN(i))
	}
	h.tick()
	h.tick()
	h.tick()
	calls := h.world.calls()
	if len(calls) != 3 {
		t.Fatalf("Step calls = %d", len(calls))
	}
	var got []string
	for i, size := range []int{500, 500, 200} {
		if n := len(calls[i].GetCommands()); n != size {
			t.Fatalf("batch %d has %d commands, want %d", i, n, size)
		}
		got = append(got, actionIDs(calls[i].GetCommands())...)
	}
	if !slices.Equal(got, want) {
		t.Fatal("commands must reach Step in admission order")
	}
}

func TestStepHasA200msDeadline(t *testing.T) {
	h := newHarness(t)
	var remaining time.Duration
	var hasDeadline bool
	h.world.set(func(w *fakeWorld) {
		w.hook = func(ctx context.Context, _ *worldv1.StepRequest) {
			var deadline time.Time
			deadline, hasDeadline = ctx.Deadline()
			remaining = time.Until(deadline)
		}
	})
	h.tick()
	if !hasDeadline || remaining <= 0 || remaining > 200*time.Millisecond {
		t.Fatalf("deadline present=%v remaining=%v, want <= 200ms", hasDeadline, remaining)
	}
}

func TestFailedStepRequeuesAtFrontAndPauses(t *testing.T) {
	h := newHarness(t)
	s := h.hub.Connect(ana, true)
	h.hub.SubmitDrop(s, drop(1))
	h.world.set(func(w *fakeWorld) {
		w.fail = true
		// Llega una gota nueva mientras el Step que va a fallar sigue en curso.
		w.hook = func(context.Context, *worldv1.StepRequest) {
			h.hub.SubmitDrop(s, drop(2))
			w.hook = nil
		}
	})
	h.tick()

	st := h.hub.Status()
	if !st.Paused || st.Tick != 0 || st.QueueLen != 2 {
		t.Fatalf("after a failed Step status = %+v, want paused with the command back in the queue", st)
	}
	if got := h.hub.SubmitDrop(s, drop(1)); got != IgnoredPending {
		t.Fatalf("a requeued command is still pending; outcome = %v", got)
	}
	if r := results(drain(s)); len(r) != 0 {
		t.Fatalf("a failed Step sends no result, got %v", statusesOf(r))
	}

	h.hub.SubmitDrop(s, drop(3))
	h.world.set(func(w *fakeWorld) { w.fail = false })
	h.tick()
	if got := actionIDs(h.world.lastCommands()); !slices.Equal(got, []string{uuidN(1), uuidN(2), uuidN(3)}) {
		t.Fatalf("Step commands = %v, want the failed one first", got)
	}
	if st := h.hub.Status(); st.Paused || st.Tick != 1 {
		t.Fatalf("after a successful Step status = %+v", st)
	}
}

func TestInvalidStepResponseCountsAsFailure(t *testing.T) {
	h := newHarness(t)
	s := h.hub.Connect(ana, true)
	h.hub.SubmitDrop(s, drop(1))
	h.world.set(func(w *fakeWorld) { w.mangle = func(r *worldv1.StepResponse) { r.Results = nil } })
	h.tick()
	if st := h.hub.Status(); !st.Paused || st.QueueLen != 1 {
		t.Fatalf("status = %+v, want paused with the command requeued", st)
	}
}

func TestExpiresCommandsOlderThan30Seconds(t *testing.T) {
	h := newHarness(t)
	s := h.hub.Connect(ana, true)
	h.world.set(func(w *fakeWorld) { w.fail = true })
	h.hub.SubmitDrop(s, drop(1))
	h.clock.Advance(15 * time.Second)
	h.hub.SubmitDrop(s, drop(2))

	h.clock.Advance(15*time.Second - 100*time.Millisecond)
	h.tick() // exactamente 30 s: todavía no caduca
	if r := results(drain(s)); len(r) != 0 {
		t.Fatalf("at exactly 30 s nothing expires, got %v", statusesOf(r))
	}
	h.clock.Advance(time.Millisecond)
	h.hub.Tick(context.Background())
	if got := statusesOf(results(drain(s))); !slices.Equal(got, []string{"expired:" + uuidN(1)}) {
		t.Fatalf("results = %v", got)
	}
	if st := h.hub.Status(); st.QueueLen != 1 {
		t.Fatalf("queue length = %d, want 1", st.QueueLen)
	}
	if got := h.hub.SubmitDrop(s, drop(1)); got != Admitted {
		t.Fatalf("expired only leaves pending; outcome = %v, want admitted", got)
	}
}

func TestExpiresWhileTheBreakerIsOpen(t *testing.T) {
	h := newHarness(t)
	s := h.hub.Connect(ana, true)
	h.world.set(func(w *fakeWorld) { w.fail = true })
	for range 3 {
		h.tick()
	}
	h.hub.SubmitDrop(s, drop(1))
	h.clock.Advance(31 * time.Second)
	h.hub.Tick(context.Background())
	if got := statusesOf(results(drain(s))); !slices.Equal(got, []string{"expired:" + uuidN(1)}) {
		t.Fatalf("results = %v", got)
	}
}

func TestCircuitBreakerOpensProbesAndCloses(t *testing.T) {
	h := newHarness(t)
	s := h.hub.Connect(ana, true)
	h.tick() // primer Step correcto
	drain(s)
	h.world.set(func(w *fakeWorld) { w.fail = true })

	h.tick()
	h.tick()
	if st := h.hub.Status(); st.Breaker != breaker.Closed.String() || !st.Paused {
		t.Fatalf("after 2 failures status = %+v", st)
	}
	h.tick()
	if st := h.hub.Status(); st.Breaker != breaker.Open.String() {
		t.Fatalf("after 3 failures breaker = %s, want open", st.Breaker)
	}
	notice := states(drain(s))
	if len(notice) != 1 || !notice[0].GetPaused() {
		t.Fatalf("opening the breaker must notify players with a paused state, got %v", notice)
	}
	if h.hub.Status().Ready {
		t.Fatal("not ready while the breaker is open")
	}

	calls := len(h.world.calls())
	for range 9 { // 900 ms: no hay prueba todavía
		h.tick()
	}
	if n := len(h.world.calls()); n != calls {
		t.Fatalf("open breaker called Step %d times before the probe interval", n-calls)
	}
	h.tick() // 1 s después de abrirse: prueba
	if n := len(h.world.calls()); n != calls+1 {
		t.Fatalf("want one probe after 1 s, got %d calls", n-calls)
	}
	if st := h.hub.Status(); st.Breaker != breaker.Open.String() {
		t.Fatalf("failed probe must reopen, breaker = %s", st.Breaker)
	}

	h.world.set(func(w *fakeWorld) { w.fail = false })
	for range 10 {
		h.tick()
	}
	if st := h.hub.Status(); st.Breaker != breaker.Closed.String() || st.Paused {
		t.Fatalf("successful probe must close, status = %+v", st)
	}
	if !h.hub.Status().Ready {
		t.Fatal("ready once the breaker is closed and Step answers")
	}
}

func TestReadyOnlyAfterASuccessfulStep(t *testing.T) {
	h := newHarness(t)
	if h.hub.Status().Ready {
		t.Fatal("not ready before any Step")
	}
	h.world.set(func(w *fakeWorld) { w.fail = true })
	h.tick()
	if h.hub.Status().Ready {
		t.Fatal("not ready after a failed Step")
	}
	h.world.set(func(w *fakeWorld) { w.fail = false })
	h.tick()
	if !h.hub.Status().Ready {
		t.Fatal("ready after a successful Step")
	}
}

func TestBootIDChangePrunesConfirmedAndAcceptedDrops(t *testing.T) {
	h := newHarness(t)
	s := h.hub.Connect(ana, true)
	// Primer boot_id: línea base, sin poda aunque restored_tick = 0.
	h.hub.SubmitDrop(s, drop(1))
	h.tick() // tick 1
	if got := h.hub.SubmitDrop(s, drop(1)); got != RejectedDuplicate {
		t.Fatalf("the first boot_id must not prune; outcome = %v", got)
	}
	h.hub.SubmitDrop(s, drop(2))
	h.tick() // tick 2
	h.hub.SubmitDrop(s, drop(3))
	h.tick() // tick 3
	if h.hub.AcceptedDrops() != 3 {
		t.Fatalf("accepted drops = %d, want 3", h.hub.AcceptedDrops())
	}

	// Rust se reinicia desde la instantánea del tick 1.
	h.world.set(func(w *fakeWorld) { w.bootID, w.restored, w.tick = "boot-b", 1, 1 })
	h.hub.SubmitDrop(s, drop(4))
	h.tick() // tick 2 del nuevo arranque; drop 4 se aplica aquí
	drain(s)

	if got := h.hub.AcceptedDrops(); got != 2 {
		t.Fatalf("accepted drops = %d, want 2 (1 kept + 1 new, 2 lost)", got)
	}
	if got := h.hub.SubmitDrop(s, drop(1)); got != RejectedDuplicate {
		t.Fatalf("drop 1 (tick 1 <= restored_tick) outcome = %v, want duplicate", got)
	}
	if got := h.hub.SubmitDrop(s, drop(4)); got != RejectedDuplicate {
		t.Fatalf("drop 4 was applied after the restart; outcome = %v, want duplicate", got)
	}
	for _, n := range []int{2, 3} {
		if got := h.hub.SubmitDrop(s, drop(n)); got != Admitted {
			t.Fatalf("drop %d was lost with the restart; outcome = %v, want admitted", n, got)
		}
	}
	// La recuperación se aplica una sola vez por arranque.
	h.tick()
	if got := h.hub.SubmitDrop(s, drop(4)); got != RejectedDuplicate {
		t.Fatalf("the same boot_id must not prune again; outcome = %v", got)
	}
	if got := h.hub.AcceptedDrops(); got != 4 {
		t.Fatalf("accepted drops = %d, want 4", got)
	}
}

func TestAcceptedDropsLedgerKeeps600Ticks(t *testing.T) {
	h := newHarness(t)
	s := h.hub.Connect(ana, true)
	h.hub.SubmitDrop(s, drop(1))
	h.tick() // tick 1
	for range 700 {
		h.tick()
	}
	// Reinicio desde el tick 0: la gota del tick 1 ya salió de la ventana de
	// 600 ticks, así que no se puede restar (límite conocido del documento).
	h.world.set(func(w *fakeWorld) { w.bootID, w.restored, w.tick = "boot-b", 0, 0 })
	h.tick()
	if got := h.hub.AcceptedDrops(); got != 1 {
		t.Fatalf("accepted drops = %d, want 1", got)
	}
}

func TestBroadcastsStateOncePerSecond(t *testing.T) {
	h := newHarness(t)
	h.world.set(func(w *fakeWorld) {
		w.players = []*worldv1.PlayerState{{PlayerId: ana, Water: 19, TankCapacity: 20}}
	})
	withMap := h.hub.Connect(ana, true)
	noMap := h.hub.Connect(bob, false)
	for range 20 {
		h.tick()
	}
	got := states(drain(withMap))
	if len(got) != 2 {
		t.Fatalf("got %d state frames in 2 s, want 2", len(got))
	}
	last := got[len(got)-1]
	if last.GetTick() != 11 || last.GetPaused() || last.GetWidth() != 32 || last.GetPlayer().GetWater() != 19 {
		t.Fatalf("state = %v", last)
	}
	other := states(drain(noMap))
	if len(other) != 2 || other[1].GetPlayer() != nil {
		t.Fatalf("bob has no player in the world yet: %v", other)
	}
}

func TestRunNeverOverlapsStepCalls(t *testing.T) {
	h := newHarness(t)
	release := make(chan struct{})
	h.world.set(func(w *fakeWorld) {
		w.hook = func(context.Context, *worldv1.StepRequest) { <-release }
	})
	ticks := make(chan time.Time)
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Go(func() { h.hub.Run(ctx, ticks) })

	ticks <- time.Now() // primer Step, bloqueado
	sent := make(chan struct{})
	go func() {
		ticks <- time.Now() // no se puede entregar mientras Step sigue en curso
		close(sent)
	}()
	select {
	case <-sent:
		t.Fatal("the loop accepted a new tick while Step was in flight")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	<-sent
	cancel()
	wg.Wait()

	h.world.mu.Lock()
	defer h.world.mu.Unlock()
	if h.world.maxInFly != 1 {
		t.Fatalf("max concurrent Step calls = %d, want 1", h.world.maxInFly)
	}
}

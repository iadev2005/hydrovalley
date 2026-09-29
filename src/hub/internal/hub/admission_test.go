package hub

import (
	"context"
	"slices"
	"testing"
	"time"

	worldv1 "github.com/iadev2005/hydrovalley/gen/go/hydrovalley/world/v1"
	"github.com/iadev2005/hydrovalley/src/hub/internal/auth"
	"github.com/iadev2005/hydrovalley/src/hub/internal/frames"
)

func TestDefaultConfigMatchesTheDocument(t *testing.T) {
	c := DefaultConfig()
	if c.TickInterval != 100*time.Millisecond || c.StepTimeout != 200*time.Millisecond || c.BatchSize != 500 || c.QueueCap != 5000 ||
		c.DropsPerSecond != 15 || c.ExpireAfter != 30*time.Second || c.ConfirmedTicks != 600 ||
		c.BreakerThreshold != 3 || c.ProbeInterval != time.Second || c.BroadcastEvery != time.Second ||
		c.LedgerTicks != 600 {
		t.Fatalf("DefaultConfig = %+v", c)
	}
}

func TestAdmittedDropIsSentToStepAndApplied(t *testing.T) {
	h := newHarness(t)
	s := h.hub.Connect(ana, auth.RolePlayer, true)

	if got := h.hub.SubmitDrop(s, frames.Drop{ActionID: uuidN(1), X: 14, Y: 9}); got != Admitted {
		t.Fatalf("outcome = %v, want admitted", got)
	}
	if r := results(drain(s)); len(r) != 0 {
		t.Fatalf("admission must not answer immediately, got %v", statusesOf(r))
	}
	h.tick()

	cmds := h.world.lastCommands()
	if len(cmds) != 1 {
		t.Fatalf("Step got %d commands, want 1", len(cmds))
	}
	c := cmds[0]
	if c.GetActionId() != uuidN(1) || c.GetPlayerId() != ana || c.GetDrop().GetX() != 14 || c.GetDrop().GetY() != 9 {
		t.Fatalf("command = %v", c)
	}
	if got := statusesOf(results(drain(s))); !slices.Equal(got, []string{"applied:" + uuidN(1)}) {
		t.Fatalf("results = %v", got)
	}
	if h.hub.AcceptedDrops() != 1 {
		t.Fatalf("accepted drops = %d, want 1", h.hub.AcceptedDrops())
	}
}

func TestResendWhilePendingIsIgnored(t *testing.T) {
	h := newHarness(t)
	s := h.hub.Connect(ana, auth.RolePlayer, true)
	h.hub.SubmitDrop(s, drop(1))
	if got := h.hub.SubmitDrop(s, drop(1)); got != IgnoredPending {
		t.Fatalf("outcome = %v, want ignored", got)
	}
	h.tick()
	if n := len(h.world.lastCommands()); n != 1 {
		t.Fatalf("Step got %d commands, want 1", n)
	}
	if got := results(drain(s)); len(got) != 1 {
		t.Fatalf("want exactly one final result, got %v", statusesOf(got))
	}
}

func TestResendWhileInFlightIsIgnored(t *testing.T) {
	h := newHarness(t)
	s := h.hub.Connect(ana, auth.RolePlayer, true)
	h.hub.SubmitDrop(s, drop(1))
	var during Outcome
	// Si el hub tuviera su candado tomado durante Step, esto se bloquearía.
	h.world.set(func(w *fakeWorld) {
		w.hook = func(context.Context, *worldv1.StepRequest) { during = h.hub.SubmitDrop(s, drop(1)) }
	})
	h.tick()
	if during != IgnoredPending {
		t.Fatalf("outcome during Step = %v, want ignored", during)
	}
}

func TestConfirmedResendGetsImmediateDuplicate(t *testing.T) {
	h := newHarness(t)
	s := h.hub.Connect(ana, auth.RolePlayer, true)
	h.hub.SubmitDrop(s, drop(1))
	h.tick()
	drain(s)

	if got := h.hub.SubmitDrop(s, drop(1)); got != RejectedDuplicate {
		t.Fatalf("outcome = %v, want duplicate", got)
	}
	if got := statusesOf(results(drain(s))); !slices.Equal(got, []string{"duplicate:" + uuidN(1)}) {
		t.Fatalf("results = %v", got)
	}
	h.tick()
	if n := len(h.world.lastCommands()); n != 0 {
		t.Fatalf("a duplicate must not be queued, Step got %d", n)
	}
}

func TestConfirmedWindowIs600Ticks(t *testing.T) {
	h := newHarness(t)
	s := h.hub.Connect(ana, auth.RolePlayer, true)
	h.hub.SubmitDrop(s, drop(1))
	h.tick() // aplicado en el tick 1
	for range 600 {
		h.tick() // hasta el tick 601
	}
	if got := h.hub.SubmitDrop(s, drop(1)); got != RejectedDuplicate {
		t.Fatalf("at tick 601 (t+600) outcome = %v, want duplicate", got)
	}
	h.tick() // tick 602
	if got := h.hub.SubmitDrop(s, drop(1)); got != Admitted {
		t.Fatalf("at tick 602 outcome = %v, want admitted", got)
	}
}

func TestRustDuplicateMovesToConfirmedWithoutCounting(t *testing.T) {
	h := newHarness(t)
	s := h.hub.Connect(ana, auth.RolePlayer, true)
	h.world.set(func(w *fakeWorld) { w.statuses[uuidN(1)] = worldv1.CommandStatus_COMMAND_STATUS_DUPLICATE })
	h.hub.SubmitDrop(s, drop(1))
	h.tick()
	if got := statusesOf(results(drain(s))); !slices.Equal(got, []string{"duplicate:" + uuidN(1)}) {
		t.Fatalf("results = %v", got)
	}
	if h.hub.AcceptedDrops() != 0 {
		t.Fatal("only applied results count as accepted drops")
	}
	if got := h.hub.SubmitDrop(s, drop(1)); got != RejectedDuplicate {
		t.Fatalf("outcome = %v, want duplicate from confirmed", got)
	}
}

func TestRejectedOnlyLeavesPending(t *testing.T) {
	h := newHarness(t)
	s := h.hub.Connect(ana, auth.RolePlayer, true)
	h.world.set(func(w *fakeWorld) { w.statuses[uuidN(1)] = worldv1.CommandStatus_COMMAND_STATUS_REJECTED })
	h.hub.SubmitDrop(s, drop(1))
	h.tick()
	rs := results(drain(s))
	if len(rs) != 1 || rs[0].GetStatus() != "rejected" || rs[0].GetReason() != "out_of_bounds" {
		t.Fatalf("results = %v", rs)
	}
	if got := h.hub.SubmitDrop(s, drop(1)); got != Admitted {
		t.Fatalf("a rejected action is not confirmed; outcome = %v, want admitted", got)
	}
}

func TestRateLimitFifteenDropsPerSecond(t *testing.T) {
	h := newHarness(t)
	s := h.hub.Connect(ana, auth.RolePlayer, true)
	for i := range 15 {
		if got := h.hub.SubmitDrop(s, drop(i)); got != Admitted {
			t.Fatalf("drop %d outcome = %v", i, got)
		}
	}
	if got := h.hub.SubmitDrop(s, drop(15)); got != RejectedRateLimited {
		t.Fatalf("16th drop outcome = %v, want rate_limited", got)
	}
	if got := statusesOf(results(drain(s))); !slices.Equal(got, []string{"rate_limited:" + uuidN(15)}) {
		t.Fatalf("results = %v", got)
	}
	// El límite es por jugador.
	other := h.hub.Connect(bob, auth.RolePlayer, true)
	if got := h.hub.SubmitDrop(other, drop(100)); got != Admitted {
		t.Fatalf("another player outcome = %v, want admitted", got)
	}
	h.clock.Advance(time.Second)
	if got := h.hub.SubmitDrop(s, drop(15)); got != Admitted {
		t.Fatalf("after 1 s outcome = %v, want admitted", got)
	}
}

func TestQueueCapAnswersPaused(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.QueueCap = 3 })
	s := h.hub.Connect(ana, auth.RolePlayer, true)
	for i := range 3 {
		h.hub.SubmitDrop(s, drop(i))
	}
	if got := h.hub.SubmitDrop(s, drop(3)); got != RejectedPaused {
		t.Fatalf("outcome = %v, want paused", got)
	}
	if got := statusesOf(results(drain(s))); !slices.Equal(got, []string{"paused:" + uuidN(3)}) {
		t.Fatalf("results = %v", got)
	}
	if h.hub.Status().QueueLen != 3 {
		t.Fatalf("queue length = %d, want 3", h.hub.Status().QueueLen)
	}
}

func TestSessionsCountsConnections(t *testing.T) {
	h := newHarness(t)
	a1 := h.hub.Connect(ana, auth.RolePlayer, true)
	h.hub.Connect(ana, auth.RolePlayer, true)
	h.hub.Connect(bob, auth.RolePlayer, false)
	if n := h.hub.Sessions(); n != 3 {
		t.Fatalf("sessions = %d, want 3", n)
	}
	h.hub.Disconnect(a1)
	if n := h.hub.Sessions(); n != 2 {
		t.Fatalf("sessions = %d, want 2", n)
	}
}

func TestSendQueuesAnyFrame(t *testing.T) {
	h := newHarness(t)
	s := h.hub.Connect(ana, auth.RolePlayer, true)
	s.Send(frames.Result(uuidN(1), frames.StatusForbidden, worldv1.RejectReason_REJECT_REASON_UNSPECIFIED))
	if got := statusesOf(results(drain(s))); !slices.Equal(got, []string{"forbidden:" + uuidN(1)}) {
		t.Fatalf("outbox = %v", got)
	}
}

func TestSubmitWithoutSessionIsRejected(t *testing.T) {
	h := newHarness(t)
	s := h.hub.Connect(ana, auth.RolePlayer, true)
	h.hub.Disconnect(s)
	if got := h.hub.SubmitDrop(s, drop(1)); got != RejectedNoSession {
		t.Fatalf("outcome = %v, want no session", got)
	}
	h.tick()
	if n := len(h.world.lastCommands()); n != 0 {
		t.Fatal("nothing must be queued without a session")
	}
}

func TestPendingSurvivesReconnect(t *testing.T) {
	h := newHarness(t)
	s := h.hub.Connect(ana, auth.RolePlayer, true)
	h.hub.SubmitDrop(s, drop(1))
	h.hub.Disconnect(s)
	again := h.hub.Connect(ana, auth.RolePlayer, true)
	if got := h.hub.SubmitDrop(again, drop(1)); got != IgnoredPending {
		t.Fatalf("resend after reconnect outcome = %v, want ignored", got)
	}
	h.tick()
	if got := statusesOf(results(drain(again))); !slices.Equal(got, []string{"applied:" + uuidN(1)}) {
		t.Fatalf("the final result must reach the new connection, got %v", got)
	}
}

func TestResultsGoOnlyToTheirPlayer(t *testing.T) {
	h := newHarness(t)
	a := h.hub.Connect(ana, auth.RolePlayer, true)
	b := h.hub.Connect(bob, auth.RolePlayer, true)
	h.hub.SubmitDrop(a, drop(1))
	h.hub.SubmitDrop(b, drop(2))
	h.tick()
	if got := statusesOf(results(drain(a))); !slices.Equal(got, []string{"applied:" + uuidN(1)}) {
		t.Fatalf("ana results = %v", got)
	}
	if got := statusesOf(results(drain(b))); !slices.Equal(got, []string{"applied:" + uuidN(2)}) {
		t.Fatalf("bob results = %v", got)
	}
}

func TestSlowConsumerIsKicked(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.OutboxSize = 1 })
	s := h.hub.Connect(ana, auth.RolePlayer, true)
	h.hub.SubmitDrop(s, drop(1))
	h.hub.SubmitDrop(s, drop(2))
	h.tick() // un estado y dos resultados no caben en un buzón de 1
	select {
	case <-s.Kicked():
	default:
		t.Fatal("a session whose outbox is full must be kicked")
	}
}

func adminDemo(n int, enabled bool) frames.Admin {
	return frames.Admin{ActionID: uuidN(n), Kind: frames.AdminSetDemoMode, Enabled: enabled}
}

func adminReset(n int) frames.Admin {
	return frames.Admin{ActionID: uuidN(n), Kind: frames.AdminResetValley}
}

func TestAdminCommandsFromAPlayerAreForbiddenAndNeverQueued(t *testing.T) {
	h := newHarness(t)
	s := h.hub.Connect(ana, auth.RolePlayer, true)

	for _, a := range []frames.Admin{adminDemo(1, true), adminReset(2)} {
		if got := h.hub.SubmitAdmin(s, a); got != RejectedForbidden {
			t.Fatalf("outcome = %v, want forbidden", got)
		}
	}
	if got := statusesOf(results(drain(s))); !slices.Equal(got, []string{"forbidden:" + uuidN(1), "forbidden:" + uuidN(2)}) {
		t.Fatalf("results = %v", got)
	}
	if n := h.hub.Status().QueueLen; n != 0 {
		t.Fatalf("queue = %d, forbidden commands must never be queued", n)
	}
	h.tick()
	if n := len(h.world.lastCommands()); n != 0 {
		t.Fatalf("Step got %d commands, want 0", n)
	}
	// El rechazo no deja rastro: ni pending ni confirmed, y el mismo
	// action_id vuelve a recibir forbidden.
	if got := h.hub.SubmitAdmin(s, adminDemo(1, true)); got != RejectedForbidden {
		t.Fatalf("resend outcome = %v, want forbidden again", got)
	}
}

func TestAdminCommandsFromAnAdminAreQueuedAsStepCommands(t *testing.T) {
	h := newHarness(t)
	s := h.hub.Connect(ana, auth.RoleAdmin, true)

	if got := h.hub.SubmitAdmin(s, adminDemo(1, true)); got != Admitted {
		t.Fatalf("set_demo_mode outcome = %v", got)
	}
	if got := h.hub.SubmitAdmin(s, adminReset(2)); got != Admitted {
		t.Fatalf("reset_valley outcome = %v", got)
	}
	if r := results(drain(s)); len(r) != 0 {
		t.Fatalf("admission must not answer immediately, got %v", statusesOf(r))
	}
	h.tick()

	cmds := h.world.lastCommands()
	if len(cmds) != 2 {
		t.Fatalf("Step got %d commands, want 2", len(cmds))
	}
	if c := cmds[0]; c.GetActionId() != uuidN(1) || c.GetPlayerId() != ana || c.GetSetDemoMode() == nil || !c.GetSetDemoMode().GetEnabled() {
		t.Fatalf("command 0 = %v", c)
	}
	if c := cmds[1]; c.GetActionId() != uuidN(2) || c.GetPlayerId() != ana || c.GetResetValley() == nil {
		t.Fatalf("command 1 = %v", c)
	}
	if got := statusesOf(results(drain(s))); !slices.Equal(got, []string{"applied:" + uuidN(1), "applied:" + uuidN(2)}) {
		t.Fatalf("results = %v", got)
	}
	// Los comandos de administración no son gotas: no cuentan como aceptadas.
	if n := h.hub.AcceptedDrops(); n != 0 {
		t.Fatalf("accepted drops = %d, admin commands are not drops", n)
	}
}

func TestAdminCommandsShareIdempotencyAndQueueLimits(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.QueueCap = 1 })
	s := h.hub.Connect(ana, auth.RoleAdmin, true)

	h.hub.SubmitAdmin(s, adminReset(1))
	if got := h.hub.SubmitAdmin(s, adminReset(1)); got != IgnoredPending {
		t.Fatalf("resend while pending = %v, want ignored", got)
	}
	if got := h.hub.SubmitAdmin(s, adminReset(2)); got != RejectedPaused {
		t.Fatalf("full queue = %v, want paused", got)
	}
	if got := statusesOf(results(drain(s))); !slices.Equal(got, []string{"paused:" + uuidN(2)}) {
		t.Fatalf("results = %v", got)
	}
	h.tick()
	drain(s)
	if got := h.hub.SubmitAdmin(s, adminReset(1)); got != RejectedDuplicate {
		t.Fatalf("resend after applied = %v, want duplicate", got)
	}
	if got := statusesOf(results(drain(s))); !slices.Equal(got, []string{"duplicate:" + uuidN(1)}) {
		t.Fatalf("results = %v", got)
	}
}

func TestAdminSubmitWithoutSession(t *testing.T) {
	h := newHarness(t)
	s := h.hub.Connect(ana, auth.RoleAdmin, true)
	h.hub.Disconnect(s)
	if got := h.hub.SubmitAdmin(s, adminReset(1)); got != RejectedNoSession {
		t.Fatalf("outcome = %v, want no session", got)
	}
}

func TestSessionKeepsTheRoleOfItsToken(t *testing.T) {
	h := newHarness(t)
	if r := h.hub.Connect(ana, auth.RoleAdmin, true).Role(); r != auth.RoleAdmin {
		t.Fatalf("role = %v", r)
	}
	if r := h.hub.Connect(bob, auth.RolePlayer, true).Role(); r != auth.RolePlayer {
		t.Fatalf("role = %v", r)
	}
}

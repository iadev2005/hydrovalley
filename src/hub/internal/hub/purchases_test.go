package hub

import (
	"slices"
	"testing"

	worldv1 "github.com/iadev2005/hydrovalley/gen/go/hydrovalley/world/v1"
	"github.com/iadev2005/hydrovalley/src/hub/internal/auth"
	"github.com/iadev2005/hydrovalley/src/hub/internal/frames"
)

func place(n int, kind worldv1.InstallationKind) frames.Place {
	return frames.Place{ActionID: uuidN(n), Kind: kind, X: uint32(n % 32), Y: 2}
}

func buy(n int, kind worldv1.UpgradeKind) frames.Buy {
	return frames.Buy{ActionID: uuidN(n), Kind: kind}
}

const (
	sprinkler  = worldv1.InstallationKind_INSTALLATION_KIND_SPRINKLER
	biggerDrop = worldv1.UpgradeKind_UPGRADE_KIND_BIGGER_DROP
)

func TestPlaceInstallationTravelsToStepWithTheSessionPlayer(t *testing.T) {
	h := newHarness(t)
	s := h.hub.Connect(ana, auth.RolePlayer, true)

	if got := h.hub.SubmitPlace(s, frames.Place{ActionID: uuidN(1), Kind: sprinkler, X: 5, Y: 6}); got != Admitted {
		t.Fatalf("outcome = %v, want admitted", got)
	}
	h.tick()

	cmds := h.world.lastCommands()
	if len(cmds) != 1 {
		t.Fatalf("commands = %v", cmds)
	}
	c := cmds[0]
	p := c.GetPlaceInstallation()
	if c.GetActionId() != uuidN(1) || c.GetPlayerId() != ana || p == nil ||
		p.GetKind() != sprinkler || p.GetX() != 5 || p.GetY() != 6 {
		t.Fatalf("command = %v", c)
	}
	if got := statusesOf(results(drain(s))); !slices.Equal(got, []string{"applied:" + uuidN(1)}) {
		t.Fatalf("results = %v", got)
	}
}

func TestBuyUpgradeTravelsToStep(t *testing.T) {
	h := newHarness(t)
	s := h.hub.Connect(ana, auth.RolePlayer, true)

	if got := h.hub.SubmitBuy(s, buy(1, biggerDrop)); got != Admitted {
		t.Fatalf("outcome = %v, want admitted", got)
	}
	h.tick()

	cmds := h.world.lastCommands()
	if len(cmds) != 1 || cmds[0].GetBuyUpgrade().GetKind() != biggerDrop ||
		cmds[0].GetActionId() != uuidN(1) || cmds[0].GetPlayerId() != ana {
		t.Fatalf("commands = %v", cmds)
	}
	if got := statusesOf(results(drain(s))); !slices.Equal(got, []string{"applied:" + uuidN(1)}) {
		t.Fatalf("results = %v", got)
	}
}

func TestPurchasesDoNotNeedTheAdminRole(t *testing.T) {
	h := newHarness(t)
	s := h.hub.Connect(ana, auth.RolePlayer, true)
	if got := h.hub.SubmitPlace(s, place(1, sprinkler)); got != Admitted {
		t.Fatalf("place by a player = %v", got)
	}
	if got := h.hub.SubmitBuy(s, buy(2, biggerDrop)); got != Admitted {
		t.Fatalf("buy by a player = %v", got)
	}
}

func TestPurchasesAreNotRateLimitedAsDropsAndDoNotConsumeTheDropQuota(t *testing.T) {
	h := newHarness(t)
	s := h.hub.Connect(ana, auth.RolePlayer, true)
	// Más de 15 compras en el mismo segundo: ninguna es una gota.
	for i := range 25 {
		var got Outcome
		if i%2 == 0 {
			got = h.hub.SubmitPlace(s, place(i, sprinkler))
		} else {
			got = h.hub.SubmitBuy(s, buy(i, biggerDrop))
		}
		if got != Admitted {
			t.Fatalf("purchase %d outcome = %v, want admitted", i, got)
		}
	}
	// Y las 15 gotas siguen disponibles.
	for i := range 15 {
		if got := h.hub.SubmitDrop(s, drop(100+i)); got != Admitted {
			t.Fatalf("drop %d outcome = %v after the purchases", i, got)
		}
	}
	if got := h.hub.SubmitDrop(s, drop(200)); got != RejectedRateLimited {
		t.Fatalf("16th drop = %v, want rate limited", got)
	}
	// Ocupar la ventana de gotas tampoco frena las compras.
	if got := h.hub.SubmitPlace(s, place(300, sprinkler)); got != Admitted {
		t.Fatalf("purchase with a full drop window = %v", got)
	}
}

func TestPurchasesAreNotCountedAsAcceptedDrops(t *testing.T) {
	h := newHarness(t)
	s := h.hub.Connect(ana, auth.RolePlayer, true)
	h.hub.SubmitPlace(s, place(1, sprinkler))
	h.hub.SubmitBuy(s, buy(2, biggerDrop))
	h.hub.SubmitDrop(s, drop(3))
	h.tick()
	if n := h.hub.AcceptedDrops(); n != 1 {
		t.Fatalf("accepted drops = %d, want 1 (the drop only)", n)
	}
}

func TestPurchasesShareIdempotencyAndQueueLimits(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.QueueCap = 1 })
	s := h.hub.Connect(ana, auth.RolePlayer, true)

	h.hub.SubmitPlace(s, place(1, sprinkler))
	if got := h.hub.SubmitPlace(s, place(1, sprinkler)); got != IgnoredPending {
		t.Fatalf("resend while pending = %v, want ignored", got)
	}
	// El action_id es único por jugador, sea del tipo que sea.
	if got := h.hub.SubmitBuy(s, buy(1, biggerDrop)); got != IgnoredPending {
		t.Fatalf("same action_id as a buy = %v, want ignored", got)
	}
	if got := h.hub.SubmitBuy(s, buy(2, biggerDrop)); got != RejectedPaused {
		t.Fatalf("full queue = %v, want paused", got)
	}
	if got := statusesOf(results(drain(s))); !slices.Equal(got, []string{"paused:" + uuidN(2)}) {
		t.Fatalf("results = %v", got)
	}
	h.tick()
	drain(s)
	if got := h.hub.SubmitPlace(s, place(1, sprinkler)); got != RejectedDuplicate {
		t.Fatalf("resend after applied = %v, want duplicate", got)
	}
	if got := statusesOf(results(drain(s))); !slices.Equal(got, []string{"duplicate:" + uuidN(1)}) {
		t.Fatalf("results = %v", got)
	}
}

func TestPurchaseRejectionsCarryTheirReasonAndCanBeRetried(t *testing.T) {
	h := newHarness(t)
	s := h.hub.Connect(ana, auth.RolePlayer, true)
	h.world.set(func(w *fakeWorld) {
		w.statuses[uuidN(1)] = worldv1.CommandStatus_COMMAND_STATUS_REJECTED
		w.reasons = map[string]worldv1.RejectReason{uuidN(1): worldv1.RejectReason_REJECT_REASON_INSUFFICIENT_LEAVES}
		w.statuses[uuidN(2)] = worldv1.CommandStatus_COMMAND_STATUS_REJECTED
		w.reasons[uuidN(2)] = worldv1.RejectReason_REJECT_REASON_UPGRADE_ALREADY_OWNED
	})
	h.hub.SubmitPlace(s, place(1, sprinkler))
	h.hub.SubmitBuy(s, buy(2, biggerDrop))
	h.tick()

	var got []string
	for _, r := range results(drain(s)) {
		got = append(got, r.GetStatus()+":"+r.GetReason())
	}
	if !slices.Equal(got, []string{"rejected:insufficient_leaves", "rejected:upgrade_already_owned"}) {
		t.Fatalf("results = %v", got)
	}
	// Un rechazo no entra en confirmed: con otras hojas se puede reenviar.
	if got := h.hub.SubmitPlace(s, place(1, sprinkler)); got != Admitted {
		t.Fatalf("retry after a rejection = %v, want admitted", got)
	}
}

func TestPurchaseRequeuedAfterAFailedStepIsNotADropAndGetsItsResult(t *testing.T) {
	h := newHarness(t)
	s := h.hub.Connect(ana, auth.RolePlayer, true)
	h.hub.SubmitPlace(s, place(1, sprinkler))
	h.world.set(func(w *fakeWorld) { w.fail = true })
	h.tick()
	// Rust ya lo había aplicado: el reintento vuelve como DUPLICATE.
	h.world.set(func(w *fakeWorld) {
		w.fail = false
		w.statuses[uuidN(1)] = worldv1.CommandStatus_COMMAND_STATUS_DUPLICATE
	})
	h.tick()
	if got := statusesOf(results(drain(s))); !slices.Equal(got, []string{"applied:" + uuidN(1)}) {
		t.Fatalf("results = %v", got)
	}
	if n := h.hub.AcceptedDrops(); n != 0 {
		t.Fatalf("accepted drops = %d, a purchase is not a drop", n)
	}
}

func TestPurchaseWithoutSession(t *testing.T) {
	h := newHarness(t)
	s := h.hub.Connect(ana, auth.RolePlayer, true)
	h.hub.Disconnect(s)
	if got := h.hub.SubmitPlace(s, place(1, sprinkler)); got != RejectedNoSession {
		t.Fatalf("place = %v, want no session", got)
	}
	if got := h.hub.SubmitBuy(s, buy(2, biggerDrop)); got != RejectedNoSession {
		t.Fatalf("buy = %v, want no session", got)
	}
}

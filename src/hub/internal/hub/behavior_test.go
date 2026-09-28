package hub

import (
	"slices"
	"testing"
	"time"

	worldv1 "github.com/iadev2005/hydrovalley/gen/go/hydrovalley/world/v1"
	"github.com/iadev2005/hydrovalley/src/hub/internal/frames"
)

// Un Step que vence su plazo después de que Rust lo aplicara: el reintento
// vuelve como DUPLICATE, pero para el hub es la primera confirmación, así
// que el jugador recibe "applied" y la gota se cuenta.
func TestRequeuedDuplicateCountsAsApplied(t *testing.T) {
	h := newHarness(t)
	s := h.hub.Connect(ana, true)
	h.hub.SubmitDrop(s, drop(1))
	h.world.set(func(w *fakeWorld) { w.fail = true })
	h.tick() // Step fallido (Rust lo aplicó, pero el hub no lo sabe)
	h.world.set(func(w *fakeWorld) {
		w.fail = false
		w.statuses[uuidN(1)] = worldv1.CommandStatus_COMMAND_STATUS_DUPLICATE
	})
	h.tick() // tick 1: el reintento vuelve como DUPLICATE
	if got := statusesOf(results(drain(s))); !slices.Equal(got, []string{"applied:" + uuidN(1)}) {
		t.Fatalf("results = %v, want applied", got)
	}
	if got := h.hub.AcceptedDrops(); got != 1 {
		t.Fatalf("accepted drops = %d, want 1", got)
	}
	if got := h.hub.SubmitDrop(s, drop(1)); got != RejectedDuplicate {
		t.Fatalf("outcome = %v, want duplicate from confirmed", got)
	}
	// La gota queda en la cuenta del tick en que se confirmó: un reinicio
	// de Rust desde el tick 0 la resta.
	h.world.set(func(w *fakeWorld) { w.bootID, w.restored, w.tick = "boot-b", 0, 0 })
	h.tick()
	if got := h.hub.AcceptedDrops(); got != 0 {
		t.Fatalf("accepted drops after restart = %d, want 0", got)
	}
}

// Un DUPLICATE de Rust en el primer intento (sin Step fallido) sigue siendo
// "duplicate" y no se cuenta.
func TestFirstAttemptDuplicateIsNotCountedEvenAfterAnotherFailure(t *testing.T) {
	h := newHarness(t)
	s := h.hub.Connect(ana, true)
	h.hub.SubmitDrop(s, drop(1))
	h.world.set(func(w *fakeWorld) { w.fail = true })
	h.tick()
	// La gota 2 llega después del fallo: nunca estuvo en un Step fallido.
	h.hub.SubmitDrop(s, drop(2))
	h.world.set(func(w *fakeWorld) {
		w.fail = false
		w.statuses[uuidN(2)] = worldv1.CommandStatus_COMMAND_STATUS_DUPLICATE
	})
	h.tick()
	if got := statusesOf(results(drain(s))); !slices.Equal(got, []string{"applied:" + uuidN(1), "duplicate:" + uuidN(2)}) {
		t.Fatalf("results = %v", got)
	}
	if got := h.hub.AcceptedDrops(); got != 1 {
		t.Fatalf("accepted drops = %d, want 1", got)
	}
}

// Una gota rechazada por la cola llena no ocupa hueco en la ventana de 15/s.
func TestPausedDropsDoNotConsumeRateWindow(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.QueueCap = 1 })
	s := h.hub.Connect(ana, true)
	h.hub.SubmitDrop(s, drop(0))
	for i := 1; i <= 20; i++ {
		if got := h.hub.SubmitDrop(s, drop(i)); got != RejectedPaused {
			t.Fatalf("drop %d outcome = %v, want paused", i, got)
		}
	}
	h.tick() // la cola se vacía
	drain(s)
	h.hub.cfg.QueueCap = 100
	for i := 100; i < 114; i++ {
		if got := h.hub.SubmitDrop(s, drop(i)); got != Admitted {
			t.Fatalf("drop %d outcome = %v, want admitted (paused drops must not use the window)", i, got)
		}
	}
}

// El registro del jugador (y su ventana) no se borra mientras la ventana
// tenga gotas del último segundo: reconectarse no reinicia el límite.
func TestPlayerRecordKeepsRateWindowAcrossReconnect(t *testing.T) {
	h := newHarness(t)
	s := h.hub.Connect(ana, true)
	for i := range 15 {
		h.world.set(func(w *fakeWorld) { w.statuses[uuidN(i)] = worldv1.CommandStatus_COMMAND_STATUS_REJECTED })
		h.hub.SubmitDrop(s, drop(i))
	}
	h.tick() // todas rechazadas: pending y confirmed quedan vacíos
	h.hub.Disconnect(s)
	again := h.hub.Connect(ana, true)
	if got := h.hub.SubmitDrop(again, drop(15)); got != RejectedRateLimited {
		t.Fatalf("outcome after reconnect = %v, want rate_limited", got)
	}
	h.hub.Disconnect(again)
	h.clock.Advance(time.Second)
	h.tick()
	h.hub.mu.Lock()
	_, kept := h.hub.players[ana]
	h.hub.mu.Unlock()
	if kept {
		t.Fatal("an idle player whose window is empty must be dropped")
	}
}

func TestWireStatusMapping(t *testing.T) {
	cases := []struct {
		o    Outcome
		want string
		ok   bool
	}{
		{Admitted, "", false},
		{IgnoredPending, "", false},
		{RejectedNoSession, "", false},
		{RejectedDuplicate, frames.StatusDuplicate, true},
		{RejectedRateLimited, frames.StatusRateLimited, true},
		{RejectedPaused, frames.StatusPaused, true},
		{Outcome(99), "", false},
	}
	for _, c := range cases {
		got, ok := WireStatus(c.o)
		if got != c.want || ok != c.ok {
			t.Errorf("WireStatus(%v) = %q, %v; want %q, %v", c.o, got, ok, c.want, c.ok)
		}
	}
}

// Status trae la disponibilidad en la misma foto que el resto de campos:
// /readyz no puede mezclar un "ready" de un instante con un "paused" de otro.
func TestStatusCarriesReadinessInTheSameSnapshot(t *testing.T) {
	h := newHarness(t)
	if st := h.hub.Status(); st.Ready {
		t.Fatalf("status before any Step = %+v, want not ready", st)
	}
	h.tick()
	if st := h.hub.Status(); !st.Ready || st.Paused || st.Breaker != "closed" {
		t.Fatalf("status after a successful Step = %+v, want ready", st)
	}
	h.world.set(func(w *fakeWorld) { w.fail = true })
	h.tick()
	if st := h.hub.Status(); st.Ready || !st.Paused {
		t.Fatalf("status after a failed Step = %+v, want paused and not ready", st)
	}
}

func playerKept(h *harness, id string) bool {
	h.hub.mu.Lock()
	defer h.hub.mu.Unlock()
	_, ok := h.hub.players[id]
	return ok
}

// El barrido de registros inactivos recorre todos los jugadores bajo el
// candado: no se hace en cada tick, sino como mucho una vez por
// SweepInterval.
func TestIdleSweepRunsAtMostOncePerInterval(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.SweepInterval = 5 * time.Second })
	s := h.hub.Connect(ana, true)
	h.world.set(func(w *fakeWorld) { w.statuses[uuidN(1)] = worldv1.CommandStatus_COMMAND_STATUS_REJECTED })
	h.hub.SubmitDrop(s, drop(1))
	h.tick() // primer barrido: la ventana del límite sigue activa
	h.hub.Disconnect(s)
	if !playerKept(h, ana) {
		t.Fatal("a player with an active rate window must be kept")
	}
	// La ventana se vacía, pero aún no toca barrer.
	h.clock.Advance(1500 * time.Millisecond)
	h.tick()
	if !playerKept(h, ana) {
		t.Fatal("the sweep ran before SweepInterval elapsed")
	}
	h.clock.Advance(3500 * time.Millisecond)
	h.tick()
	if playerKept(h, ana) {
		t.Fatal("the sweep must run once SweepInterval has elapsed")
	}
}

func TestDefaultSweepIntervalIsOneSecond(t *testing.T) {
	if got := DefaultConfig().SweepInterval; got != time.Second {
		t.Fatalf("SweepInterval = %v, want 1s", got)
	}
}

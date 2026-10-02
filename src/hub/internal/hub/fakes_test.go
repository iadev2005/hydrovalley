package hub

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	playv1 "github.com/iadev2005/hydrovalley/gen/go/hydrovalley/play/v1"
	worldv1 "github.com/iadev2005/hydrovalley/gen/go/hydrovalley/world/v1"
	"github.com/iadev2005/hydrovalley/src/hub/internal/frames"
	"google.golang.org/protobuf/proto"
)

var errUnavailable = errors.New("world unavailable")

// fakeClock es un reloj manual.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// fakeWorld simula WorldService.Step: por defecto aplica todos los comandos
// y avanza el tick en 1.
type fakeWorld struct {
	mu       sync.Mutex
	tick     uint64
	bootID   string
	restored uint64
	fail     bool
	// status por action_id; por defecto APPLIED.
	statuses map[string]worldv1.CommandStatus
	// motivo de rechazo por action_id; por defecto OUT_OF_BOUNDS.
	reasons  map[string]worldv1.RejectReason
	requests []*worldv1.StepRequest
	// hook se llama dentro de Step, sin el candado del mundo.
	hook     func(ctx context.Context, req *worldv1.StepRequest)
	mangle   func(resp *worldv1.StepResponse)
	inFlight int
	maxInFly int
	players  []*worldv1.PlayerState
}

func newFakeWorld() *fakeWorld {
	return &fakeWorld{bootID: "boot-a", statuses: map[string]worldv1.CommandStatus{}}
}

func (w *fakeWorld) Step(ctx context.Context, req *worldv1.StepRequest) (*worldv1.StepResponse, error) {
	w.mu.Lock()
	w.inFlight++
	w.maxInFly = max(w.maxInFly, w.inFlight)
	w.requests = append(w.requests, req)
	hook := w.hook
	w.mu.Unlock()
	defer func() {
		w.mu.Lock()
		w.inFlight--
		w.mu.Unlock()
	}()
	if hook != nil {
		hook(ctx, req)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.fail {
		return nil, errUnavailable
	}
	w.tick++
	resp := &worldv1.StepResponse{
		Tick: w.tick, RestoredTick: w.restored, BootId: w.bootID,
		State: &worldv1.WorldState{Width: 32, Height: 32, Players: w.players},
	}
	for _, c := range req.GetCommands() {
		status, ok := w.statuses[c.GetActionId()]
		if !ok {
			status = worldv1.CommandStatus_COMMAND_STATUS_APPLIED
		}
		r := &worldv1.CommandResult{ActionId: c.GetActionId(), PlayerId: c.GetPlayerId(), Status: status}
		if status == worldv1.CommandStatus_COMMAND_STATUS_REJECTED {
			r.Reason = worldv1.RejectReason_REJECT_REASON_OUT_OF_BOUNDS
			if reason, ok := w.reasons[c.GetActionId()]; ok {
				r.Reason = reason
			}
		}
		resp.Results = append(resp.Results, r)
	}
	if w.mangle != nil {
		w.mangle(resp)
	}
	return resp, nil
}

func (w *fakeWorld) set(f func(w *fakeWorld)) {
	w.mu.Lock()
	defer w.mu.Unlock()
	f(w)
}

func (w *fakeWorld) calls() []*worldv1.StepRequest {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]*worldv1.StepRequest(nil), w.requests...)
}

func (w *fakeWorld) lastCommands() []*worldv1.Command {
	calls := w.calls()
	if len(calls) == 0 {
		return nil
	}
	return calls[len(calls)-1].GetCommands()
}

type harness struct {
	t     *testing.T
	hub   *Hub
	world *fakeWorld
	clock *fakeClock
}

func newHarness(t *testing.T, tweak ...func(*Config)) *harness {
	t.Helper()
	cfg := DefaultConfig()
	for _, f := range tweak {
		f(&cfg)
	}
	world := newFakeWorld()
	clock := newFakeClock()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return &harness{t: t, hub: New(world, clock, cfg, logger), world: world, clock: clock}
}

// tick avanza el reloj 100 ms y ejecuta una vuelta del bucle.
func (h *harness) tick() {
	h.clock.Advance(100 * time.Millisecond)
	h.hub.Tick(context.Background())
}

func uuidN(n int) string { return fmt.Sprintf("00000000-0000-4000-8000-%012d", n) }

func drop(n int) frames.Drop { return frames.Drop{ActionID: uuidN(n), X: uint32(n % 32), Y: 1} }

// drain lee sin bloquear todo lo que haya en el buzón de la sesión.
func drain(s *Session) []proto.Message {
	var out []proto.Message
	for {
		select {
		case m := <-s.Outbox():
			out = append(out, m)
		default:
			return out
		}
	}
}

func results(msgs []proto.Message) []*playv1.ResultMessage {
	var out []*playv1.ResultMessage
	for _, m := range msgs {
		if r, ok := m.(*playv1.ResultMessage); ok {
			out = append(out, r)
		}
	}
	return out
}

func states(msgs []proto.Message) []*playv1.StateMessage {
	var out []*playv1.StateMessage
	for _, m := range msgs {
		if s, ok := m.(*playv1.StateMessage); ok {
			out = append(out, s)
		}
	}
	return out
}

func statusesOf(rs []*playv1.ResultMessage) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.GetStatus() + ":" + r.GetActionId()
	}
	return out
}

const (
	ana = "11111111-1111-4111-8111-111111111111"
	bob = "22222222-2222-4222-8222-222222222222"
)

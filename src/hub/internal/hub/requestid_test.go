package hub

import (
	"bytes"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"

	worldv1 "github.com/iadev2005/hydrovalley/gen/go/hydrovalley/world/v1"
	"github.com/iadev2005/hydrovalley/src/hub/internal/auth"
)

// lockedBuffer es un bytes.Buffer seguro para el registro concurrente.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func loggedHarness(t *testing.T, level slog.Level) (*harness, *lockedBuffer) {
	t.Helper()
	buf := &lockedBuffer{}
	logger := slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: level}))
	return newHarnessLogged(t, logger), buf
}

// RNF-07: cada Step lleva un x-request-id propio por tick, así se encuentra
// en el registro del hub y en el del simulador.
func TestStepCarriesATickRequestID(t *testing.T) {
	h := newHarness(t)
	h.tick()
	h.tick()
	h.tick()
	want := []string{"tick-1", "tick-2", "tick-3"}
	h.world.mu.Lock()
	got := slices.Clone(h.world.requestIDs)
	h.world.mu.Unlock()
	if !slices.Equal(got, want) {
		t.Fatalf("request ids = %v, want %v", got, want)
	}
}

func TestFailedStepKeepsItsRequestIDInTheWarning(t *testing.T) {
	h, buf := loggedHarness(t, slog.LevelInfo)
	h.world.set(func(w *fakeWorld) { w.fail = true })
	h.tick()
	out := buf.String()
	if !strings.Contains(out, "Step failed") || !strings.Contains(out, "request_id=tick-1") {
		t.Fatalf("the warning must name the Step id: %q", out)
	}
}

func TestDebugLogsAdmissionAndFinalResultWithActionID(t *testing.T) {
	h, buf := loggedHarness(t, slog.LevelDebug)
	s := h.hub.Connect(ana, auth.RolePlayer, true)
	h.hub.SubmitDrop(s, drop(1))
	h.tick()
	h.hub.SubmitDrop(s, drop(1)) // ya confirmada: rechazo inmediato
	out := buf.String()
	for _, want := range []string{
		"action_id=" + uuidN(1),
		"action admitted",
		"action result",
		"status=applied",
		"action rejected",
		"outcome=rejected_duplicate",
		"request_id=tick-1",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestInfoLevelStaysQuietPerAction(t *testing.T) {
	h, buf := loggedHarness(t, slog.LevelInfo)
	s := h.hub.Connect(ana, auth.RolePlayer, true)
	for i := range 50 {
		h.hub.SubmitDrop(s, drop(i))
	}
	h.tick()
	if strings.Contains(buf.String(), "action_id") {
		t.Fatalf("info level must not log per action:\n%s", buf.String())
	}
}

func TestExpiredAndRejectedResultsAreLoggedToo(t *testing.T) {
	h, buf := loggedHarness(t, slog.LevelDebug)
	s := h.hub.Connect(ana, auth.RolePlayer, true)
	h.world.set(func(w *fakeWorld) {
		w.statuses = map[string]worldv1.CommandStatus{uuidN(2): worldv1.CommandStatus_COMMAND_STATUS_REJECTED}
	})
	h.hub.SubmitDrop(s, drop(2))
	h.tick()
	if out := buf.String(); !strings.Contains(out, "status=rejected") {
		t.Fatalf("rejected result not logged:\n%s", out)
	}
}

package worldclient

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	worldv1 "github.com/iadev2005/hydrovalley/gen/go/hydrovalley/world/v1"
	"google.golang.org/grpc"
)

type fakeWorldServer struct {
	worldv1.UnimplementedWorldServiceServer
}

func (fakeWorldServer) Step(_ context.Context, req *worldv1.StepRequest) (*worldv1.StepResponse, error) {
	resp := &worldv1.StepResponse{Tick: 7, BootId: "boot"}
	for _, c := range req.GetCommands() {
		resp.Results = append(resp.Results, &worldv1.CommandResult{
			ActionId: c.GetActionId(), PlayerId: c.GetPlayerId(), Status: worldv1.CommandStatus_COMMAND_STATUS_APPLIED,
		})
	}
	return resp, nil
}

func serve(t *testing.T, lis net.Listener) {
	t.Helper()
	srv := grpc.NewServer()
	worldv1.RegisterWorldServiceServer(srv, fakeWorldServer{})
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
}

// gate es un marcador de conexión controlado por la prueba. El listener se
// reserva al principio y nunca se libera (no hay que volver a enlazar un
// puerto liberado); mientras la puerta está cerrada, cada intento de
// conexión falla como si nadie escuchara.
type gate struct {
	mu   sync.Mutex
	open bool
	lis  net.Listener
	// dials cuenta los intentos de conexión, abiertos o no.
	dials int
}

func newGate(t *testing.T) *gate {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lis.Close() })
	return &gate{lis: lis}
}

func (g *gate) dialer(ctx context.Context, _ string) (net.Conn, error) {
	g.mu.Lock()
	g.dials++
	open := g.open
	g.mu.Unlock()
	if !open {
		return nil, errors.New("connection refused (gate closed)")
	}
	var d net.Dialer
	return d.DialContext(ctx, "tcp", g.lis.Addr().String())
}

// up sirve el WorldService falso en el listener reservado y abre la puerta.
func (g *gate) up(t *testing.T) {
	t.Helper()
	serve(t, g.lis)
	g.mu.Lock()
	g.open = true
	g.mu.Unlock()
}

func (g *gate) attempts() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.dials
}

func (g *gate) client(t *testing.T) *Client {
	t.Helper()
	// passthrough: el nombre llega tal cual al marcador, sin resolución DNS.
	c, err := New("passthrough:///world:50051", grpc.WithContextDialer(g.dialer))
	if err != nil {
		t.Fatalf("the client must be created without the world running: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// eventually repite f hasta que devuelve nil o vence el plazo.
func eventually(t *testing.T, within time.Duration, f func() error) {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		err := f()
		if err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("condition not met within %v: %v", within, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func step(c *Client, timeout time.Duration) (*worldv1.StepResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return c.Step(ctx, &worldv1.StepRequest{Commands: []*worldv1.Command{{ActionId: "a", PlayerId: "p"}}})
}

func TestStepRoundTrip(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	serve(t, lis)
	c, err := New(lis.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	resp, err := step(c, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetTick() != 7 || len(resp.GetResults()) != 1 || resp.GetResults()[0].GetActionId() != "a" {
		t.Fatalf("resp = %v", resp)
	}
}

func TestStartsWithoutTheWorldAndFailsFast(t *testing.T) {
	c := newGate(t).client(t)
	start := time.Now()
	if _, err := step(c, 200*time.Millisecond); err == nil {
		t.Fatal("Step without a server must fail")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("Step took %v, want it bounded by its deadline", elapsed)
	}
}

func TestReconnectsWhenTheWorldAppears(t *testing.T) {
	g := newGate(t)
	c := g.client(t)
	if _, err := step(c, 200*time.Millisecond); err == nil {
		t.Fatal("Step must fail while nobody listens")
	}
	// El canal reintenta la conexión por su cuenta (con su backoff), sin que
	// nadie llame a Step.
	eventually(t, 3*time.Second, func() error {
		if n := g.attempts(); n < 2 {
			return fmt.Errorf("%d connection attempts, want a retry", n)
		}
		return nil
	})
	g.up(t)
	// Espera de reconexión acotada a 1 s (MaxDelay): con margen, 3 s.
	eventually(t, 3*time.Second, func() error {
		_, err := step(c, 200*time.Millisecond)
		return err
	})
}

func TestFailsOverAcrossCommaSeparatedAddressesAndCanFailBack(t *testing.T) {
	primary := newGate(t)
	standby := newGate(t)
	standby.up(t)

	c, err := New(primary.lis.Addr().String() + "," + standby.lis.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })

	// 3 fallos en el primario conmutan al standby.
	for i := range 3 {
		if _, err := step(c, 100*time.Millisecond); err == nil {
			t.Fatalf("attempt %d on closed primary should fail", i)
		}
	}
	if c.ActiveAddr() != standby.lis.Addr().String() {
		t.Fatalf("ActiveAddr = %q, want standby %q", c.ActiveAddr(), standby.lis.Addr().String())
	}
	resp, err := step(c, 2*time.Second)
	if err != nil || resp.GetTick() != 7 {
		t.Fatalf("Step on standby = %v, %v", resp, err)
	}
	// La afinidad mantiene el standby mientras funcione.
	if _, err := step(c, 2*time.Second); err != nil || c.ActiveAddr() != standby.lis.Addr().String() {
		t.Fatalf("sticky affinity lost: err=%v active=%q", err, c.ActiveAddr())
	}
}

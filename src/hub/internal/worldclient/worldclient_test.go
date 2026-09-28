package worldclient

import (
	"context"
	"net"
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

// freeAddr reserva un puerto local y lo libera: nadie escucha en él.
func freeAddr(t *testing.T) string {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := lis.Addr().String()
	_ = lis.Close()
	return addr
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
	c, err := New(freeAddr(t))
	if err != nil {
		t.Fatalf("the client must be created without the world running: %v", err)
	}
	defer c.Close()
	start := time.Now()
	if _, err := step(c, 200*time.Millisecond); err == nil {
		t.Fatal("Step without a server must fail")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("Step took %v, want it bounded by its deadline", elapsed)
	}
}

func TestReconnectsWhenTheWorldAppears(t *testing.T) {
	addr := freeAddr(t)
	c, err := New(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := step(c, 200*time.Millisecond); err == nil {
		t.Fatal("Step must fail while nobody listens")
	}
	time.Sleep(1500 * time.Millisecond) // el world tarda en volver
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		t.Skipf("port %s was taken meanwhile: %v", addr, err)
	}
	serve(t, lis)
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := step(c, 200*time.Millisecond); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the client did not reconnect within 3 s of the world coming back")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

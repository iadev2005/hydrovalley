package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/iadev2005/hydrovalley/src/world-go/internal/fakepersist"
)

func env(pairs ...string) func(string) string {
	values := map[string]string{}
	for i := 0; i < len(pairs); i += 2 {
		values[pairs[i]] = pairs[i+1]
	}
	return func(name string) string { return values[name] }
}

func TestRunRejectsAMissingPersistenceAddress(t *testing.T) {
	err := run(context.Background(), env(), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "PERSISTENCE_ADDR") {
		t.Fatalf("error: %v", err)
	}
}

func TestRunRejectsAnInvalidListenAddress(t *testing.T) {
	err := run(context.Background(), env("PERSISTENCE_ADDR", "persistence:50051", "WORLD_LISTEN", "world:50051"), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "WORLD_LISTEN") {
		t.Fatalf("error: %v", err)
	}
}

func TestRunServesUntilItsContextEndsAndLogsTheSeed(t *testing.T) {
	persistence := fakepersist.Serve(t, fakepersist.New())
	ctx, cancel := context.WithCancel(context.Background())
	out := &syncBuffer{}
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, env("PERSISTENCE_ADDR", persistence, "WORLD_LISTEN", "127.0.0.1:0", "WORLD_SEED", "42"), out)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(out.String(), "mundo listo") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("run debe terminar al cancelar su contexto")
	}
	for _, want := range []string{"WorldService escuchando", "seed=42", "from_env=true", "mundo listo"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("falta %q en el registro:\n%s", want, out.String())
		}
	}
}

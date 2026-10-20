// Comando world-go: el simulador del valle en Go, reemplazo directo del
// simulador en Rust (src/world). Lee la configuración del entorno (los mismos
// nombres que Rust), abre el puerto y sirve WorldService y grpc.health.v1 hasta
// recibir SIGINT o SIGTERM; al parar, guarda una instantánea final.
//
// Uso:
//
//	PERSISTENCE_ADDR=persistence:50051 world-go
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/iadev2005/hydrovalley/src/world-go/internal/config"
	"github.com/iadev2005/hydrovalley/src/world-go/internal/server"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Getenv, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "world-go:", err)
		os.Exit(1)
	}
}

// run sirve hasta que ctx termine. El registro va a out.
func run(ctx context.Context, getenv func(string) string, out io.Writer) error {
	cfg, err := config.Load(getenv)
	if err != nil {
		return err
	}
	log := slog.New(slog.NewTextHandler(out, &slog.HandlerOptions{Level: cfg.LogLevel}))

	listener, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return err
	}
	log.Info("WorldService escuchando", "listen", listener.Addr().String(), "persistence", cfg.PersistenceAddr)
	log.Info("semilla del azar (solo se usa si el valle empieza de cero)", "seed", cfg.Seed, "from_env", cfg.SeedFromEnv)

	settings := server.NewSettings(cfg.PersistenceAddr)
	settings.Seed = cfg.Seed
	settings.Log = log
	// Cuando ctx termina (señal de parada) Serve para ordenadamente.
	go func() {
		<-ctx.Done()
		log.Info("señal de parada recibida")
	}()
	return server.Serve(ctx, settings, listener)
}

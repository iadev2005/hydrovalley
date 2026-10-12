// Comando hub: el hub en tiempo real de hydrovalley (Go). Sirve el
// WebSocket /play, marca el tick cada 100 ms llamando a WorldService.Step y
// difunde el estado una vez por segundo.
//
// Uso:
//
//	WORLD_ADDR=world:50051 TOKEN_PUBLIC_KEY=... hub   # servidor
//	hub healthcheck                      # comprobación para Docker (sale 0 si /healthz responde 200)
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/iadev2005/hydrovalley/src/hub/internal/auth"
	"github.com/iadev2005/hydrovalley/src/hub/internal/config"
	"github.com/iadev2005/hydrovalley/src/hub/internal/hub"
	"github.com/iadev2005/hydrovalley/src/hub/internal/transport"
	"github.com/iadev2005/hydrovalley/src/hub/internal/worldclient"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		listen := os.Getenv("HUB_LISTEN")
		if listen == "" {
			listen = ":8081"
		}
		os.Exit(healthcheck(listen))
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "hub:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel}))

	// El canal gRPC no se conecta hasta el primer Step: el hub arranca
	// aunque el simulador no esté (regla de portabilidad 5).
	world, err := worldclient.New(cfg.WorldAddr)
	if err != nil {
		return fmt.Errorf("world client: %w", err)
	}
	defer world.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	hubCfg := hub.DefaultConfig()
	h := hub.New(world, hub.SystemClock{}, hubCfg, log)
	ticker := time.NewTicker(hubCfg.TickInterval)
	defer ticker.Stop()
	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		h.Run(ctx, ticker.C)
	}()

	srv := &http.Server{
		Addr: cfg.Listen,
		Handler: transport.NewHandler(ctx, h, log, transport.Options{
			OriginPatterns: cfg.OriginPatterns,
			Verifier:       auth.NewVerifier(cfg.TokenKey, time.Now),
		}),
		ReadHeaderTimeout: 5 * time.Second,
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.ListenAndServe() }()
	log.Info("hub listening", "addr", cfg.Listen, "world_addr", cfg.WorldAddr)

	select {
	case err := <-serveErr:
		stop()
		<-loopDone
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
	}
	log.Info("shutting down")
	// ctx ya está cancelado: las conexiones WebSocket se cierran con
	// StatusGoingAway y el bucle del tick termina.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Warn("http shutdown", "err", err)
	}
	<-loopDone
	return nil
}

// probeURL traduce la dirección de escucha a la URL de /healthz dentro del
// propio contenedor (una dirección comodín se prueba por 127.0.0.1).
func probeURL(listen string) (string, error) {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return "", err
	}
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/healthz", nil
}

// healthcheck devuelve 0 si /healthz responde 200 y 1 en otro caso. La
// imagen distroless no trae curl ni wget.
func healthcheck(listen string) int {
	url, err := probeURL(listen)
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck:", err)
		return 1
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck:", err)
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, "healthcheck: status", resp.StatusCode)
		return 1
	}
	return 0
}

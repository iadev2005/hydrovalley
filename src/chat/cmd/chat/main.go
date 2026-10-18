// Comando chat: el servicio de chat en tiempo real de hydrovalley (Go).
// Sirve el WebSocket /chat con reloj lógico (número de secuencia creciente)
// y expone el servicio gRPC ChatService (SendMessage y ListMessages).
//
// Uso:
//
//	CHAT_HTTP_LISTEN=:8082 CHAT_GRPC_LISTEN=:50051 TOKEN_PUBLIC_KEY=... chat # servidor
//	chat healthcheck # comprobación para Docker (sale 0 si /healthz responde 200)
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

	chatv1 "github.com/iadev2005/hydrovalley/gen/go/hydrovalley/chat/v1"
	"github.com/iadev2005/hydrovalley/src/chat/internal/auth"
	"github.com/iadev2005/hydrovalley/src/chat/internal/config"
	grpcserver "github.com/iadev2005/hydrovalley/src/chat/internal/grpc"
	"github.com/iadev2005/hydrovalley/src/chat/internal/store"
	"github.com/iadev2005/hydrovalley/src/chat/internal/transport"
	"google.golang.org/grpc"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		listen := os.Getenv("CHAT_HTTP_LISTEN")
		if listen == "" {
			listen = ":8082"
		}
		os.Exit(healthcheck(listen))
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "chat:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel}))

	st, err := store.New("")
	if err != nil {
		return fmt.Errorf("init store: %w", err)
	}

	hub := transport.NewHub(st.ServerEpoch())
	verifier := auth.NewVerifier(cfg.TokenKey, time.Now)

	httpHandler := transport.NewHandler(st, hub, verifier, log, transport.Options{
		OriginPatterns: cfg.OriginPatterns,
	})

	httpServer := &http.Server{
		Addr:    cfg.HTTPListen,
		Handler: httpHandler,
	}

	httpLis, err := net.Listen("tcp", cfg.HTTPListen)
	if err != nil {
		return fmt.Errorf("listen HTTP on %s: %w", cfg.HTTPListen, err)
	}
	defer httpLis.Close()

	grpcLis, err := net.Listen("tcp", cfg.GRPCListen)
	if err != nil {
		return fmt.Errorf("listen gRPC on %s: %w", cfg.GRPCListen, err)
	}
	defer grpcLis.Close()

	grpcServer := grpc.NewServer()
	chatv1.RegisterChatServiceServer(grpcServer, grpcserver.New(st, hub))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	httpErrCh := make(chan error, 1)
	go func() {
		log.Info("HTTP/WebSocket server listening", "addr", cfg.HTTPListen)
		if err := httpServer.Serve(httpLis); err != nil && !errors.Is(err, http.ErrServerClosed) {
			httpErrCh <- fmt.Errorf("http server: %w", err)
		}
		close(httpErrCh)
	}()

	grpcErrCh := make(chan error, 1)
	go func() {
		log.Info("gRPC server listening", "addr", cfg.GRPCListen)
		if err := grpcServer.Serve(grpcLis); err != nil {
			grpcErrCh <- fmt.Errorf("grpc server: %w", err)
		}
		close(grpcErrCh)
	}()

	select {
	case <-ctx.Done():
		log.Info("shutting down servers gracefully...")
	case err := <-httpErrCh:
		return err
	case err := <-grpcErrCh:
		return err
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_ = httpServer.Shutdown(shutdownCtx)
	grpcServer.GracefulStop()

	return nil
}

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

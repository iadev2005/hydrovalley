// Comando bots: jugadores simulados de hydrovalley. Entran por la misma
// puerta que los humanos (el proxy: /api y /play) y nunca llaman a los
// servicios internos.
//
// Uso:
//
//	HV_BASE_URL=http://nginx:8080 HV_ADMIN_USERNAME=admin HV_ADMIN_PASSWORD=... bots neighbors
//
// Subcomandos: neighbors (vecinos). load, cheater y firefighter llegan en
// tareas siguientes.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/iadev2005/hydrovalley/src/bots/internal/client"
	"github.com/iadev2005/hydrovalley/src/bots/internal/config"
	"github.com/iadev2005/hydrovalley/src/bots/internal/scenario/neighbors"
)

const usage = `usage: bots <command>

commands:
  neighbors     slow-playing neighbors that make the valley feel alive
  load          (not implemented yet)
  cheater       (not implemented yet)
  firefighter   (not implemented yet)

environment: HV_BASE_URL, HV_ADMIN_USERNAME, HV_ADMIN_PASSWORD, BOTS_COUNT,
BOTS_DURATION, BOTS_SEED, LOG_LEVEL (see src/bots/README.md)`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	switch cmd := os.Args[1]; cmd {
	case "neighbors":
		if err := runNeighbors(); err != nil {
			fmt.Fprintln(os.Stderr, "bots:", err)
			os.Exit(1)
		}
	case "load", "cheater", "firefighter":
		fmt.Fprintf(os.Stderr, "bots: command %q is not implemented yet\n", cmd)
		os.Exit(2)
	case "help", "-h", "--help":
		fmt.Println(usage)
	default:
		fmt.Fprintf(os.Stderr, "bots: unknown command %q\n\n%s\n", cmd, usage)
		os.Exit(2)
	}
}

func runNeighbors() error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	if err := cfg.RequireAdmin(); err != nil {
		return err
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel}))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	api, err := client.NewAPI(cfg.BaseURL)
	if err != nil {
		return err
	}
	adminToken, err := api.Login(ctx, cfg.AdminUsername, cfg.AdminPassword)
	if err != nil {
		return err
	}
	accounts, err := api.ProvisionBots(ctx, adminToken, cfg.Count, "bot")
	if err != nil {
		return err
	}
	log.Info("bot accounts provisioned", "count", len(accounts))

	seed := cfg.Seed
	if !cfg.SeedSet {
		seed = rand.Uint64()
	}
	log.Info("neighbors starting", "bots", len(accounts), "seed", seed, "duration", cfg.Duration)

	sessions := make([]neighbors.Session, 0, len(accounts))
	for _, acc := range accounts {
		s, err := client.NewSession(client.SessionConfig{
			BaseURL: cfg.BaseURL,
			Token:   acc.Token,
			Map:     true,
			Logger:  log.With("player", acc.Username),
		})
		if err != nil {
			return err
		}
		sessions = append(sessions, s)
	}

	runCtx := ctx
	if cfg.Duration > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, cfg.Duration)
		defer cancel()
	}
	started := time.Now()
	sum := neighbors.Run(runCtx, neighbors.Config{Seed: seed}, sessions, log)

	log.Info("neighbors finished",
		"elapsed", time.Since(started).Round(time.Millisecond),
		"bots", sum.Bots, "sent", sum.Sent, "results", sum.Results,
		"unresolved", sum.Unresolved, "send_errors", sum.SendErrors,
		"unauthorized", sum.Unauthorized)
	if sum.Unauthorized == sum.Bots && sum.Bots > 0 {
		return errors.New("every bot was rejected with an invalid token")
	}
	return nil
}

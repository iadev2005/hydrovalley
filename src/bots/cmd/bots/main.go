// Comando bots: jugadores simulados de hydrovalley. Entran por la misma
// puerta que los humanos (el proxy: /api y /play) y nunca llaman a los
// servicios internos.
//
// Uso:
//
//	HV_BASE_URL=http://nginx:8080 HV_ADMIN_USERNAME=admin HV_ADMIN_PASSWORD=... bots neighbors
//
// Subcomandos: neighbors (vecinos), load o load-test (prueba de carga, CP-11), cheater
// (tramposo, CP-03) y firefighter (bomberos, CP-04).
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
	"github.com/iadev2005/hydrovalley/src/bots/internal/scenario/cheater"
	"github.com/iadev2005/hydrovalley/src/bots/internal/scenario/firefighter"
	"github.com/iadev2005/hydrovalley/src/bots/internal/scenario/load"
	"github.com/iadev2005/hydrovalley/src/bots/internal/scenario/neighbors"
)

const usage = `usage: bots <command>

commands:
  neighbors     slow-playing neighbors that make the valley feel alive
  load          (alias: load-test) CP-11 load test: demo mode, RNF-01/RNF-02 and the humidity invariant;
                exit code 1 if any check fails
  cheater       CP-03: 50 drops/s, a repeated action_id and invalid frames;
                exit code 1 if any check fails
  firefighter   CP-04: bots put out each fire together; exit code 1 if a fire
                was never put out or the bonus looks repeated

environment: HV_BASE_URL, HV_ADMIN_USERNAME, HV_ADMIN_PASSWORD, BOTS_COUNT,
BOTS_DURATION, BOTS_SEED, BOTS_RESET, BOTS_REPORT_PATH,
BOTS_FIREFIGHTER_SEED_PLANTS, BOTS_FIREFIGHTER_BONUS, LOG_LEVEL
(see src/bots/README.md)`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	switch cmd := canonicalCommand(os.Args[1]); cmd {
	case "neighbors":
		if err := runNeighbors(); err != nil {
			fmt.Fprintln(os.Stderr, "bots:", err)
			os.Exit(1)
		}
	case "load":
		passed, err := runLoad()
		if err != nil {
			fmt.Fprintln(os.Stderr, "bots:", err)
			os.Exit(1)
		}
		if !passed {
			os.Exit(1)
		}
	case "cheater", "firefighter":
		run := runCheater
		if cmd == "firefighter" {
			run = runFirefighter
		}
		passed, err := run()
		if err != nil {
			fmt.Fprintln(os.Stderr, "bots:", err)
			os.Exit(1)
		}
		if !passed {
			os.Exit(1)
		}
	case "help", "-h", "--help":
		fmt.Println(usage)
	default:
		fmt.Fprintf(os.Stderr, "bots: unknown command %q\n\n%s\n", cmd, usage)
		os.Exit(2)
	}
}

// canonicalCommand traduce los alias de los subcomandos a su nombre: la
// documentación llama a la prueba de carga «load-test» (CP-11).
func canonicalCommand(cmd string) string {
	if cmd == "load-test" {
		return "load"
	}
	return cmd
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

// runLoad ejecuta la prueba de carga (CP-11): entra como administrador,
// aprovisiona los bots y mide. Devuelve si pasaron las tres comprobaciones.
func runLoad() (passed bool, err error) {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return false, err
	}
	if err := cfg.RequireAdmin(); err != nil {
		return false, err
	}
	cfg = cfg.WithLoadDefaults()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel}))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	api, err := client.NewAPI(cfg.BaseURL)
	if err != nil {
		return false, err
	}
	adminToken, err := api.Login(ctx, cfg.AdminUsername, cfg.AdminPassword)
	if err != nil {
		return false, err
	}
	accounts, err := api.ProvisionBots(ctx, adminToken, cfg.Count, "bot")
	if err != nil {
		return false, err
	}
	log.Info("bot accounts provisioned", "count", len(accounts))

	// Sin mapa: la prueba solo necesita el tamaño del valle y los contadores,
	// y así cada estado pesa unos pocos bytes.
	adminSession, err := client.NewSession(client.SessionConfig{BaseURL: cfg.BaseURL, Token: adminToken, Logger: log.With("player", "admin")})
	if err != nil {
		return false, err
	}
	sessions := make([]load.Session, 0, len(accounts))
	for _, acc := range accounts {
		s, err := client.NewSession(client.SessionConfig{BaseURL: cfg.BaseURL, Token: acc.Token, Logger: log.With("player", acc.Username)})
		if err != nil {
			return false, err
		}
		sessions = append(sessions, s)
	}

	seed := cfg.Seed
	if !cfg.SeedSet {
		seed = rand.Uint64()
	}
	log.Info("load test starting", "bots", len(sessions), "duration", cfg.Duration, "seed", seed, "reset", cfg.Reset)
	rep, err := load.Run(ctx, load.Config{Duration: cfg.Duration, Reset: cfg.Reset, Seed: seed}, adminSession, sessions, log)
	if err != nil {
		return false, err
	}

	fmt.Print(load.FormatSummary(rep))
	if cfg.ReportPath != "" {
		data, err := load.MarshalReport(rep)
		if err != nil {
			return false, err
		}
		if err := os.WriteFile(cfg.ReportPath, data, 0o644); err != nil {
			return false, fmt.Errorf("writing the report: %w", err)
		}
		log.Info("report written", "path", cfg.ReportPath)
	}
	return rep.Passed(), nil
}

// provisioned es lo que comparten cheater y firefighter: configuración,
// registro y las cuentas de bots ya creadas.
type provisioned struct {
	cfg      config.Config
	log      *slog.Logger
	accounts []client.Account
}

// provision entra como administrador y crea count cuentas de bots.
func provision(ctx context.Context, count int) (provisioned, error) {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return provisioned{}, err
	}
	if err := cfg.RequireAdmin(); err != nil {
		return provisioned{}, err
	}
	if count == 0 {
		count = cfg.Count
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel}))
	api, err := client.NewAPI(cfg.BaseURL)
	if err != nil {
		return provisioned{}, err
	}
	adminToken, err := api.Login(ctx, cfg.AdminUsername, cfg.AdminPassword)
	if err != nil {
		return provisioned{}, err
	}
	accounts, err := api.ProvisionBots(ctx, adminToken, count, "bot")
	if err != nil {
		return provisioned{}, err
	}
	log.Info("bot accounts provisioned", "count", len(accounts))
	return provisioned{cfg: cfg, log: log, accounts: accounts}, nil
}

// writeReport escribe el informe JSON si BOTS_REPORT_PATH está definida.
func writeReport(p provisioned, marshal func() ([]byte, error)) error {
	if p.cfg.ReportPath == "" {
		return nil
	}
	data, err := marshal()
	if err != nil {
		return err
	}
	if err := os.WriteFile(p.cfg.ReportPath, data, 0o644); err != nil {
		return fmt.Errorf("writing the report: %w", err)
	}
	p.log.Info("report written", "path", p.cfg.ReportPath)
	return nil
}

// runCheater ejecuta CP-03 con un solo bot. Devuelve si pasaron las tres fases.
func runCheater() (passed bool, err error) {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	// Un solo tramposo: BOTS_COUNT no cuenta aquí.
	p, err := provision(ctx, 1)
	if err != nil {
		return false, err
	}
	s, err := client.NewSession(client.SessionConfig{BaseURL: p.cfg.BaseURL, Token: p.accounts[0].Token, Logger: p.log.With("player", p.accounts[0].Username)})
	if err != nil {
		return false, err
	}
	seed := p.cfg.Seed
	if !p.cfg.SeedSet {
		seed = rand.Uint64()
	}
	p.log.Info("cheater starting", "duration", p.cfg.Duration, "seed", seed)
	rep, err := cheater.Run(ctx, cheater.Config{Duration: p.cfg.Duration, Seed: seed}, s)
	if err != nil {
		return false, err
	}
	fmt.Print(cheater.FormatSummary(rep))
	if err := writeReport(p, func() ([]byte, error) { return cheater.MarshalReport(rep) }); err != nil {
		return false, err
	}
	return rep.Passed(), nil
}

// runFirefighter ejecuta CP-04 con BOTS_COUNT bomberos (3 por defecto).
func runFirefighter() (passed bool, err error) {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	p, err := provision(ctx, 0)
	if err != nil {
		return false, err
	}
	sessions := make([]firefighter.Session, 0, len(p.accounts))
	for _, acc := range p.accounts {
		// Con mapa: el bot necesita las etapas para distinguir un fuego
		// apagado de uno consumido.
		s, err := client.NewSession(client.SessionConfig{BaseURL: p.cfg.BaseURL, Token: acc.Token, Map: true, Logger: p.log.With("player", acc.Username)})
		if err != nil {
			return false, err
		}
		sessions = append(sessions, s)
	}
	seed := p.cfg.Seed
	if !p.cfg.SeedSet {
		seed = rand.Uint64()
	}
	cfg := firefighter.Config{Duration: p.cfg.Duration, Bonus: p.cfg.FireBonus, SeedPlants: p.cfg.SeedPlants, Seed: seed}
	p.log.Info("firefighters starting", "bots", len(sessions), "duration", cfg.Duration, "seed", seed, "seed_plants", cfg.SeedPlants)
	rep, err := firefighter.Run(ctx, cfg, sessions, p.log)
	if err != nil && rep.Bots == 0 {
		return false, err
	}
	fmt.Print(firefighter.FormatSummary(rep))
	if werr := writeReport(p, func() ([]byte, error) { return firefighter.MarshalReport(rep) }); werr != nil {
		return false, werr
	}
	return rep.Passed(), err
}

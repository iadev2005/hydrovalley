// Package server monta el servidor gRPC: WorldService, la salud estándar
// (grpc.health.v1) y la carga del catálogo y del mundo en segundo plano. Es el
// espejo de src/world/src/server.rs.
package server

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"sync"
	"time"

	worldv1 "github.com/iadev2005/hydrovalley/gen/go/hydrovalley/world/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	"github.com/iadev2005/hydrovalley/src/world-go/internal/simulator"
	"github.com/iadev2005/hydrovalley/src/world-go/internal/store"
)

// WorldServiceName es el nombre completo del servicio para grpc.health.v1.
const WorldServiceName = "hydrovalley.world.v1.WorldService"

// Settings es lo que necesita Serve.
type Settings struct {
	PersistenceAddr string
	CallPolicy      store.CallPolicy
	LoadBackoff     simulator.Backoff
	// ShutdownTimeout es el plazo de la parada ordenada: esperar al guardado
	// en curso y enviar la instantánea final.
	ShutdownTimeout time.Duration
	// Seed es la semilla del azar para un valle que empieza de cero
	// (WORLD_SEED).
	Seed uint64
	// Standby (WORLD_MODE=standby) pospone la carga de la instantánea hasta el
	// primer Step de promoción.
	Standby bool
	// Log puede ser nil.
	Log *slog.Logger
}

// NewSettings son los plazos y reintentos de CLAUDE.md para la persistencia en
// addr; la semilla la pone quien llama.
func NewSettings(addr string) Settings {
	return Settings{
		PersistenceAddr: addr,
		CallPolicy:      store.DefaultCallPolicy(),
		LoadBackoff:     simulator.DefaultBackoff(),
		ShutdownTimeout: 5 * time.Second,
	}
}

// Serve atiende en listener hasta que ctx termine.
//
// Al terminar ctx, el servidor deja de aceptar peticiones y espera a las que
// están en curso; después el simulador espera al guardado en curso y envía una
// instantánea final si el tick avanzó desde la última guardada, con un plazo de
// settings.ShutdownTimeout.
//
// El gRPC atiende desde el principio (regla 5 de portabilidad): mientras el
// catálogo y el mundo se cargan, Step responde UNAVAILABLE y la salud está en
// NOT_SERVING.
func Serve(ctx context.Context, settings Settings, listener net.Listener) error {
	log := settings.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	persistence, err := store.NewGRPC(settings.PersistenceAddr, settings.CallPolicy, log)
	if err != nil {
		return err
	}
	defer func() { _ = persistence.Close() }()
	sim := simulator.New(persistence, simulator.Options{
		Seed:    settings.Seed,
		Standby: settings.Standby,
		Backoff: settings.LoadBackoff,
		Log:     log,
	})
	log.Info("simulador creado", "boot_id", sim.BootID(), "standby", settings.Standby)

	healthServer := health.NewServer()
	setHealth := func(value healthpb.HealthCheckResponse_ServingStatus) {
		for _, name := range []string{"", WorldServiceName} {
			healthServer.SetServingStatus(name, value)
		}
	}
	if settings.Standby {
		setHealth(healthpb.HealthCheckResponse_SERVING)
	} else {
		setHealth(healthpb.HealthCheckResponse_NOT_SERVING)
	}

	loadCtx, cancelLoad := context.WithCancel(ctx)
	defer cancelLoad()
	var loader sync.WaitGroup
	loader.Add(1)
	go func() {
		defer loader.Done()
		if !settings.Standby {
			if err := sim.Load(loadCtx); err != nil {
				return // parada antes de terminar la carga
			}
			setHealth(healthpb.HealthCheckResponse_SERVING)
			log.Info("mundo listo: Step atiende")
		}
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-loadCtx.Done():
				return
			case <-ticker.C:
				if sim.IsReady() || sim.IsStandby() {
					setHealth(healthpb.HealthCheckResponse_SERVING)
				} else {
					setHealth(healthpb.HealthCheckResponse_NOT_SERVING)
				}
			}
		}
	}()

	server := grpc.NewServer()
	healthpb.RegisterHealthServer(server, healthServer)
	worldv1.RegisterWorldServiceServer(server, &service{sim: sim, log: log})
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()

	var result error
	select {
	case result = <-serveErr:
	case <-ctx.Done():
		// Las peticiones en curso terminan; las nuevas ya no se aceptan. Un
		// cliente que no suelta la conexión no retrasa la parada más que el
		// plazo de la parada.
		healthServer.Shutdown()
		stopped := make(chan struct{})
		go func() { server.GracefulStop(); close(stopped) }()
		select {
		case <-stopped:
		case <-time.After(settings.ShutdownTimeout):
			log.Warn("las peticiones en curso no terminaron a tiempo: se cierra el servidor", "timeout", settings.ShutdownTimeout)
			server.Stop()
		}
		result = <-serveErr
	}
	if errors.Is(result, grpc.ErrServerStopped) {
		result = nil
	}
	cancelLoad()
	loader.Wait()
	sim.Shutdown(settings.ShutdownTimeout)
	return result
}

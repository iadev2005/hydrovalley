// Package config lee la configuración de las variables de entorno (regla 1 de
// portabilidad: ninguna dirección en el código). Usa los mismos nombres y
// valores por omisión que src/world/src/config.rs, para que world-go sea un
// reemplazo directo de Rust en Compose.
package config

import (
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

// DefaultListen es la dirección de escucha por omisión.
const DefaultListen = "0.0.0.0:50051"

// WorldMode define el modo de arranque del simulador (WORLD_MODE, B8 W2).
type WorldMode string

const (
	// ModeActive carga el catálogo y la instantánea al arrancar el proceso.
	ModeActive WorldMode = "active"
	// ModeStandby arranca en espera sin cargar la instantánea hasta recibir su
	// primer Step de promoción.
	ModeStandby WorldMode = "standby"
)

// Config es la configuración del proceso.
type Config struct {
	// Listen (WORLD_LISTEN): dirección y puerto del gRPC.
	Listen string
	// PersistenceAddr (PERSISTENCE_ADDR): host:puerto de PersistenceService y
	// CatalogService (obligatoria).
	PersistenceAddr string
	// Seed (WORLD_SEED, u64): semilla del azar de los incendios cuando el valle
	// empieza de cero. Sin la variable se deriva de la hora de arranque
	// (SeedFromEnv es false); quien la use debe registrarla para poder repetir
	// la partida.
	Seed        uint64
	SeedFromEnv bool
	// Mode (WORLD_MODE): active o standby (por omisión active).
	Mode WorldMode
	// LogLevel (LOG_LEVEL): trace, debug, info, warn o error. Un valor que no
	// se entiende deja info, como en Rust.
	LogLevel slog.Level
}

// SeedFromTime es la semilla derivada de la hora: los nanosegundos desde la
// época Unix. Separada del reloj para probarla.
func SeedFromTime(at time.Time) uint64 { return uint64(at.UnixNano()) }

// Load lee la configuración con getenv (os.Getenv en producción).
func Load(getenv func(string) string) (Config, error) {
	read := func(name string) string { return strings.TrimSpace(getenv(name)) }

	listen := read("WORLD_LISTEN")
	if listen == "" {
		listen = DefaultListen
	}
	if !validListen(listen) {
		return Config{}, fmt.Errorf("WORLD_LISTEN no es válida: %q", listen)
	}
	persistence := read("PERSISTENCE_ADDR")
	if persistence == "" {
		return Config{}, fmt.Errorf("falta la variable de entorno PERSISTENCE_ADDR")
	}
	cfg := Config{Listen: listen, PersistenceAddr: persistence, Mode: ModeActive, LogLevel: parseLevel(read("LOG_LEVEL"))}
	if text := read("WORLD_SEED"); text != "" {
		seed, err := strconv.ParseUint(text, 10, 64)
		if err != nil {
			return Config{}, fmt.Errorf("WORLD_SEED no es válida: %q", text)
		}
		cfg.Seed, cfg.SeedFromEnv = seed, true
	} else {
		cfg.Seed = SeedFromTime(time.Now())
	}
	if modeText := read("WORLD_MODE"); modeText != "" {
		switch strings.ToLower(modeText) {
		case string(ModeActive):
			cfg.Mode = ModeActive
		case string(ModeStandby):
			cfg.Mode = ModeStandby
		default:
			return Config{}, fmt.Errorf("WORLD_MODE no es válida: %q", modeText)
		}
	}
	return cfg, nil
}

// validListen acepta host:puerto con host IP (o vacío, como ":50051") y un
// puerto de 0 a 65535. Un nombre de host no vale, igual que en Rust.
func validListen(listen string) bool {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return false
	}
	if _, err := strconv.ParseUint(port, 10, 16); err != nil {
		return false
	}
	if host == "" {
		return true
	}
	_, err = netip.ParseAddr(host)
	return err == nil
}

func parseLevel(text string) slog.Level {
	switch strings.ToLower(text) {
	case "trace":
		return slog.LevelDebug - 4
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

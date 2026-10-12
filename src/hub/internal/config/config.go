// Package config lee la configuración del hub de las variables de entorno
// (regla de portabilidad 1: ninguna dirección en el código).
package config

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/iadev2005/hydrovalley/src/hub/internal/auth"
)

// Config es la configuración del proceso.
type Config struct {
	// HUB_LISTEN: dirección del HTTP (/play, /healthz, /readyz).
	Listen string
	// WORLD_ADDR: host:puerto de WorldService (obligatoria).
	WorldAddr string
	// LOG_LEVEL: debug, info, warn o error.
	LogLevel slog.Level
	// HUB_ORIGIN_PATTERNS: orígenes (host[:puerto], admite comodines) que
	// pueden abrir /play además del propio; vacío = solo el mismo origen.
	OriginPatterns []string
	// TOKEN_PUBLIC_KEY: clave pública Ed25519 (PEM SPKI o su base64) con la
	// que se verifica el token de /play (obligatoria).
	TokenKey ed25519.PublicKey
}

// Load lee la configuración con getenv (os.Getenv en producción).
func Load(getenv func(string) string) (Config, error) {
	cfg := Config{Listen: ":8081", LogLevel: slog.LevelInfo}
	if v := strings.TrimSpace(getenv("HUB_LISTEN")); v != "" {
		cfg.Listen = v
	}
	cfg.WorldAddr = strings.TrimSpace(getenv("WORLD_ADDR"))
	if cfg.WorldAddr == "" {
		return Config{}, errors.New("WORLD_ADDR is required (e.g. world:50051)")
	}
	key, err := auth.ParsePublicKey(getenv("TOKEN_PUBLIC_KEY"))
	if err != nil {
		return Config{}, fmt.Errorf("TOKEN_PUBLIC_KEY is required and must be an Ed25519 public key (PEM or base64 of the PEM): %w", err)
	}
	cfg.TokenKey = key
	if v := strings.TrimSpace(getenv("LOG_LEVEL")); v != "" {
		if err := cfg.LogLevel.UnmarshalText([]byte(v)); err != nil {
			return Config{}, fmt.Errorf("invalid LOG_LEVEL %q: %w", v, err)
		}
	}
	for _, o := range strings.Split(getenv("HUB_ORIGIN_PATTERNS"), ",") {
		if o = strings.TrimSpace(o); o != "" {
			cfg.OriginPatterns = append(cfg.OriginPatterns, o)
		}
	}
	return cfg, nil
}

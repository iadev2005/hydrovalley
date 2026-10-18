// Package config lee la configuración del servicio de chat de las variables
// de entorno (regla de portabilidad 1: ninguna dirección fija en código).
package config

import (
	"crypto/ed25519"
	"fmt"
	"log/slog"
	"strings"

	"github.com/iadev2005/hydrovalley/src/chat/internal/auth"
)

// Config almacena los parámetros de configuración del servicio chat.
type Config struct {
	// CHAT_HTTP_LISTEN: dirección de escucha HTTP y WebSocket (/chat, /healthz, /readyz).
	HTTPListen string
	// CHAT_GRPC_LISTEN: dirección de escucha del servicio gRPC ChatService.
	GRPCListen string
	// LOG_LEVEL: nivel de registro (debug, info, warn o error).
	LogLevel slog.Level
	// CHAT_ORIGIN_PATTERNS: orígenes permitidos para abrir el WebSocket además del host actual.
	OriginPatterns []string
	// TOKEN_PUBLIC_KEY: clave pública Ed25519 para verificar tokens de sesión.
	TokenKey ed25519.PublicKey
}

// Load lee la configuración a través de getenv (os.Getenv en producción).
func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		HTTPListen: ":8082",
		GRPCListen: ":50051",
		LogLevel:   slog.LevelInfo,
	}

	if v := strings.TrimSpace(getenv("CHAT_HTTP_LISTEN")); v != "" {
		cfg.HTTPListen = v
	}
	if v := strings.TrimSpace(getenv("CHAT_GRPC_LISTEN")); v != "" {
		cfg.GRPCListen = v
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

	for _, o := range strings.Split(getenv("CHAT_ORIGIN_PATTERNS"), ",") {
		if o = strings.TrimSpace(o); o != "" {
			cfg.OriginPatterns = append(cfg.OriginPatterns, o)
		}
	}

	return cfg, nil
}

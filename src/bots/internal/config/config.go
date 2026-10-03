// Paquete config: variables de entorno de los bots. Ninguna dirección va en
// el código (regla 1 de portabilidad).
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"
)

// Config es la configuración común de todos los escenarios.
type Config struct {
	// BaseURL es la dirección del proxy, p. ej. http://nginx:8080.
	BaseURL       string
	AdminUsername string
	AdminPassword string
	// Count es el número de bots (por defecto 3, el rango de «vecinos»).
	Count int
	// Duration limita la ejecución; 0 significa hasta que se detenga.
	Duration time.Duration
	// Seed fija las decisiones de los bots; solo vale si SeedSet.
	Seed    uint64
	SeedSet bool
	// LogLevel es el nivel de slog.
	LogLevel slog.Level
}

// Load lee la configuración con getenv (os.Getenv en producción). Los
// errores nombran la variable y nunca repiten la contraseña.
func Load(getenv func(string) string) (Config, error) {
	c := Config{
		BaseURL:       strings.TrimSpace(getenv("HV_BASE_URL")),
		AdminUsername: getenv("HV_ADMIN_USERNAME"),
		AdminPassword: getenv("HV_ADMIN_PASSWORD"),
		Count:         3,
		LogLevel:      slog.LevelInfo,
	}
	if c.BaseURL == "" {
		return c, errors.New("HV_BASE_URL is required (e.g. http://nginx:8080)")
	}
	if v := getenv("BOTS_COUNT"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return c, fmt.Errorf("BOTS_COUNT must be a positive integer, got %q", v)
		}
		c.Count = n
	}
	if v := getenv("BOTS_DURATION"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d < 0 {
			return c, fmt.Errorf("BOTS_DURATION must be a non-negative duration such as 30s or 0, got %q", v)
		}
		c.Duration = d
	}
	if v := getenv("BOTS_SEED"); v != "" {
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return c, fmt.Errorf("BOTS_SEED must be an unsigned integer, got %q", v)
		}
		c.Seed, c.SeedSet = n, true
	}
	if v := getenv("LOG_LEVEL"); v != "" {
		if err := c.LogLevel.UnmarshalText([]byte(v)); err != nil {
			return c, fmt.Errorf("LOG_LEVEL must be debug, info, warn or error, got %q", v)
		}
	}
	return c, nil
}

// RequireAdmin comprueba las credenciales necesarias para aprovisionar
// cuentas.
func (c Config) RequireAdmin() error {
	if c.AdminUsername == "" {
		return errors.New("HV_ADMIN_USERNAME is required to provision bot accounts")
	}
	if c.AdminPassword == "" {
		return errors.New("HV_ADMIN_PASSWORD is required to provision bot accounts")
	}
	return nil
}

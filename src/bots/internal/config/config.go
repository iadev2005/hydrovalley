// Paquete config: variables de entorno de los bots. Ninguna dirección va en
// el código (regla 1 de portabilidad).
package config

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config es la configuración común de todos los escenarios.
type Config struct {
	// BaseURL es la dirección del proxy, p. ej. http://nginx:8080.
	BaseURL string
	// TLSInsecure (HV_TLS_INSECURE) omite la verificación del certificado del
	// proxy: solo para el certificado autofirmado del laboratorio.
	TLSInsecure bool
	// CACertPath (HV_CA_CERT) es un certificado PEM que se confía además de
	// los del sistema; es la opción preferible a TLSInsecure.
	CACertPath    string
	AdminUsername string
	AdminPassword string
	// Count es el número de bots (por defecto 3, el rango de «vecinos»).
	Count int
	// Duration limita la ejecución; 0 significa hasta que se detenga.
	Duration time.Duration
	// Seed fija las decisiones de los bots; solo vale si SeedSet.
	Seed    uint64
	SeedSet bool
	// CountSet indica que BOTS_COUNT venía definida.
	CountSet bool
	// Reset pide devolver el valle a su estado inicial al terminar la prueba
	// de carga (BOTS_RESET, por defecto verdadero).
	Reset bool
	// ReportPath es el archivo del informe JSON de la prueba de carga
	// (BOTS_REPORT_PATH); vacío = sin informe.
	ReportPath string
	// SeedPlants (BOTS_FIREFIGHTER_SEED_PLANTS) hace que los bomberos rieguen
	// unas casillas al empezar para que pueda haber incendios. FireBonus
	// (BOTS_FIREFIGHTER_BONUS) son las hojas de la extinción que esperan ver;
	// 0 = el valor del catálogo de arranque (5).
	SeedPlants bool
	FireBonus  uint64
	// LoadRanking (BOTS_LOAD_RANKING) activa la consulta en bucle de
	// GET /api/ranking durante la prueba de carga para medir RNF-03.
	// LoadRankingInterval (BOTS_LOAD_RANKING_INTERVAL) es la espera entre
	// consultas; 2 s por defecto, para quedar por debajo del límite de la API
	// (60 lecturas por minuto y por IP).
	LoadRanking         bool
	LoadRankingInterval time.Duration
	// LoadBaselineReport (BOTS_LOAD_BASELINE_REPORT) es la ruta del informe
	// JSON de una ejecución sin ranking; su p95 es la línea base de RNF-03.
	LoadBaselineReport string
	// LogLevel es el nivel de slog.
	LogLevel slog.Level
}

// Valores de arranque de la prueba de carga (CP-11).
const (
	loadCount    = 30
	loadDuration = 60 * time.Second
)

// WithLoadDefaults aplica los valores de CP-11 (30 bots, 60 s) a lo que el
// entorno no definió. La prueba necesita una duración: 0 también cae a 60 s.
func (c Config) WithLoadDefaults() Config {
	if !c.CountSet {
		c.Count = loadCount
	}
	if c.Duration == 0 {
		c.Duration = loadDuration
	}
	return c
}

// Load lee la configuración con getenv (os.Getenv en producción). Los
// errores nombran la variable y nunca repiten la contraseña.
func Load(getenv func(string) string) (Config, error) {
	c := Config{
		BaseURL:       strings.TrimSpace(getenv("HV_BASE_URL")),
		AdminUsername: getenv("HV_ADMIN_USERNAME"),
		AdminPassword: getenv("HV_ADMIN_PASSWORD"),
		Count:         3,
		Reset:         true,
		ReportPath:    strings.TrimSpace(getenv("BOTS_REPORT_PATH")),
		LogLevel:      slog.LevelInfo,
	}
	if c.BaseURL == "" {
		return c, errors.New("HV_BASE_URL is required (e.g. https://nginx:8080)")
	}
	if v := getenv("BOTS_COUNT"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return c, fmt.Errorf("BOTS_COUNT must be a positive integer, got %q", v)
		}
		c.Count, c.CountSet = n, true
	}
	if v := getenv("BOTS_RESET"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return c, fmt.Errorf("BOTS_RESET must be true or false, got %q", v)
		}
		c.Reset = b
	}
	if v := getenv("HV_TLS_INSECURE"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return c, fmt.Errorf("HV_TLS_INSECURE must be true or false, got %q", v)
		}
		c.TLSInsecure = b
	}
	c.CACertPath = strings.TrimSpace(getenv("HV_CA_CERT"))
	if c.TLSInsecure && c.CACertPath != "" {
		return c, errors.New("HV_TLS_INSECURE and HV_CA_CERT are mutually exclusive: use only one")
	}
	if v := getenv("BOTS_DURATION"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d < 0 {
			return c, fmt.Errorf("BOTS_DURATION must be a non-negative duration such as 30s or 0, got %q", v)
		}
		c.Duration = d
	}
	if v := getenv("BOTS_FIREFIGHTER_SEED_PLANTS"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return c, fmt.Errorf("BOTS_FIREFIGHTER_SEED_PLANTS must be true or false, got %q", v)
		}
		c.SeedPlants = b
	}
	if v := getenv("BOTS_FIREFIGHTER_BONUS"); v != "" {
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil || n < 1 {
			return c, fmt.Errorf("BOTS_FIREFIGHTER_BONUS must be a positive integer, got %q", v)
		}
		c.FireBonus = n
	}
	if v := getenv("BOTS_SEED"); v != "" {
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return c, fmt.Errorf("BOTS_SEED must be an unsigned integer, got %q", v)
		}
		c.Seed, c.SeedSet = n, true
	}
	c.LoadRankingInterval = 2 * time.Second
	if v := getenv("BOTS_LOAD_RANKING"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return c, fmt.Errorf("BOTS_LOAD_RANKING must be true or false, got %q", v)
		}
		c.LoadRanking = b
	}
	if v := getenv("BOTS_LOAD_RANKING_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			return c, fmt.Errorf("BOTS_LOAD_RANKING_INTERVAL must be a positive duration such as 1s or 500ms, got %q", v)
		}
		c.LoadRankingInterval = d
	}
	c.LoadBaselineReport = strings.TrimSpace(getenv("BOTS_LOAD_BASELINE_REPORT"))
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

// WarnIfInsecure avisa una sola vez, a nivel WARN, de que la verificación del
// certificado del proxy está desactivada (HV_TLS_INSECURE=true). Cada
// escenario la llama una vez al crear su registro.
func (c Config) WarnIfInsecure(log *slog.Logger) {
	if c.TLSInsecure {
		log.Warn("HV_TLS_INSECURE=true: the proxy certificate is NOT verified; use HV_CA_CERT outside the Compose network")
	}
}

// TLSConfig arma la configuración TLS de los clientes: nil si no se pidió
// nada (se usan los certificados del sistema), una que omite la verificación
// con HV_TLS_INSECURE, o una que confía en el certificado de HV_CA_CERT además
// de los del sistema.
func (c Config) TLSConfig() (*tls.Config, error) {
	switch {
	case c.TLSInsecure:
		return &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12}, nil //nolint:gosec // opt-in para el certificado autofirmado del laboratorio
	case c.CACertPath != "":
		pemBytes, err := os.ReadFile(c.CACertPath)
		if err != nil {
			return nil, fmt.Errorf("HV_CA_CERT: cannot read %q: %w", c.CACertPath, err)
		}
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(pemBytes) {
			return nil, fmt.Errorf("HV_CA_CERT: %q has no PEM certificate", c.CACertPath)
		}
		return &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}, nil
	}
	return nil, nil
}

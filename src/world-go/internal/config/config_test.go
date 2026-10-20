package config

import (
	"log/slog"
	"testing"
	"time"
)

// Portadas de las pruebas de src/world/src/config.rs.

func env(pairs ...string) func(string) string {
	values := map[string]string{}
	for i := 0; i < len(pairs); i += 2 {
		values[pairs[i]] = pairs[i+1]
	}
	return func(name string) string { return values[name] }
}

func TestListenDefaultsToAllInterfacesOn50051(t *testing.T) {
	cfg, err := Load(env("PERSISTENCE_ADDR", "persistence:50051"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != "0.0.0.0:50051" || cfg.PersistenceAddr != "persistence:50051" {
		t.Fatalf("configuración: %+v", cfg)
	}
}

func TestListenCanBeOverridden(t *testing.T) {
	cfg, err := Load(env("WORLD_LISTEN", "127.0.0.1:6000", "PERSISTENCE_ADDR", "10.68.17.2:50051"))
	if err != nil || cfg.Listen != "127.0.0.1:6000" {
		t.Fatalf("(%+v, %v)", cfg, err)
	}
}

func TestAnInvalidListenAddressIsReported(t *testing.T) {
	for _, bad := range []string{"world:50051", "0.0.0.0", "0.0.0.0:abc", "0.0.0.0:70000"} {
		_, err := Load(env("WORLD_LISTEN", bad, "PERSISTENCE_ADDR", "persistence:50051"))
		if err == nil {
			t.Errorf("%q debería ser inválida", bad)
		}
	}
}

func TestPersistenceAddrIsRequired(t *testing.T) {
	for _, value := range []string{"", "  "} {
		if _, err := Load(env("PERSISTENCE_ADDR", value)); err == nil {
			t.Errorf("PERSISTENCE_ADDR=%q debería faltar", value)
		}
	}
}

func TestTheSeedIsOptionalAndParsedAsUint64(t *testing.T) {
	load := func(value string) (Config, error) {
		return Load(env("PERSISTENCE_ADDR", "persistence:50051", "WORLD_SEED", value))
	}
	if cfg, _ := load(""); cfg.SeedFromEnv {
		t.Error("vacía = sin definir")
	}
	if cfg, err := load(" 42 "); err != nil || !cfg.SeedFromEnv || cfg.Seed != 42 {
		t.Errorf("42: (%+v, %v)", cfg, err)
	}
	if cfg, err := load("18446744073709551615"); err != nil || cfg.Seed != ^uint64(0) {
		t.Errorf("u64::MAX: (%+v, %v)", cfg, err)
	}
	for _, bad := range []string{"-1", "1.5", "abc", "18446744073709551616"} {
		if _, err := load(bad); err == nil {
			t.Errorf("WORLD_SEED=%q debería ser inválida", bad)
		}
	}
}

func TestWithoutASeedItComesFromTheClock(t *testing.T) {
	cfg, err := Load(env("PERSISTENCE_ADDR", "persistence:50051"))
	if err != nil || cfg.SeedFromEnv {
		t.Fatalf("(%+v, %v)", cfg, err)
	}
	if cfg.Seed == 0 {
		t.Error("la semilla del reloj no debería ser 0")
	}
}

func TestTheClockSeedIsTheNanosecondsSinceTheEpoch(t *testing.T) {
	at := time.Unix(1_790_000_000, 123_456_789)
	if got := SeedFromTime(at); got != uint64(at.UnixNano()) {
		t.Errorf("semilla: %d", got)
	}
	if SeedFromTime(at) == SeedFromTime(at.Add(time.Nanosecond)) {
		t.Error("dos lecturas que difieren en 1 ns dan semillas distintas")
	}
}

func TestLogLevelFollowsTheRustNamesAndFallsBackToInfo(t *testing.T) {
	cases := map[string]slog.Level{
		"":      slog.LevelInfo,
		"info":  slog.LevelInfo,
		"debug": slog.LevelDebug,
		"DEBUG": slog.LevelDebug,
		"warn":  slog.LevelWarn,
		"error": slog.LevelError,
		"trace": slog.LevelDebug - 4,
		// Como EnvFilter en Rust: un valor que no se entiende deja el nivel info.
		"verboso": slog.LevelInfo,
	}
	for value, want := range cases {
		cfg, err := Load(env("PERSISTENCE_ADDR", "persistence:50051", "LOG_LEVEL", value))
		if err != nil || cfg.LogLevel != want {
			t.Errorf("LOG_LEVEL=%q: (%v, %v), quiero %v", value, cfg.LogLevel, err, want)
		}
	}
}

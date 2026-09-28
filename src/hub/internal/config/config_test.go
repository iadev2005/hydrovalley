package config

import (
	"log/slog"
	"testing"
)

func env(values map[string]string) func(string) string {
	return func(k string) string { return values[k] }
}

func TestDefaults(t *testing.T) {
	cfg, err := Load(env(map[string]string{"WORLD_ADDR": "world:50051"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != ":8081" || cfg.WorldAddr != "world:50051" || cfg.LogLevel != slog.LevelInfo {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestOverrides(t *testing.T) {
	cfg, err := Load(env(map[string]string{
		"WORLD_ADDR": " 10.68.17.2:50051 ", "HUB_LISTEN": "0.0.0.0:9000", "LOG_LEVEL": "debug",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != "0.0.0.0:9000" || cfg.WorldAddr != "10.68.17.2:50051" || cfg.LogLevel != slog.LevelDebug {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestWorldAddrIsRequired(t *testing.T) {
	if _, err := Load(env(map[string]string{"WORLD_ADDR": "  "})); err == nil {
		t.Fatal("missing WORLD_ADDR must be an error")
	}
}

func TestInvalidLogLevel(t *testing.T) {
	if _, err := Load(env(map[string]string{"WORLD_ADDR": "world:50051", "LOG_LEVEL": "loud"})); err == nil {
		t.Fatal("unknown LOG_LEVEL must be an error")
	}
}

func TestOriginPatterns(t *testing.T) {
	cfg, err := Load(env(map[string]string{"WORLD_ADDR": "world:50051", "HUB_ORIGIN_PATTERNS": " 10.68.17.1:8080 , localhost:5173,"}))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.OriginPatterns) != 2 || cfg.OriginPatterns[0] != "10.68.17.1:8080" || cfg.OriginPatterns[1] != "localhost:5173" {
		t.Fatalf("OriginPatterns = %q", cfg.OriginPatterns)
	}
	cfg, _ = Load(env(map[string]string{"WORLD_ADDR": "world:50051"}))
	if len(cfg.OriginPatterns) != 0 {
		t.Fatalf("default OriginPatterns = %q, want none (same origin only)", cfg.OriginPatterns)
	}
}

package config

import (
	"log/slog"
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestLoadDefaults(t *testing.T) {
	c, err := Load(env(map[string]string{"HV_BASE_URL": "http://nginx:8080"}))
	if err != nil {
		t.Fatal(err)
	}
	if c.BaseURL != "http://nginx:8080" || c.Count != 3 || c.Duration != 0 || c.SeedSet || c.LogLevel != slog.LevelInfo {
		t.Fatalf("unexpected defaults %+v", c)
	}
}

func TestLoadParsesAllVariables(t *testing.T) {
	c, err := Load(env(map[string]string{
		"HV_BASE_URL": "https://h/", "HV_ADMIN_USERNAME": "admin", "HV_ADMIN_PASSWORD": "pw",
		"BOTS_COUNT": "5", "BOTS_DURATION": "30s", "BOTS_SEED": "42", "LOG_LEVEL": "debug",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Count != 5 || c.Duration != 30*time.Second || !c.SeedSet || c.Seed != 42 || c.LogLevel != slog.LevelDebug ||
		c.AdminUsername != "admin" || c.AdminPassword != "pw" {
		t.Fatalf("unexpected config %+v", c)
	}
}

func TestLoadRejectsBadValuesNamingTheVariable(t *testing.T) {
	cases := map[string]map[string]string{
		"HV_BASE_URL":   {},
		"BOTS_COUNT":    {"HV_BASE_URL": "http://x", "BOTS_COUNT": "0"},
		"BOTS_DURATION": {"HV_BASE_URL": "http://x", "BOTS_DURATION": "-1s"},
		"BOTS_SEED":     {"HV_BASE_URL": "http://x", "BOTS_SEED": "abc"},
		"LOG_LEVEL":     {"HV_BASE_URL": "http://x", "LOG_LEVEL": "loud"},
	}
	for name, m := range cases {
		_, err := Load(env(m))
		if err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("%s: want error naming the variable, got %v", name, err)
		}
	}
}

func TestRequireAdmin(t *testing.T) {
	c := Config{}
	if err := c.RequireAdmin(); err == nil || !strings.Contains(err.Error(), "HV_ADMIN_USERNAME") {
		t.Fatalf("want error naming HV_ADMIN_USERNAME, got %v", err)
	}
	c = Config{AdminUsername: "a", AdminPassword: "p"}
	if err := c.RequireAdmin(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadReadsResetAndReportPath(t *testing.T) {
	c, err := Load(env(map[string]string{"HV_BASE_URL": "http://x"}))
	if err != nil {
		t.Fatal(err)
	}
	if !c.Reset || c.ReportPath != "" {
		t.Fatalf("defaults must reset the valley and write no report: %+v", c)
	}
	c, err = Load(env(map[string]string{"HV_BASE_URL": "http://x", "BOTS_RESET": "false", "BOTS_REPORT_PATH": "/tmp/r.json"}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Reset || c.ReportPath != "/tmp/r.json" {
		t.Fatalf("unexpected config %+v", c)
	}
	if _, err := Load(env(map[string]string{"HV_BASE_URL": "http://x", "BOTS_RESET": "maybe"})); err == nil || !strings.Contains(err.Error(), "BOTS_RESET") {
		t.Fatalf("want an error naming BOTS_RESET, got %v", err)
	}
}

func TestWithLoadDefaultsOnlyFillsWhatWasNotSet(t *testing.T) {
	c, _ := Load(env(map[string]string{"HV_BASE_URL": "http://x"}))
	c = c.WithLoadDefaults()
	if c.Count != 30 || c.Duration != 60*time.Second {
		t.Fatalf("CP-11 defaults are 30 bots for 60 s, got %d for %v", c.Count, c.Duration)
	}
	c, _ = Load(env(map[string]string{"HV_BASE_URL": "http://x", "BOTS_COUNT": "10", "BOTS_DURATION": "5s"}))
	c = c.WithLoadDefaults()
	if c.Count != 10 || c.Duration != 5*time.Second {
		t.Fatalf("explicit values must win, got %d for %v", c.Count, c.Duration)
	}
	c, _ = Load(env(map[string]string{"HV_BASE_URL": "http://x", "BOTS_DURATION": "0"}))
	if c = c.WithLoadDefaults(); c.Duration != 60*time.Second {
		t.Fatalf("a load test needs a duration, 0 falls back to 60 s, got %v", c.Duration)
	}
}

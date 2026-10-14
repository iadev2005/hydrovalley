package config

import (
	"crypto/x509"
	"encoding/pem"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func TestLoadFirefighterVariables(t *testing.T) {
	c, err := Load(env(map[string]string{"HV_BASE_URL": "http://x"}))
	if err != nil || c.SeedPlants || c.FireBonus != 0 {
		t.Fatalf("defaults: %+v %v", c, err)
	}
	c, err = Load(env(map[string]string{"HV_BASE_URL": "http://x", "BOTS_FIREFIGHTER_SEED_PLANTS": "true", "BOTS_FIREFIGHTER_BONUS": "7"}))
	if err != nil || !c.SeedPlants || c.FireBonus != 7 {
		t.Fatalf("parsed: %+v %v", c, err)
	}
	for name, v := range map[string]string{"BOTS_FIREFIGHTER_SEED_PLANTS": "maybe", "BOTS_FIREFIGHTER_BONUS": "0"} {
		_, err := Load(env(map[string]string{"HV_BASE_URL": "http://x", name: v}))
		if err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("%s: want error naming the variable, got %v", name, err)
		}
	}
}

func TestLoadTLSVariables(t *testing.T) {
	base := map[string]string{"HV_BASE_URL": "https://nginx:8080"}
	c, err := Load(env(base))
	if err != nil || c.TLSInsecure || c.CACertPath != "" {
		t.Fatalf("defaults: %+v, %v", c, err)
	}
	c, err = Load(env(map[string]string{"HV_BASE_URL": "https://h", "HV_TLS_INSECURE": "true"}))
	if err != nil || !c.TLSInsecure {
		t.Fatalf("HV_TLS_INSECURE=true: %+v, %v", c, err)
	}
	c, err = Load(env(map[string]string{"HV_BASE_URL": "https://h", "HV_CA_CERT": " /certs/ca.crt "}))
	if err != nil || c.CACertPath != "/certs/ca.crt" {
		t.Fatalf("HV_CA_CERT: %+v, %v", c, err)
	}
}

func TestLoadTLSRejectsBadValuesNamingTheVariable(t *testing.T) {
	cases := map[string]map[string]string{
		"HV_TLS_INSECURE": {"HV_BASE_URL": "https://h", "HV_TLS_INSECURE": "maybe"},
		"HV_CA_CERT":      {"HV_BASE_URL": "https://h", "HV_TLS_INSECURE": "true", "HV_CA_CERT": "/x.crt"},
	}
	for name, vars := range cases {
		_, err := Load(env(vars))
		if err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("%s: want an error naming the variable, got %v", name, err)
		}
	}
}

func TestLoadRankingVariables(t *testing.T) {
	c, err := Load(env(map[string]string{"HV_BASE_URL": "http://x"}))
	if err != nil || c.LoadRanking || c.LoadRankingInterval != DefaultLoadRankingInterval || DefaultLoadRankingInterval != 2*time.Second || c.LoadBaselineReport != "" {
		t.Fatalf("defaults: %+v %v", c, err)
	}
	c, err = Load(env(map[string]string{
		"HV_BASE_URL":                "http://x",
		"BOTS_LOAD_RANKING":          "true",
		"BOTS_LOAD_RANKING_INTERVAL": "500ms",
		"BOTS_LOAD_BASELINE_REPORT":  " /reports/load-baseline.json ",
	}))
	if err != nil || !c.LoadRanking || c.LoadRankingInterval != 500*time.Millisecond || c.LoadBaselineReport != "/reports/load-baseline.json" {
		t.Fatalf("parsed: %+v %v", c, err)
	}
	for name, v := range map[string]string{
		"BOTS_LOAD_RANKING":               "maybe",
		"BOTS_LOAD_RANKING_INTERVAL":      "invalid",
		"BOTS_LOAD_RANKING_INTERVAL_ZERO": "0s",
	} {
		varName := name
		if name == "BOTS_LOAD_RANKING_INTERVAL_ZERO" {
			varName = "BOTS_LOAD_RANKING_INTERVAL"
		}
		_, err := Load(env(map[string]string{"HV_BASE_URL": "http://x", varName: v}))
		if err == nil || !strings.Contains(err.Error(), varName) {
			t.Errorf("%s: want error naming %s, got %v", name, varName, err)
		}
	}
}

func TestTLSConfigIsNilByDefault(t *testing.T) {
	got, err := Config{}.TLSConfig()
	if err != nil || got != nil {
		t.Fatalf("TLSConfig() = %v, %v; want nil, nil", got, err)
	}
}

func TestTLSConfigInsecureSkipsVerification(t *testing.T) {
	got, err := Config{TLSInsecure: true}.TLSConfig()
	if err != nil || got == nil || !got.InsecureSkipVerify {
		t.Fatalf("TLSConfig() = %+v, %v", got, err)
	}
}

func TestTLSConfigTrustsTheGivenCertificate(t *testing.T) {
	srv := httptest.NewTLSServer(http.NotFoundHandler())
	defer srv.Close()
	path := filepath.Join(t.TempDir(), "ca.crt")
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	if err := os.WriteFile(path, pemBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Config{CACertPath: path}.TLSConfig()
	if err != nil || got == nil || got.InsecureSkipVerify || got.RootCAs == nil {
		t.Fatalf("TLSConfig() = %+v, %v", got, err)
	}
	if _, err := srv.Certificate().Verify(x509.VerifyOptions{Roots: got.RootCAs}); err != nil {
		t.Fatalf("the given certificate is not trusted: %v", err)
	}
}

func TestTLSConfigReportsUnreadableOrInvalidCertificate(t *testing.T) {
	_, err := Config{CACertPath: filepath.Join(t.TempDir(), "missing.crt")}.TLSConfig()
	if err == nil || !strings.Contains(err.Error(), "HV_CA_CERT") {
		t.Fatalf("missing file: got %v", err)
	}
	bad := filepath.Join(t.TempDir(), "bad.crt")
	if werr := os.WriteFile(bad, []byte("not a pem"), 0o600); werr != nil {
		t.Fatal(werr)
	}
	_, err = Config{CACertPath: bad}.TLSConfig()
	if err == nil || !strings.Contains(err.Error(), "HV_CA_CERT") {
		t.Fatalf("invalid PEM: got %v", err)
	}
}

func TestWarnIfInsecureLogsOnceOnlyWhenVerificationIsOff(t *testing.T) {
	var buf strings.Builder
	log := slog.New(slog.NewTextHandler(&buf, nil))

	Config{}.WarnIfInsecure(log)
	Config{CACertPath: "/certs/tls.crt"}.WarnIfInsecure(log)
	if buf.Len() != 0 {
		t.Fatalf("verification on must not warn, got %q", buf.String())
	}

	Config{TLSInsecure: true}.WarnIfInsecure(log)
	out := buf.String()
	if strings.Count(out, "\n") != 1 || !strings.Contains(out, "level=WARN") || !strings.Contains(out, "HV_TLS_INSECURE") {
		t.Fatalf("want a single WARN naming HV_TLS_INSECURE, got %q", out)
	}
}

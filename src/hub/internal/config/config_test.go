package config

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"log/slog"
	"strings"
	"testing"
)

var testPub, testPubPEM = func() (ed25519.PublicKey, string) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		panic(err)
	}
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		panic(err)
	}
	return pub, string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}()

// env devuelve un getenv con una TOKEN_PUBLIC_KEY válida, salvo que el mapa
// la defina (también vacía).
func env(values map[string]string) func(string) string {
	return func(k string) string {
		if v, ok := values[k]; ok {
			return v
		}
		if k == "TOKEN_PUBLIC_KEY" {
			return testPubPEM
		}
		return ""
	}
}

func TestDefaults(t *testing.T) {
	cfg, err := Load(env(map[string]string{"WORLD_ADDR": "world:50051"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != ":8081" || cfg.WorldAddr != "world:50051" || cfg.HubGeneration != 1 || cfg.PrimaryHealthURL != "" || cfg.LogLevel != slog.LevelInfo {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestOverrides(t *testing.T) {
	cfg, err := Load(env(map[string]string{
		"WORLD_ADDR":             " 10.68.17.2:50051 ",
		"HUB_LISTEN":             "0.0.0.0:9000",
		"HUB_GENERATION":         " 2 ",
		"HUB_PRIMARY_HEALTH_URL": " http://10.68.17.12:8081/healthz ",
		"LOG_LEVEL":              "debug",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != "0.0.0.0:9000" || cfg.WorldAddr != "10.68.17.2:50051" || cfg.HubGeneration != 2 || cfg.PrimaryHealthURL != "http://10.68.17.12:8081/healthz" || cfg.LogLevel != slog.LevelDebug {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestInvalidHubGeneration(t *testing.T) {
	for _, bad := range []string{"0", "-1", "abc"} {
		if _, err := Load(env(map[string]string{"WORLD_ADDR": "world:50051", "HUB_GENERATION": bad})); err == nil {
			t.Fatalf("HUB_GENERATION=%q must be an error", bad)
		}
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

func TestTokenPublicKeyIsLoaded(t *testing.T) {
	cfg, err := Load(env(map[string]string{"WORLD_ADDR": "world:50051"}))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.TokenKey.Equal(testPub) {
		t.Fatal("TokenKey differs from TOKEN_PUBLIC_KEY")
	}
}

func TestTokenPublicKeyIsRequiredAndNamedInTheError(t *testing.T) {
	for name, v := range map[string]string{"missing": "", "blank": "  ", "garbage": "not-a-key", "private key": "-----BEGIN PRIVATE KEY-----\nAAAA\n-----END PRIVATE KEY-----"} {
		t.Run(name, func(t *testing.T) {
			_, err := Load(env(map[string]string{"WORLD_ADDR": "world:50051", "TOKEN_PUBLIC_KEY": v}))
			if err == nil || !strings.Contains(err.Error(), "TOKEN_PUBLIC_KEY") {
				t.Fatalf("err = %v, want an error naming TOKEN_PUBLIC_KEY", err)
			}
			if v = strings.TrimSpace(v); v != "" && strings.Contains(err.Error(), v) {
				t.Fatalf("error repeats the value: %v", err)
			}
		})
	}
}

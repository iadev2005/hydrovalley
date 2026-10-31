package config

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"log/slog"
	"testing"
)

func validPublicKeyPEM(t *testing.T) string {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatalf("MarshalPKIXPublicKey: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

func TestLoad_Defaults(t *testing.T) {
	keyPEM := validPublicKeyPEM(t)
	env := map[string]string{
		"TOKEN_PUBLIC_KEY": keyPEM,
	}
	cfg, err := Load(func(k string) string { return env[k] })
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.HTTPListen != ":8082" {
		t.Errorf("HTTPListen: got %q, want :8082", cfg.HTTPListen)
	}
	if cfg.GRPCListen != ":50051" {
		t.Errorf("GRPCListen: got %q, want :50051", cfg.GRPCListen)
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Errorf("LogLevel: got %v, want LevelInfo", cfg.LogLevel)
	}
	if len(cfg.OriginPatterns) != 0 {
		t.Errorf("OriginPatterns: got %v, want empty", cfg.OriginPatterns)
	}
}

func TestLoad_Custom(t *testing.T) {
	keyPEM := validPublicKeyPEM(t)
	env := map[string]string{
		"TOKEN_PUBLIC_KEY":     keyPEM,
		"CHAT_HTTP_LISTEN":     "0.0.0.0:9000",
		"CHAT_GRPC_LISTEN":     "0.0.0.0:9001",
		"LOG_LEVEL":            "debug",
		"CHAT_ORIGIN_PATTERNS": "example.com:8080, localhost:*",
	}
	cfg, err := Load(func(k string) string { return env[k] })
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.HTTPListen != "0.0.0.0:9000" {
		t.Errorf("HTTPListen: got %q", cfg.HTTPListen)
	}
	if cfg.GRPCListen != "0.0.0.0:9001" {
		t.Errorf("GRPCListen: got %q", cfg.GRPCListen)
	}
	if cfg.LogLevel != slog.LevelDebug {
		t.Errorf("LogLevel: got %v, want LevelDebug", cfg.LogLevel)
	}
	if len(cfg.OriginPatterns) != 2 || cfg.OriginPatterns[0] != "example.com:8080" || cfg.OriginPatterns[1] != "localhost:*" {
		t.Errorf("OriginPatterns: got %v", cfg.OriginPatterns)
	}
}

func TestLoad_MissingKey(t *testing.T) {
	_, err := Load(func(string) string { return "" })
	if err == nil {
		t.Fatal("expected error when TOKEN_PUBLIC_KEY is missing")
	}
}

func TestLoad_InvalidLogLevel(t *testing.T) {
	keyPEM := validPublicKeyPEM(t)
	env := map[string]string{
		"TOKEN_PUBLIC_KEY": keyPEM,
		"LOG_LEVEL":        "verbose",
	}
	_, err := Load(func(k string) string { return env[k] })
	if err == nil {
		t.Fatal("expected error with invalid LOG_LEVEL")
	}
}

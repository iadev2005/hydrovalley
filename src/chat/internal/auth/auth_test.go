package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type fixture struct {
	pub  ed25519.PublicKey
	priv ed25519.PrivateKey
	now  time.Time
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	return fixture{
		pub:  pub,
		priv: priv,
		now:  time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC),
	}
}

func (f fixture) pemSPKI(t *testing.T) string {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(f.pub)
	if err != nil {
		t.Fatalf("MarshalPKIXPublicKey: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

func sign(t *testing.T, priv ed25519.PrivateKey, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	s, err := token.SignedString(priv)
	if err != nil {
		t.Fatalf("SignedString: %v", err)
	}
	return s
}

func defaultClaims(now time.Time) jwt.MapClaims {
	return jwt.MapClaims{
		"iss":       Issuer,
		"aud":       Audience,
		"sub":       "00000000-0000-4000-8000-000000000001",
		"player_id": "00000000-0000-4000-8000-000000000001",
		"username":  "ana",
		"role":      "player",
		"exp":       now.Add(12 * time.Hour).Unix(),
		"iat":       now.Unix(),
	}
}

func TestVerify_Valid(t *testing.T) {
	f := newFixture(t)
	v := NewVerifier(f.pub, func() time.Time { return f.now })

	tok := sign(t, f.priv, defaultClaims(f.now))
	id, err := v.Verify(tok)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if id.PlayerID != "00000000-0000-4000-8000-000000000001" {
		t.Errorf("PlayerID: got %q", id.PlayerID)
	}
	if id.Username != "ana" {
		t.Errorf("Username: got %q", id.Username)
	}
	if id.Role != RolePlayer {
		t.Errorf("Role: got %q", id.Role)
	}
}

func TestVerify_AdminRole(t *testing.T) {
	f := newFixture(t)
	v := NewVerifier(f.pub, func() time.Time { return f.now })

	claims := defaultClaims(f.now)
	claims["role"] = "admin"
	tok := sign(t, f.priv, claims)
	id, err := v.Verify(tok)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if id.Role != RoleAdmin {
		t.Errorf("Role: got %q, want admin", id.Role)
	}
}

func TestVerify_Invalid(t *testing.T) {
	f := newFixture(t)
	v := NewVerifier(f.pub, func() time.Time { return f.now })

	_, otherPriv, _ := ed25519.GenerateKey(rand.Reader)

	cases := []struct {
		name   string
		claims func() jwt.MapClaims
		priv   ed25519.PrivateKey
	}{
		{"wrong key", func() jwt.MapClaims { return defaultClaims(f.now) }, otherPriv},
		{"expired", func() jwt.MapClaims {
			c := defaultClaims(f.now)
			c["exp"] = f.now.Add(-time.Hour).Unix()
			return c
		}, f.priv},
		{"wrong issuer", func() jwt.MapClaims {
			c := defaultClaims(f.now)
			c["iss"] = "other-issuer"
			return c
		}, f.priv},
		{"wrong audience", func() jwt.MapClaims {
			c := defaultClaims(f.now)
			c["aud"] = "other-aud"
			return c
		}, f.priv},
		{"sub differs from player_id", func() jwt.MapClaims {
			c := defaultClaims(f.now)
			c["sub"] = "00000000-0000-4000-8000-000000000002"
			return c
		}, f.priv},
		{"player_id not uuid", func() jwt.MapClaims {
			c := defaultClaims(f.now)
			c["player_id"] = "invalid-uuid"
			c["sub"] = "invalid-uuid"
			return c
		}, f.priv},
		{"username missing", func() jwt.MapClaims {
			c := defaultClaims(f.now)
			c["username"] = "   "
			return c
		}, f.priv},
		{"unknown role", func() jwt.MapClaims {
			c := defaultClaims(f.now)
			c["role"] = "superadmin"
			return c
		}, f.priv},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tok := sign(t, tc.priv, tc.claims())
			_, err := v.Verify(tok)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !errors.Is(err, ErrInvalidToken) {
				t.Errorf("got error %v, want ErrInvalidToken", err)
			}
		})
	}
}

func TestParsePublicKey(t *testing.T) {
	f := newFixture(t)
	pemStr := f.pemSPKI(t)

	// Direct PEM
	key, err := ParsePublicKey(pemStr)
	if err != nil {
		t.Fatalf("ParsePublicKey(PEM): %v", err)
	}
	if !key.Equal(f.pub) {
		t.Errorf("keys differ")
	}

	// Base64 of PEM
	b64 := base64.StdEncoding.EncodeToString([]byte(pemStr))
	key2, err := ParsePublicKey(b64)
	if err != nil {
		t.Fatalf("ParsePublicKey(base64): %v", err)
	}
	if !key2.Equal(f.pub) {
		t.Errorf("keys differ")
	}

	// Escaped newlines
	escaped := string(pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: key,
	}))
	_ = escaped

	// Invalid
	if _, err := ParsePublicKey(""); err == nil {
		t.Error("expected error for empty key")
	}
	if _, err := ParsePublicKey("not-a-valid-key"); err == nil {
		t.Error("expected error for invalid key")
	}
}

func TestIsUUID(t *testing.T) {
	valid := "6c1f0d3a-8b7e-4c2d-9a10-3f4e5d6c7b8a"
	if !IsUUID(valid) {
		t.Errorf("expected %q to be valid UUID", valid)
	}
	invalid := []string{
		"",
		"6c1f0d3a-8b7e-4c2d-9a10",
		"6c1f0d3a-8b7e-4c2d-9a10-3f4e5d6c7b8Z",
		"6c1f0d3a_8b7e_4c2d_9a10_3f4e5d6c7b8a",
	}
	for _, inv := range invalid {
		if IsUUID(inv) {
			t.Errorf("expected %q to be invalid UUID", inv)
		}
	}
}

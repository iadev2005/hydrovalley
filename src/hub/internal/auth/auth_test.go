package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	ana = "11111111-1111-4111-8111-111111111111"
	bob = "22222222-2222-4222-8222-222222222222"
)

var now = time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)

type fixture struct {
	pub  ed25519.PublicKey
	priv ed25519.PrivateKey
	v    *Verifier
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return fixture{pub: pub, priv: priv, v: NewVerifier(pub, func() time.Time { return now })}
}

// claims devuelve los campos de un token válido; cada prueba cambia uno.
func claims() jwt.MapClaims {
	return jwt.MapClaims{
		"sub": ana, "player_id": ana, "username": "Ana", "role": "player",
		"iat": now.Unix(), "exp": now.Add(12 * time.Hour).Unix(),
	}
}

func sign(t *testing.T, method jwt.SigningMethod, key any, c jwt.MapClaims) string {
	t.Helper()
	s, err := jwt.NewWithClaims(method, c).SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestVerifyValidToken(t *testing.T) {
	f := newFixture(t)
	id, err := f.v.Verify(sign(t, jwt.SigningMethodEdDSA, f.priv, claims()))
	if err != nil {
		t.Fatal(err)
	}
	if id.PlayerID != ana || id.Username != "Ana" || id.Role != RolePlayer {
		t.Fatalf("identity = %+v", id)
	}
}

func TestVerifyAdminRole(t *testing.T) {
	f := newFixture(t)
	c := claims()
	c["role"] = "admin"
	id, err := f.v.Verify(sign(t, jwt.SigningMethodEdDSA, f.priv, c))
	if err != nil || id.Role != RoleAdmin {
		t.Fatalf("identity = %+v, err = %v", id, err)
	}
}

func TestVerifyRejects(t *testing.T) {
	f := newFixture(t)
	_, otherPriv, _ := ed25519.GenerateKey(rand.Reader)
	with := func(k string, v any) jwt.MapClaims {
		c := claims()
		if v == nil {
			delete(c, k)
		} else {
			c[k] = v
		}
		return c
	}
	good := sign(t, jwt.SigningMethodEdDSA, f.priv, claims())
	parts := strings.Split(good, ".")
	tampered := parts[0] + "." + base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"`+bob+`","player_id":"`+bob+`","username":"Bob","role":"admin","exp":`+"9999999999"+`}`)) + "." + parts[2]
	noneToken := jwt.NewWithClaims(jwt.SigningMethodNone, claims())
	none, _ := noneToken.SignedString(jwt.UnsafeAllowNoneSignatureType)
	hmacTok := sign(t, jwt.SigningMethodHS256, []byte(f.pub), claims())

	cases := []struct{ name, token string }{
		{"empty", ""},
		{"garbage", "not-a-jwt"},
		{"signed by another key", sign(t, jwt.SigningMethodEdDSA, otherPriv, claims())},
		{"tampered payload", tampered},
		{"alg none", none},
		{"alg HS256 keyed with the public key", hmacTok},
		{"expired", sign(t, jwt.SigningMethodEdDSA, f.priv, with("exp", now.Add(-time.Minute).Unix()))},
		{"missing exp", sign(t, jwt.SigningMethodEdDSA, f.priv, with("exp", nil))},
		{"missing sub", sign(t, jwt.SigningMethodEdDSA, f.priv, with("sub", nil))},
		{"sub differs from player_id", sign(t, jwt.SigningMethodEdDSA, f.priv, with("sub", bob))},
		{"player_id not a uuid", sign(t, jwt.SigningMethodEdDSA, f.priv, with("player_id", "ana"))},
		{"missing username", sign(t, jwt.SigningMethodEdDSA, f.priv, with("username", nil))},
		{"blank username", sign(t, jwt.SigningMethodEdDSA, f.priv, with("username", "  "))},
		{"unknown role", sign(t, jwt.SigningMethodEdDSA, f.priv, with("role", "root"))},
		{"missing role", sign(t, jwt.SigningMethodEdDSA, f.priv, with("role", nil))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if id, err := f.v.Verify(tc.token); err == nil {
				t.Fatalf("token accepted as %+v", id)
			}
		})
	}
}

func TestVerifyClockSkew(t *testing.T) {
	f := newFixture(t)
	within := claims()
	within["exp"] = now.Add(-20 * time.Second).Unix()
	if _, err := f.v.Verify(sign(t, jwt.SigningMethodEdDSA, f.priv, within)); err != nil {
		t.Fatalf("token expired 20 s ago must pass with 30 s of skew: %v", err)
	}
	beyond := claims()
	beyond["exp"] = now.Add(-40 * time.Second).Unix()
	if _, err := f.v.Verify(sign(t, jwt.SigningMethodEdDSA, f.priv, beyond)); err == nil {
		t.Fatal("token expired 40 s ago must be rejected")
	}
}

func TestParsePublicKey(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
	for name, in := range map[string]string{
		"pem":                 string(pemBytes),
		"pem with spaces":     "\n  " + string(pemBytes) + "\n",
		"pem with \\n escape": strings.ReplaceAll(strings.TrimSpace(string(pemBytes)), "\n", `\n`),
		"base64 of the pem":   base64.StdEncoding.EncodeToString(pemBytes),
	} {
		t.Run(name, func(t *testing.T) {
			got, err := ParsePublicKey(in)
			if err != nil {
				t.Fatal(err)
			}
			if !got.Equal(pub) {
				t.Fatal("parsed key differs")
			}
		})
	}
}

func TestParsePublicKeyErrors(t *testing.T) {
	rsaLike := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: []byte("junk")})
	for name, in := range map[string]string{
		"empty":       "",
		"garbage":     "hello",
		"junk in pem": string(rsaLike),
		"private key": "-----BEGIN PRIVATE KEY-----\nAAAA\n-----END PRIVATE KEY-----",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ParsePublicKey(in)
			if err == nil {
				t.Fatal("expected an error")
			}
			if strings.Contains(err.Error(), in) && in != "" {
				t.Fatalf("error repeats the key value: %v", err)
			}
		})
	}
}

// Package auth verifica el token de acceso que la API firma con Ed25519
// (diagrama 09). El hub solo tiene la clave pública: verifica por sí solo,
// sin llamar a nadie, y de ahí saca la identidad de la sesión (player_id,
// username y rol).
//
// Biblioteca: github.com/golang-jwt/jwt/v5. Está mantenida y su API de
// validación es cerrada: con WithValidMethods solo se acepta `EdDSA`, así que
// `none` y los HMAC (el ataque de confusión de algoritmo, con la clave
// pública como secreto) quedan fuera antes de mirar la firma. Hacerlo a mano
// con crypto/ed25519 obligaba a reescribir el análisis de la cabecera, el
// plazo y los campos, que es justo donde se cometen los errores.
package auth

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Role es el rol del token (RF-31).
type Role string

// Roles válidos del token.
const (
	RolePlayer Role = "player"
	RoleAdmin  Role = "admin"
)

// clockSkew es la tolerancia con el reloj de la API al comprobar `exp`.
const clockSkew = 30 * time.Second

// Identity es la identidad de la sesión: sale solo del token verificado.
type Identity struct {
	PlayerID string
	Username string
	Role     Role
}

// ErrInvalidToken envuelve todo rechazo de un token.
var ErrInvalidToken = errors.New("invalid token")

type tokenClaims struct {
	PlayerID string `json:"player_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

// Verifier comprueba tokens con una clave pública Ed25519.
type Verifier struct {
	key    ed25519.PublicKey
	parser *jwt.Parser
}

// NewVerifier crea el verificador; now es el reloj (time.Now en producción).
func NewVerifier(key ed25519.PublicKey, now func() time.Time) *Verifier {
	return &Verifier{
		key: key,
		parser: jwt.NewParser(
			jwt.WithValidMethods([]string{jwt.SigningMethodEdDSA.Alg()}),
			jwt.WithExpirationRequired(),
			jwt.WithLeeway(clockSkew),
			jwt.WithTimeFunc(now),
		),
	}
}

// Verify comprueba la firma (solo EdDSA), que `exp` siga en el futuro (con
// 30 s de margen), que `player_id` sea un UUID igual a `sub`, que `role` sea
// player o admin y que haya `username`. El error nunca incluye el token.
func (v *Verifier) Verify(token string) (Identity, error) {
	var c tokenClaims
	if _, err := v.parser.ParseWithClaims(token, &c, func(*jwt.Token) (any, error) { return v.key, nil }); err != nil {
		return Identity{}, fmt.Errorf("%w: %s", ErrInvalidToken, reason(err))
	}
	switch {
	case !isUUID(c.PlayerID):
		return Identity{}, fmt.Errorf("%w: player_id is not a UUID", ErrInvalidToken)
	case c.Subject != c.PlayerID:
		return Identity{}, fmt.Errorf("%w: sub differs from player_id", ErrInvalidToken)
	case strings.TrimSpace(c.Username) == "":
		return Identity{}, fmt.Errorf("%w: username is missing", ErrInvalidToken)
	}
	role := Role(c.Role)
	if role != RolePlayer && role != RoleAdmin {
		return Identity{}, fmt.Errorf("%w: unknown role", ErrInvalidToken)
	}
	return Identity{PlayerID: c.PlayerID, Username: c.Username, Role: role}, nil
}

// reason resume por qué falló el análisis sin repetir texto del token.
func reason(err error) string {
	switch {
	case errors.Is(err, jwt.ErrTokenExpired):
		return "expired"
	case errors.Is(err, jwt.ErrTokenSignatureInvalid):
		return "bad signature"
	case errors.Is(err, jwt.ErrTokenRequiredClaimMissing):
		return "required claim missing"
	case errors.Is(err, jwt.ErrTokenMalformed):
		return "malformed"
	default:
		return "rejected"
	}
}

// ParsePublicKey lee la clave pública Ed25519 de TOKEN_PUBLIC_KEY: un PEM
// SPKI («-----BEGIN PUBLIC KEY-----»), con los saltos de línea escritos como
// `\n` si hace falta, o el base64 del PEM completo (la misma tolerancia que
// la API con la privada). El error no repite el valor.
func ParsePublicKey(value string) (ed25519.PublicKey, error) {
	text := strings.TrimSpace(value)
	if text == "" {
		return nil, errors.New("empty value")
	}
	text = strings.ReplaceAll(text, `\n`, "\n")
	if !strings.Contains(text, "-----BEGIN") {
		raw, err := base64.StdEncoding.DecodeString(text)
		if err != nil {
			return nil, errors.New("not a PEM and not the base64 of a PEM")
		}
		text = string(raw)
	}
	block, _ := pem.Decode([]byte(text))
	if block == nil || block.Type != "PUBLIC KEY" {
		return nil, errors.New("expected a PEM block of type PUBLIC KEY (SPKI)")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, errors.New("the PEM is not a valid SPKI public key")
	}
	key, ok := parsed.(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("the public key is not Ed25519")
	}
	return key, nil
}

// isUUID: forma canónica 8-4-4-4-12 en hexadecimal.
func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i := range len(s) {
		c := s[i]
		switch {
		case i == 8 || i == 13 || i == 18 || i == 23:
			if c != '-' {
				return false
			}
		case !('0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F'):
			return false
		}
	}
	return true
}

// Package requestid lleva el identificador de petición de punta a punta
// (RNF-07). Es el espejo de src/world/src/request_id.rs.
//
// Entrante: el metadato gRPC `x-request-id` (la API lo envía en GetRanking; el
// hub, como `tick-<n>`, en cada Step) se lee, se valida y se registra como
// campo `request_id`. Saliente: cada llamada del simulador a la persistencia
// genera su propio id (`world-<uuid>`) y lo envía como el mismo metadato, para
// correlacionar los dos registros.
package requestid

import (
	"context"

	"google.golang.org/grpc/metadata"

	"github.com/iadev2005/hydrovalley/src/world-go/internal/uuid"
)

// Key es el nombre del metadato gRPC (en minúsculas, como exige HTTP/2).
const Key = "x-request-id"

// None es lo que se registra cuando la llamada no trae un id utilizable.
const None = "-"

const maxLen = 64

// IsSafe dice si un id se puede escribir en un registro: de 1 a 64
// caracteres entre letras, dígitos y guion (UUID, 32 hexadecimales,
// `tick-<n>` o `world-<uuid>`). Cualquier otra cosa (espacios, saltos de
// línea) se descarta.
func IsSafe(id string) bool {
	if len(id) < 1 || len(id) > maxLen {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		alnum := c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
		if !alnum && c != '-' {
			return false
		}
	}
	return true
}

// FromContext devuelve el id de la llamada entrante, o None si falta o no es
// seguro.
func FromContext(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return None
	}
	values := md.Get(Key)
	if len(values) == 0 || !IsSafe(values[0]) {
		return None
	}
	return values[0]
}

// NewOutgoing crea el id de una llamada saliente del simulador.
func NewOutgoing() string { return "world-" + uuid.New() }

// Tag añade el id a los metadatos salientes de ctx.
func Tag(ctx context.Context, id string) context.Context {
	return metadata.AppendToOutgoingContext(ctx, Key, id)
}

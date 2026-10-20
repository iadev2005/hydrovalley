// Package store es el puerto hacia la persistencia (instantáneas y catálogo)
// y su adaptador gRPC contra PersistenceService y CatalogService, que
// comparten dirección (PERSISTENCE_ADDR) y canal. Es el espejo de
// src/world/src/store.rs.
package store

import (
	"context"
	"fmt"
	"time"

	persistencev1 "github.com/iadev2005/hydrovalley/gen/go/hydrovalley/persistence/v1"
	"google.golang.org/grpc/codes"

	"github.com/iadev2005/hydrovalley/src/world-go/internal/domain"
)

// Store es dónde se guardan y de dónde se cargan las instantáneas y el
// catálogo. El simulador solo conoce esta interfaz.
type Store interface {
	// GetCatalog devuelve las cifras del juego (CatalogService.GetCatalog), ya
	// validadas.
	GetCatalog(ctx context.Context) (domain.Catalog, error)
	// Load devuelve la instantánea de mayor tick, o nil sin error si no hay
	// ninguna (NOT_FOUND).
	Load(ctx context.Context) (*persistencev1.Snapshot, error)
	// Save guarda una instantánea.
	Save(ctx context.Context, snapshot *persistencev1.Snapshot) error
}

// Kind clasifica un error de la persistencia.
type Kind int

const (
	// KindNone es la ausencia de error.
	KindNone Kind = iota
	// KindUnavailable: la persistencia no responde (UNAVAILABLE o
	// DEADLINE_EXCEEDED tras los reintentos).
	KindUnavailable
	// KindAlreadyExists: ya hay otra instantánea distinta para ese tick. Por
	// diseño no debe ocurrir nunca.
	KindAlreadyExists
	// KindFailed: cualquier otro error.
	KindFailed
)

// Error es el error de una llamada a la persistencia.
type Error struct {
	Kind    Kind
	Message string
}

func (e *Error) Error() string {
	switch e.Kind {
	case KindUnavailable:
		return "persistencia no disponible: " + e.Message
	case KindAlreadyExists:
		return "instantánea en conflicto: " + e.Message
	default:
		return "error de la persistencia: " + e.Message
	}
}

// KindOf clasifica err: KindNone si es nil y KindFailed si no es un *Error.
func KindOf(err error) Kind {
	if err == nil {
		return KindNone
	}
	if typed, ok := err.(*Error); ok {
		return typed.Kind
	}
	return KindFailed
}

func newError(code codes.Code, message string) *Error {
	text := fmt.Sprintf("%v: %s", code, message)
	switch code {
	case codes.Unavailable, codes.DeadlineExceeded:
		return &Error{Kind: KindUnavailable, Message: text}
	case codes.AlreadyExists:
		return &Error{Kind: KindAlreadyExists, Message: text}
	default:
		return &Error{Kind: KindFailed, Message: text}
	}
}

// CallPolicy es el plazo y los reintentos de cada llamada (regla 4 de
// portabilidad): 2 s y, al ser idempotentes, como mucho 3 reintentos tras 100,
// 200 y 400 ms.
type CallPolicy struct {
	// Deadline es el plazo de cada intento.
	Deadline time.Duration
	// RetryDelays son las esperas antes de cada reintento; su número es el
	// máximo de reintentos.
	RetryDelays []time.Duration
}

// DefaultCallPolicy es la política de CLAUDE.md.
func DefaultCallPolicy() CallPolicy {
	return CallPolicy{
		Deadline:    2 * time.Second,
		RetryDelays: []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond},
	}
}

// retryable dice si un código merece otro intento de una llamada idempotente.
func retryable(code codes.Code) bool {
	return code == codes.Unavailable || code == codes.DeadlineExceeded
}

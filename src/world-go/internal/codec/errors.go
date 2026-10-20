package codec

import "fmt"

// MalformedError: world_state no es un WorldSnapshot válido
// (DecodeError::Malformed de Rust).
type MalformedError struct{ Err error }

func (e *MalformedError) Error() string { return fmt.Sprintf("world_state mal formado: %v", e.Err) }

// Unwrap devuelve el error de protobuf.
func (e *MalformedError) Unwrap() error { return e.Err }

// TickMismatchError: Snapshot.tick y WorldSnapshot.tick no coinciden
// (DecodeError::TickMismatch).
type TickMismatchError struct{ Row, World uint64 }

func (e *TickMismatchError) Error() string {
	return fmt.Sprintf("la fila es del tick %d y el mundo del tick %d", e.Row, e.World)
}

// UnknownEnumError: un campo enum trae un valor que el simulador no conoce o
// sin especificar (DecodeError::UnknownEnum).
type UnknownEnumError struct {
	Field string
	Value int32
}

func (e *UnknownEnumError) Error() string {
	return fmt.Sprintf("valor desconocido en %s: %d", e.Field, e.Value)
}

// RestoreFailure: el dominio no pudo restaurar el mundo (DecodeError::Restore);
// Err es el error del dominio (DimensionsError, TileCountError...).
type RestoreFailure struct{ Err error }

func (e *RestoreFailure) Error() string { return e.Err.Error() }

// Unwrap devuelve el error del dominio.
func (e *RestoreFailure) Unwrap() error { return e.Err }

// InvalidCatalogError: el catálogo de la persistencia no sirve para jugar
// (DecodeError::InvalidCatalog).
type InvalidCatalogError struct{ Reason string }

func (e *InvalidCatalogError) Error() string { return "catálogo no válido: " + e.Reason }

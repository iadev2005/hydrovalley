// Package breaker implementa el Circuit Breaker que vigila WorldService.Step
// (regla 2 de CLAUDE.md, CP-05): se abre tras N fallos seguidos, deja de
// llamar y prueba una vez cada intervalo (semiabierto); un éxito lo cierra.
//
// No lee el reloj: quien lo usa le pasa el instante, así las pruebas son
// deterministas. No es seguro para uso concurrente.
package breaker

import "time"

// State es el estado del circuito.
type State int

const (
	// Closed: se llama a Step en cada tick.
	Closed State = iota
	// Open: no se llama a Step hasta que pase el intervalo de prueba.
	Open
	// HalfOpen: hay una llamada de prueba en curso.
	HalfOpen
)

func (s State) String() string {
	switch s {
	case Closed:
		return "closed"
	case Open:
		return "open"
	case HalfOpen:
		return "half_open"
	default:
		return "unknown"
	}
}

// Breaker cuenta los fallos seguidos y decide si se puede llamar.
type Breaker struct {
	threshold     int
	probeInterval time.Duration

	state    State
	failures int
	openedAt time.Time
}

// New crea un circuito cerrado que se abre tras `threshold` fallos seguidos
// y prueba cada `probeInterval` mientras está abierto.
func New(threshold int, probeInterval time.Duration) *Breaker {
	return &Breaker{threshold: threshold, probeInterval: probeInterval}
}

// State devuelve el estado actual.
func (b *Breaker) State() State { return b.state }

// Allow dice si se puede llamar en `now`. Con el circuito abierto, pasado el
// intervalo, admite una sola llamada de prueba y pasa a semiabierto.
func (b *Breaker) Allow(now time.Time) bool {
	switch b.state {
	case Closed:
		return true
	case Open:
		if now.Sub(b.openedAt) >= b.probeInterval {
			b.state = HalfOpen
			return true
		}
		return false
	default: // HalfOpen: ya hay una prueba en curso.
		return false
	}
}

// Success registra una llamada correcta: cierra el circuito y pone a cero
// los fallos seguidos.
func (b *Breaker) Success() {
	b.state = Closed
	b.failures = 0
}

// Failure registra una llamada fallida en `now`. Devuelve true si esta
// llamada abrió el circuito (o lo reabrió tras una prueba fallida).
func (b *Breaker) Failure(now time.Time) bool {
	b.failures++
	if b.state == HalfOpen || (b.state == Closed && b.failures >= b.threshold) {
		b.state = Open
		b.openedAt = now
		return true
	}
	return false
}

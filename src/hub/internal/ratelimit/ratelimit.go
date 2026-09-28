// Package ratelimit limita las gotas por jugador (RF-05, CP-03).
//
// Se usa una ventana deslizante con registro de instantes en lugar de un
// cubo de fichas: garantiza que nunca hay más de `limit` gotas admitidas en
// cualquier intervalo de `window`. Un cubo de fichas con capacidad 15 y
// recarga de 15/s dejaría pasar hasta 30 en el primer segundo. El coste es
// guardar `limit` instantes por jugador (15), que es despreciable.
//
// No es seguro para uso concurrente: lo protege quien lo contiene.
package ratelimit

import "time"

// SlidingWindow admite como mucho `limit` eventos por cada `window`.
type SlidingWindow struct {
	window time.Duration
	// Anillo con los instantes admitidos; next apunta al más antiguo cuando
	// el anillo está lleno.
	times []time.Time
	next  int
	count int
}

// NewSlidingWindow crea una ventana vacía. limit debe ser mayor que 0.
func NewSlidingWindow(limit int, window time.Duration) *SlidingWindow {
	return &SlidingWindow{window: window, times: make([]time.Time, limit)}
}

// Allow registra un intento en `now` y dice si se admite. Los intentos
// rechazados no ocupan hueco en la ventana.
func (w *SlidingWindow) Allow(now time.Time) bool {
	if w.count < len(w.times) {
		w.times[(w.next+w.count)%len(w.times)] = now
		w.count++
		return true
	}
	oldest := w.times[w.next]
	if now.Sub(oldest) < w.window {
		return false
	}
	w.times[w.next] = now
	w.next = (w.next + 1) % len(w.times)
	return true
}

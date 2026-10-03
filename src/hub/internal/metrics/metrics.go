// Package metrics reúne los contadores en memoria del hub: una tasa por
// segundo en ventana móvil y una muestra acotada de duraciones con
// percentiles. No conoce el hub ni el reloj: quien las usa pasa la hora, así
// que se prueban sin esperar.
//
// Medir no debe frenar el tick: Add es O(1) con una sección crítica corta; el
// ordenado para los percentiles se hace al consultar (Summary), fuera del
// candado.
package metrics

import (
	"math"
	"slices"
	"sync"
	"time"
)

// Rate cuenta eventos por segundo promediados en una ventana móvil de N
// segundos, con un anillo de N huecos de 1 s.
type Rate struct {
	mu      sync.Mutex
	window  int64
	buckets []bucket
}

type bucket struct {
	sec int64 // segundo (Unix) al que pertenece el hueco
	n   uint64
}

// NewRate crea una tasa con ventana de windowSeconds (al menos 1).
func NewRate(windowSeconds int) *Rate {
	w := max(windowSeconds, 1)
	return &Rate{window: int64(w), buckets: make([]bucket, w)}
}

// Add registra un evento ocurrido en now.
func (r *Rate) Add(now time.Time) {
	sec := now.Unix()
	r.mu.Lock()
	defer r.mu.Unlock()
	b := &r.buckets[sec%r.window]
	if b.sec != sec {
		*b = bucket{sec: sec}
	}
	b.n++
}

// PerSecond devuelve los eventos de los últimos N segundos dividido entre N.
// Al arrancar, mientras la ventana no se ha llenado, subestima la tasa real.
func (r *Rate) PerSecond(now time.Time) float64 {
	sec := now.Unix()
	r.mu.Lock()
	defer r.mu.Unlock()
	var total uint64
	for _, b := range r.buckets {
		if b.n > 0 && b.sec > sec-r.window && b.sec <= sec {
			total += b.n
		}
	}
	return float64(total) / float64(r.window)
}

// Summary resume una muestra de duraciones.
type Summary struct {
	// Last es la última duración registrada (aunque ya esté fuera de la
	// ventana); cero si no hubo ninguna.
	Last     time.Duration
	P50, P95 time.Duration
	// Count es el número de muestras que entran en el resumen.
	Count int
}

// Samples guarda como mucho las últimas capacity duraciones en un anillo, de
// modo que la memoria queda acotada. Con window > 0, el resumen solo cuenta
// las de esa ventana.
type Samples struct {
	mu     sync.Mutex
	window time.Duration
	buf    []sample
	next   int
	size   int
	last   time.Duration
}

type sample struct {
	at time.Time
	d  time.Duration
}

// NewSamples crea la muestra. window = 0 significa sin ventana de tiempo:
// solo cuenta el límite de capacity.
func NewSamples(capacity int, window time.Duration) *Samples {
	return &Samples{window: window, buf: make([]sample, max(capacity, 1))}
}

// Add registra una duración medida en now.
func (s *Samples) Add(now time.Time, d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buf[s.next] = sample{at: now, d: d}
	s.next = (s.next + 1) % len(s.buf)
	s.size = min(s.size+1, len(s.buf))
	s.last = d
}

// Summary calcula la última duración, la mediana y el percentil 95 (rango
// más cercano) de las muestras vigentes en now. Copia bajo el candado y
// ordena fuera de él.
func (s *Samples) Summary(now time.Time) Summary {
	s.mu.Lock()
	values := make([]time.Duration, 0, s.size)
	for i := 0; i < s.size; i++ {
		smp := s.buf[i]
		if s.window > 0 && now.Sub(smp.at) >= s.window {
			continue
		}
		values = append(values, smp.d)
	}
	last := s.last
	s.mu.Unlock()
	if len(values) == 0 {
		return Summary{Last: last}
	}
	slices.Sort(values)
	return Summary{Last: last, P50: nearestRank(values, 0.50), P95: nearestRank(values, 0.95), Count: len(values)}
}

// nearestRank devuelve el percentil p (0 < p <= 1) de valores ya ordenados.
func nearestRank(sorted []time.Duration, p float64) time.Duration {
	idx := int(math.Ceil(p*float64(len(sorted)))) - 1
	return sorted[min(max(idx, 0), len(sorted)-1)]
}

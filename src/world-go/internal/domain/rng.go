package domain

import (
	"encoding/binary"
	"math/bits"
)

// Generador de números pseudoaleatorios con semilla (regla 6 de CLAUDE.md):
// SplitMix64. Port de rng.rs.
//
// Se eligió SplitMix64 y no una biblioteca porque el algoritmo cabe en tres
// líneas, su estado es un solo uint64 que viaja en la instantánea
// (WorldSnapshot.rng_state, 8 bytes) y la secuencia depende solo de la
// semilla y de la aritmética entera: es la misma en Rust y en Go (esa
// igualdad es lo que comprueba la prueba de conformidad de T7). No es
// criptográfico; aquí solo decide dónde arden los incendios. No se usa
// math/rand: su secuencia no es la de Rust. Los vectores de las pruebas son
// los de referencia de Vigna (semilla 0).

// RngStateLen es la longitud en bytes del estado serializado.
const RngStateLen = 8

// ppmScale son las partes por millón que valen el 100 %.
const ppmScale uint64 = 1_000_000

const splitMixGamma uint64 = 0x9E3779B97F4A7C15

// SplitMix64 es el generador. El valor cero es el generador de semilla 0.
type SplitMix64 struct {
	state uint64
}

// NewSplitMix64 crea el generador con la semilla dada.
func NewSplitMix64(seed uint64) SplitMix64 {
	return SplitMix64{state: seed}
}

// NextU64 es el siguiente número (rng.rs:32).
func (r *SplitMix64) NextU64() uint64 {
	r.state += splitMixGamma // envuelve, como wrapping_add
	z := r.state
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

// ChancePPM es un sorteo con probabilidad ppm partes por millón (0 nunca, un
// millón o más siempre) (rng.rs:46). Consume exactamente un número, también
// cuando la probabilidad es 0 o 100 %, para que la secuencia no dependa del
// resultado.
func (r *SplitMix64) ChancePPM(ppm uint32) bool {
	// Multiplicación de alta parte: reparte el uint64 en [0, 1.000.000) sin
	// división ni sesgo apreciable (menos de 1e-13). Es (n * 1e6) >> 64.
	scaled, _ := bits.Mul64(r.NextU64(), ppmScale)
	return scaled < uint64(ppm)
}

// Bytes es el estado serializado: el uint64 en little-endian (rng.rs:58).
func (r SplitMix64) Bytes() []byte {
	out := make([]byte, RngStateLen)
	binary.LittleEndian.PutUint64(out, r.state)
	return out
}

// SplitMix64FromBytes restaura el generador; ok es false si bytes no mide
// exactamente RngStateLen (rng.rs:63).
func SplitMix64FromBytes(bytes []byte) (rng SplitMix64, ok bool) {
	if len(bytes) != RngStateLen {
		return SplitMix64{}, false
	}
	return SplitMix64{state: binary.LittleEndian.Uint64(bytes)}, true
}

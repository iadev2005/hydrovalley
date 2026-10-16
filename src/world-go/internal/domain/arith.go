package domain

import "math/bits"

// Aritmética de enteros sin signo con los mismos límites que Rust
// (saturating_*, is_multiple_of). Regla 7: agua, hojas y humedad son u64 y
// nunca desbordan en silencio.

// satAdd es u64::saturating_add.
func satAdd(a, b uint64) uint64 {
	sum, carry := bits.Add64(a, b, 0)
	if carry != 0 {
		return ^uint64(0)
	}
	return sum
}

// satSub es u64::saturating_sub.
func satSub(a, b uint64) uint64 {
	if a < b {
		return 0
	}
	return a - b
}

// satMul es u64::saturating_mul.
func satMul(a, b uint64) uint64 {
	hi, lo := bits.Mul64(a, b)
	if hi != 0 {
		return ^uint64(0)
	}
	return lo
}

// satAdd32 es u32::saturating_add.
func satAdd32(a, b uint32) uint32 {
	sum := a + b
	if sum < a {
		return ^uint32(0)
	}
	return sum
}

// satSub32 es u32::saturating_sub.
func satSub32(a, b uint32) uint32 {
	if a < b {
		return 0
	}
	return a - b
}

// isMultiple es u64::is_multiple_of. Con divisor 0 solo el 0 es múltiplo
// (Rust no entra en pánico); un `%` de Go sí lo haría.
func isMultiple(value, divisor uint64) bool {
	if divisor == 0 {
		return value == 0
	}
	return value%divisor == 0
}

// minU64 es u64::min.
func minU64(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}

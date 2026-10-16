package domain

import (
	"math"
	"testing"
)

func TestSaturatingArithmeticMatchesRust(t *testing.T) {
	const max = math.MaxUint64
	if satAdd(max, 1) != max || satAdd(1, 2) != 3 {
		t.Error("satAdd")
	}
	if satSub(1, 2) != 0 || satSub(5, 2) != 3 {
		t.Error("satSub")
	}
	if satMul(max, 2) != max || satMul(3, 4) != 12 {
		t.Error("satMul")
	}
	if satAdd32(math.MaxUint32, 1) != math.MaxUint32 || satSub32(1, 2) != 0 {
		t.Error("satAdd32/satSub32")
	}
}

// u64::is_multiple_of(0) es true solo para el 0 y no entra en pánico.
func TestIsMultipleOfZeroDivisor(t *testing.T) {
	if !isMultiple(0, 0) || isMultiple(5, 0) {
		t.Error("zero divisor")
	}
	if !isMultiple(20, 20) || isMultiple(21, 20) || !isMultiple(0, 20) {
		t.Error("regular divisor")
	}
}

package domain

import (
	"slices"
	"testing"
)

// Portadas de las pruebas de rng.rs. Los vectores son los de referencia de
// Vigna (semilla 0).

func take(seed uint64, n int) []uint64 {
	rng := NewSplitMix64(seed)
	out := make([]uint64, n)
	for i := range out {
		out[i] = rng.NextU64()
	}
	return out
}

func TestItMatchesTheReferenceVectorsForSeedZero(t *testing.T) {
	rng := NewSplitMix64(0)
	for i, want := range []uint64{0xE220A8397B1DCDAF, 0x6E789E6AA1B965F4, 0x06C45D188009454F} {
		if got := rng.NextU64(); got != want {
			t.Errorf("number %d = %#x, want %#x", i, got, want)
		}
	}
}

func TestTheSameSeedGivesTheSameSequenceAndAnotherSeedADifferentOne(t *testing.T) {
	if !slices.Equal(take(42, 8), take(42, 8)) {
		t.Error("the same seed gave different sequences")
	}
	if slices.Equal(take(42, 8), take(43, 8)) {
		t.Error("different seeds gave the same sequence")
	}
}

func TestTheStateRoundTripsAndContinuesTheSequence(t *testing.T) {
	rng := NewSplitMix64(7)
	rng.NextU64()
	bytes := rng.Bytes()
	if len(bytes) != RngStateLen {
		t.Fatalf("state is %d bytes, want %d", len(bytes), RngStateLen)
	}
	restored, ok := SplitMix64FromBytes(bytes)
	if !ok || restored != rng {
		t.Fatalf("restored = %+v (ok %v), want %+v", restored, ok, rng)
	}
	if got, want := restored.NextU64(), rng.NextU64(); got != want {
		t.Errorf("restored continues with %#x, want %#x", got, want)
	}
}

func TestAStateOfTheWrongLengthIsRefused(t *testing.T) {
	for _, bytes := range [][]byte{nil, {}, {1, 2, 3}, make([]byte, 9)} {
		if _, ok := SplitMix64FromBytes(bytes); ok {
			t.Errorf("a state of %d bytes was accepted", len(bytes))
		}
	}
}

func TestTheChanceIsNeverWithZeroAndAlwaysWithAMillion(t *testing.T) {
	rng := NewSplitMix64(1)
	for range 1000 {
		if rng.ChancePPM(0) {
			t.Fatal("0 ppm hit")
		}
	}
	for range 1000 {
		if !rng.ChancePPM(1_000_000) {
			t.Fatal("1.000.000 ppm missed")
		}
	}
}

func TestEveryChanceConsumesExactlyOneNumber(t *testing.T) {
	a, b := NewSplitMix64(9), NewSplitMix64(9)
	a.ChancePPM(0)
	a.ChancePPM(500_000)
	b.NextU64()
	b.NextU64()
	if a != b {
		t.Errorf("states differ: %+v vs %+v", a, b)
	}
}

func TestTheChanceIsCloseToTheRequestedProbability(t *testing.T) {
	rng := NewSplitMix64(2026)
	hits := 0
	for range 200_000 {
		if rng.ChancePPM(100_000) {
			hits++
		}
	}
	// 10 % de 200.000 = 20.000; margen amplio (~7 desviaciones típicas).
	if hits < 19_000 || hits > 21_000 {
		t.Errorf("hits = %d, want about 20000", hits)
	}
}

// Go-only: el estado viaja en little-endian, byte a byte (rng.rs:58), y la
// probabilidad usa la parte alta del producto de 128 bits (rng.rs:51).
func TestTheStateIsLittleEndianAndTheChanceUsesTheHighPartOfTheProduct(t *testing.T) {
	rng := NewSplitMix64(0x0102030405060708)
	if want := []byte{8, 7, 6, 5, 4, 3, 2, 1}; !slices.Equal(rng.Bytes(), want) {
		t.Errorf("bytes = %v, want %v", rng.Bytes(), want)
	}
	// Con semilla 0 el primer número es 0xE220A8397B1DCDAF: su parte alta
	// por un millón es floor(0xE220A839... / 2^64 * 1e6) = 883.310.
	probe := NewSplitMix64(0)
	if !probe.ChancePPM(884_000) {
		t.Error("the first draw of seed 0 should fall under 884000 ppm")
	}
	probe = NewSplitMix64(0)
	if probe.ChancePPM(883_000) {
		t.Error("the first draw of seed 0 should not fall under 883000 ppm")
	}
}

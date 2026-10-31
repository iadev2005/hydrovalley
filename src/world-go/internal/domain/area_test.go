package domain

import (
	"slices"
	"testing"
)

// areaIndices es el cuadrado centrado y recortado de world.rs:793 (lo usan la
// gota más grande y el aspersor; aquí, la gota normal con lado 1).
func TestAreaIndices(t *testing.T) {
	at := func(x, y uint32) int { return int(y)*32 + int(x) }
	cases := []struct {
		name string
		x, y uint32
		side uint32
		want []int
	}{
		{"side 1 is the tile itself", 5, 7, 1, []int{at(5, 7)}},
		{"side 0 still includes the centre", 5, 7, 0, []int{at(5, 7)}},
		{"side 3 in the middle", 5, 5, 3, []int{
			at(4, 4), at(5, 4), at(6, 4),
			at(4, 5), at(5, 5), at(6, 5),
			at(4, 6), at(5, 6), at(6, 6)}},
		{"side 3 is clipped in the top-left corner", 0, 0, 3, []int{at(0, 0), at(1, 0), at(0, 1), at(1, 1)}},
		{"side 3 is clipped in the bottom-right corner", 31, 31, 3, []int{at(30, 30), at(31, 30), at(30, 31), at(31, 31)}},
		{"an even side leans to the top-left", 5, 5, 4, []int{
			at(4, 4), at(5, 4), at(6, 4), at(7, 4),
			at(4, 5), at(5, 5), at(6, 5), at(7, 5),
			at(4, 6), at(5, 6), at(6, 6), at(7, 6),
			at(4, 7), at(5, 7), at(6, 7), at(7, 7)}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := areaIndices(c.x, c.y, c.side, 32, 32); !slices.Equal(got, c.want) {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestAHugeSideCoversTheWholeValleyWithoutOverflowing(t *testing.T) {
	if got := len(areaIndices(10, 10, ^uint32(0), 32, 32)); got != 32*32 {
		t.Errorf("got %d tiles, want 1024", got)
	}
}

func TestAreaIndicesAreClippedByTheGivenValleySize(t *testing.T) {
	got := areaIndices(63, 47, 3, 64, 48)
	want := []int{46*64 + 62, 46*64 + 63, 47*64 + 62, 47*64 + 63}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

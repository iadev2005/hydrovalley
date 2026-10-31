package domain

import "testing"

// Portada de growth.rs: the_stage_ladder_is_consistent.
func TestTheStageLadderIsConsistent(t *testing.T) {
	stage := DrySoil
	for {
		next, ok := stage.Next()
		if !ok {
			break
		}
		if back, ok := next.Previous(); !ok || back != stage {
			t.Fatalf("Previous(Next(%d)) = %d, %v", stage, back, ok)
		}
		stage = next
	}
	if stage != Tree {
		t.Fatalf("the ladder ends at %d, want Tree", stage)
	}
	if _, ok := DrySoil.Previous(); ok {
		t.Fatal("dry soil has no previous stage")
	}
}

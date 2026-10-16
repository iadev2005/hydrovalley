package domain

import (
	"fmt"
	"reflect"
	"testing"
)

// Portadas de las pruebas de ranking.rs.

func score(id string, leaves uint64) Score {
	return Score{PlayerID: id, LeavesEarned: leaves}
}

type rankRow struct {
	rank   uint32
	id     string
	leaves uint64
}

func view(ranked []RankedPlayer) []rankRow {
	rows := make([]rankRow, len(ranked))
	for i, r := range ranked {
		rows[i] = rankRow{r.Rank, r.PlayerID, r.LeavesEarned}
	}
	return rows
}

func TestEmptyInputGivesAnEmptyRanking(t *testing.T) {
	if got := RankPlayers(nil, 0); len(got) != 0 {
		t.Errorf("ranking = %v, want empty", got)
	}
}

func TestOrdersByLeavesEarnedDescending(t *testing.T) {
	got := view(RankPlayers([]Score{score("a", 1), score("b", 9), score("c", 5)}, 0))
	want := []rankRow{{1, "b", 9}, {2, "c", 5}, {3, "a", 1}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ranking = %v, want %v", got, want)
	}
}

func TestTiesBreakByPlayerIDAscendingAndShareTheRank(t *testing.T) {
	got := view(RankPlayers([]Score{score("d", 3), score("c", 7), score("a", 7), score("b", 7)}, 0))
	want := []rankRow{{1, "a", 7}, {1, "b", 7}, {1, "c", 7}, {4, "d", 3}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ranking = %v, want %v", got, want)
	}
}

func TestCompetitionRankingSkipsAfterATie(t *testing.T) {
	ranked := RankPlayers([]Score{score("a", 9), score("b", 5), score("c", 5), score("d", 1)}, 0)
	var ranks []uint32
	for _, r := range ranked {
		ranks = append(ranks, r.Rank)
	}
	if want := []uint32{1, 2, 2, 4}; !reflect.DeepEqual(ranks, want) {
		t.Errorf("ranks = %v, want %v", ranks, want)
	}
}

func TestPlayersWithZeroLeavesAreRankedToo(t *testing.T) {
	got := view(RankPlayers([]Score{score("b", 0), score("a", 0)}, 0))
	want := []rankRow{{1, "a", 0}, {1, "b", 0}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ranking = %v, want %v", got, want)
	}
}

func TestLimitTruncatesWithoutChangingRanks(t *testing.T) {
	got := view(RankPlayers([]Score{score("a", 5), score("b", 5), score("c", 5), score("d", 1)}, 2))
	want := []rankRow{{1, "a", 5}, {1, "b", 5}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ranking = %v, want %v", got, want)
	}
}

func TestLimitZeroReturnsEveryoneUpToTheCap(t *testing.T) {
	var scores []Score
	for n := range MaxRankingEntries + 5 {
		scores = append(scores, score(fmt.Sprintf("p%05d", n), 1))
	}
	if got := len(RankPlayers(scores, 0)); got != MaxRankingEntries {
		t.Errorf("limit 0 returned %d, want %d", got, MaxRankingEntries)
	}
	if got := len(RankPlayers(scores, ^uint32(0))); got != MaxRankingEntries {
		t.Errorf("limit u32::MAX returned %d, want %d", got, MaxRankingEntries)
	}
	if got := len(RankPlayers(scores, 7)); got != 7 {
		t.Errorf("limit 7 returned %d, want 7", got)
	}
}

func TestLimitLargerThanThePlayersReturnsAllOfThem(t *testing.T) {
	if got := len(RankPlayers([]Score{score("a", 1)}, 50)); got != 1 {
		t.Errorf("returned %d, want 1", got)
	}
}

func TestInputOrderDoesNotMatter(t *testing.T) {
	a := RankPlayers([]Score{score("x", 2), score("y", 2), score("z", 8)}, 0)
	b := RankPlayers([]Score{score("z", 8), score("y", 2), score("x", 2)}, 0)
	if !reflect.DeepEqual(a, b) {
		t.Errorf("rankings differ: %v vs %v", a, b)
	}
}

// Go-only: el ranking no modifica la lista que recibe (Rust la consume).
func TestRankingDoesNotReorderTheCallersSlice(t *testing.T) {
	scores := []Score{score("a", 1), score("b", 9)}
	RankPlayers(scores, 0)
	if scores[0].PlayerID != "a" || scores[1].PlayerID != "b" {
		t.Errorf("scores were reordered: %v", scores)
	}
}

func TestTheWorldScoresAreTheLeavesEarnedOrderedByPlayerID(t *testing.T) {
	w := rich(holder{beto, 7}, holder{ana, 3})
	got := w.Scores()
	want := []Score{{ana, 3}, {beto, 7}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("scores = %v, want %v", got, want)
	}
	// Las hojas ganadas no bajan al gastar: el ranking usa leaves_earned.
	w = rich(holder{ana, 50})
	w.Step([]Command{placeCmd(action(1), ana, Sprinkler, 1, 1)})
	if got := w.Scores(); !reflect.DeepEqual(got, []Score{{ana, 50}}) {
		t.Errorf("scores = %v, want ana with 50 earned", got)
	}
}

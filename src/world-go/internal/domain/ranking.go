package domain

import "sort"

// Ranking de jugadores por hojas ganadas (RF-10). Port de ranking.rs.
//
// Función pura sobre una lista de puntajes: no toca el mundo. El llamador
// copia los puntajes con el bloqueo del mundo tomado y ordena después, con el
// bloqueo ya suelto, para no retrasar Step (RF-11).

// MaxRankingEntries es el máximo de entradas que devuelve un ranking, aunque
// se pida más o se pida todo (limit = 0). Acota el tamaño de la respuesta.
const MaxRankingEntries = 1_000

// Score son las hojas ganadas por un jugador (LeavesEarned, nunca baja al
// gastar).
type Score struct {
	PlayerID     string
	LeavesEarned uint64
}

// RankedPlayer es una posición del ranking.
type RankedPlayer struct {
	Rank         uint32
	PlayerID     string
	LeavesEarned uint64
}

// RankPlayers ordena por LeavesEarned de mayor a menor. El documento inicial
// no define los empates, así que se decide aquí (igual que en Rust):
//
//   - Desempate del orden: PlayerID ascendente (bytes, determinista).
//   - Posición («competition ranking»): los empatados comparten posición y la
//     siguiente salta, p. ej. 1, 2, 2, 4.
//
// limit = 0 devuelve todos; en cualquier caso se devuelven como mucho
// MaxRankingEntries. La posición se calcula sobre el ranking completo, de
// modo que recortar nunca la cambia. No modifica scores (Rust lo consume).
func RankPlayers(scores []Score, limit uint32) []RankedPlayer {
	sorted := append([]Score(nil), scores...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].LeavesEarned != sorted[j].LeavesEarned {
			return sorted[i].LeavesEarned > sorted[j].LeavesEarned
		}
		return sorted[i].PlayerID < sorted[j].PlayerID
	})
	take := MaxRankingEntries
	if limit != 0 {
		take = min(int(limit), MaxRankingEntries)
	}
	take = min(take, len(sorted))
	ranked := make([]RankedPlayer, 0, take)
	for index, score := range sorted[:take] {
		rank := uint32(index + 1)
		if index > 0 && ranked[index-1].LeavesEarned == score.LeavesEarned {
			rank = ranked[index-1].Rank
		}
		ranked = append(ranked, RankedPlayer{Rank: rank, PlayerID: score.PlayerID, LeavesEarned: score.LeavesEarned})
	}
	return ranked
}

// Scores copia las hojas ganadas de cada jugador, ordenados por player_id,
// para armar el ranking fuera del bloqueo del mundo (world.rs:777).
func (w *World) Scores() []Score {
	players := w.Players()
	scores := make([]Score, len(players))
	for i, p := range players {
		scores[i] = Score{PlayerID: p.PlayerID, LeavesEarned: p.LeavesEarned}
	}
	return scores
}

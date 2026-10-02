//! Ranking de jugadores por hojas ganadas (RF-10).
//!
//! Función pura sobre una lista de puntajes: no toca el mundo. El llamador
//! copia los puntajes con el bloqueo del mundo tomado y ordena después, con
//! el bloqueo ya suelto, para no retrasar `Step` (RF-11).

/// Máximo de entradas que devuelve un ranking, aunque se pida más o se pida
/// todo (`limit = 0`). Acota el tamaño de la respuesta.
pub const MAX_RANKING_ENTRIES: usize = 1_000;

/// Hojas ganadas por un jugador (`leaves_earned`, nunca baja al gastar).
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Score {
    pub player_id: String,
    pub leaves_earned: u64,
}

/// Una posición del ranking.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RankedPlayer {
    pub rank: u32,
    pub player_id: String,
    pub leaves_earned: u64,
}

/// Ordena por `leaves_earned` de mayor a menor. El documento inicial no
/// define los empates, así que se decide aquí:
///
/// - Desempate del orden: `player_id` ascendente (determinista).
/// - Posición («competition ranking»): los empatados comparten posición y la
///   siguiente salta, p. ej. 1, 2, 2, 4.
///
/// `limit` = 0 devuelve todos; en cualquier caso se devuelven como mucho
/// [`MAX_RANKING_ENTRIES`]. La posición se calcula sobre el ranking completo,
/// de modo que recortar nunca la cambia.
pub fn rank_players(mut scores: Vec<Score>, limit: u32) -> Vec<RankedPlayer> {
    scores.sort_by(|a, b| {
        b.leaves_earned
            .cmp(&a.leaves_earned)
            .then_with(|| a.player_id.cmp(&b.player_id))
    });
    let take = match usize::try_from(limit) {
        Ok(0) | Err(_) => MAX_RANKING_ENTRIES,
        Ok(n) => n.min(MAX_RANKING_ENTRIES),
    };
    let mut ranked: Vec<RankedPlayer> = Vec::with_capacity(take.min(scores.len()));
    for (index, score) in scores.into_iter().take(take).enumerate() {
        let rank = match ranked.last() {
            Some(prev) if prev.leaves_earned == score.leaves_earned => prev.rank,
            _ => u32::try_from(index + 1).unwrap_or(u32::MAX),
        };
        ranked.push(RankedPlayer {
            rank,
            player_id: score.player_id,
            leaves_earned: score.leaves_earned,
        });
    }
    ranked
}

#[cfg(test)]
mod tests {
    use super::*;

    fn score(id: &str, leaves: u64) -> Score {
        Score {
            player_id: id.to_string(),
            leaves_earned: leaves,
        }
    }

    fn view(ranked: &[RankedPlayer]) -> Vec<(u32, &str, u64)> {
        ranked
            .iter()
            .map(|r| (r.rank, r.player_id.as_str(), r.leaves_earned))
            .collect()
    }

    #[test]
    fn empty_input_gives_an_empty_ranking() {
        assert!(rank_players(Vec::new(), 0).is_empty());
    }

    #[test]
    fn orders_by_leaves_earned_descending() {
        let ranked = rank_players(vec![score("a", 1), score("b", 9), score("c", 5)], 0);
        assert_eq!(view(&ranked), vec![(1, "b", 9), (2, "c", 5), (3, "a", 1)]);
    }

    #[test]
    fn ties_break_by_player_id_ascending_and_share_the_rank() {
        let ranked = rank_players(
            vec![score("d", 3), score("c", 7), score("a", 7), score("b", 7)],
            0,
        );
        assert_eq!(
            view(&ranked),
            vec![(1, "a", 7), (1, "b", 7), (1, "c", 7), (4, "d", 3)]
        );
    }

    #[test]
    fn competition_ranking_skips_after_a_tie() {
        let ranked = rank_players(
            vec![score("a", 9), score("b", 5), score("c", 5), score("d", 1)],
            0,
        );
        let ranks: Vec<u32> = ranked.iter().map(|r| r.rank).collect();
        assert_eq!(ranks, vec![1, 2, 2, 4]);
    }

    #[test]
    fn players_with_zero_leaves_are_ranked_too() {
        let ranked = rank_players(vec![score("b", 0), score("a", 0)], 0);
        assert_eq!(view(&ranked), vec![(1, "a", 0), (1, "b", 0)]);
    }

    #[test]
    fn limit_truncates_without_changing_ranks() {
        let ranked = rank_players(
            vec![score("a", 5), score("b", 5), score("c", 5), score("d", 1)],
            2,
        );
        assert_eq!(view(&ranked), vec![(1, "a", 5), (1, "b", 5)]);
    }

    #[test]
    fn limit_zero_returns_everyone_up_to_the_cap() {
        let scores: Vec<Score> = (0..MAX_RANKING_ENTRIES + 5)
            .map(|n| score(&format!("p{n:05}"), 1))
            .collect();
        assert_eq!(rank_players(scores.clone(), 0).len(), MAX_RANKING_ENTRIES);
        assert_eq!(
            rank_players(scores.clone(), u32::MAX).len(),
            MAX_RANKING_ENTRIES
        );
        assert_eq!(rank_players(scores, 7).len(), 7);
    }

    #[test]
    fn limit_larger_than_the_players_returns_all_of_them() {
        assert_eq!(rank_players(vec![score("a", 1)], 50).len(), 1);
    }

    #[test]
    fn input_order_does_not_matter() {
        let a = rank_players(vec![score("x", 2), score("y", 2), score("z", 8)], 0);
        let b = rank_players(vec![score("z", 8), score("y", 2), score("x", 2)], 0);
        assert_eq!(a, b);
    }
}

// Puerto hacia el ranking del simulador (RF-10, RF-11). Las rutas dependen de
// esta interfaz, no de gRPC.

export interface RankingEntry {
  /** Posición con empates: 1, 2, 2, 4. */
  rank: number;
  /** UUID en texto. */
  playerId: string;
  /** Entero de 64 bits sin signo. */
  leavesEarned: bigint;
}

export interface Ranking {
  /** Tick en el que se calculó. */
  tick: bigint;
  /** De mayor a menor por hojas ganadas. */
  entries: readonly RankingEntry[];
}

export interface RankingSource {
  /** Las primeras `limit` entradas (WorldService.GetRanking; idempotente). */
  getRanking(limit: number): Promise<Ranking>;
}

// Puerto hacia el reporte de las 3R (RF-12). Las rutas dependen de esta
// interfaz, no de gRPC.

export interface ReportQuery {
  /** Sin valor: reporte global. */
  playerId?: string;
  /** Sin valor: inicio de la época actual. */
  fromTick?: bigint;
  /** Sin valor: la última instantánea. */
  toTick?: bigint;
}

/** Enteros de 64 bits sin signo: las 3R y los saldos de RF-27. */
export interface ReportTotals {
  waterReduced: bigint;
  waterReused: bigint;
  waterRecycled: bigint;
  waterRefilled: bigint;
  waterSpent: bigint;
  leavesEarned: bigint;
  leavesSpent: bigint;
}

export interface Report {
  /** Vacío en el reporte global. */
  playerId: string;
  epoch: number;
  /** Ticks de las instantáneas realmente usadas. */
  fromTick: bigint;
  toTick: bigint;
  totals: ReportTotals;
}

export interface ReportSource {
  /** ReportService.GetReport (idempotente). */
  getReport(query: ReportQuery): Promise<Report>;
}

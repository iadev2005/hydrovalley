// Ranking (RF-10, RF-11) y reporte de las 3R (RF-12): peticiones autenticadas,
// vistas para mostrar y mensajes de error. Funciones sin DOM: `fetch` se
// inyecta para poder probarlas con vitest.

import type { FetchFn } from "./auth.ts";

/** Cada cuánto se refresca mientras hay sesión (ms). */
export const REFRESH_MS = 10_000;
/** Filas del ranking que se muestran. */
export const RANKING_ROWS = 10;

const DEFAULT_RETRY_MS = 10_000;
const MAX_RETRY_MS = 600_000;
const U64_PATTERN = /^\d+$/;

/**
 * Entero sin signo (texto, como lo envía la API) con puntos de millar, sin
 * pasar por `Number`: los u64 no caben en un decimal de doble precisión.
 */
export function formatU64(value: unknown): string {
  if (typeof value !== "string" || !U64_PATTERN.test(value)) return "—";
  return BigInt(value).toLocaleString("en-US").replace(/,/g, ".");
}

export interface RequestSpec {
  url: string;
  init: RequestInit;
}

function authedGet(url: string, token: string): RequestSpec {
  return { url, init: { method: "GET", headers: { Authorization: `Bearer ${token}` } } };
}

/**
 * @param token
 * @param limit
 */
export function buildRankingRequest(token: string, limit = RANKING_ROWS): RequestSpec {
  return authedGet(`/api/ranking?limit=${encodeURIComponent(String(limit))}`, token);
}

export type ReportScope = "me" | "global";

export interface ReportPeriod {
  fromTick?: string;
  toTick?: string;
}

/**
 * @param token
 * @param scope
 * @param period ticks como texto (u64)
 */
export function buildReportRequest(
  token: string,
  scope: string,
  { fromTick, toTick }: ReportPeriod = {},
): RequestSpec {
  if (scope !== "me" && scope !== "global") throw new Error(`alcance no válido: ${scope}`);
  let url = `/api/report?scope=${scope}`;
  for (const [name, value] of [
    ["from_tick", fromTick],
    ["to_tick", toTick],
  ] as const) {
    if (value === undefined) continue;
    if (!U64_PATTERN.test(value)) throw new Error(`${name} no es un entero sin signo`);
    url += `&${name}=${value}`;
  }
  return authedGet(url, token);
}

export interface RankingRowView {
  rank: number;
  username: string;
  leaves: string;
  isMe: boolean;
}

export interface RankingView {
  tick: string;
  rows: RankingRowView[];
}

/**
 * Filas del ranking listas para dibujar; resalta al jugador actual.
 */
export function toRankingView(body: unknown, currentUsername?: string): RankingView {
  const me = String(currentUsername ?? "").toLowerCase();
  const rec = (body && typeof body === "object" ? body : {}) as Record<string, unknown>;
  const entries = Array.isArray(rec["entries"]) ? (rec["entries"] as Record<string, unknown>[]) : [];
  return {
    tick: formatU64(rec["tick"]),
    rows: entries.slice(0, RANKING_ROWS).map((e) => {
      const username = String(e["username"] ?? "");
      return {
        rank: typeof e["rank"] === "number" ? e["rank"] : 0,
        username,
        leaves: formatU64(e["leaves_earned"]),
        isMe: me !== "" && username.toLowerCase() === me,
      };
    }),
  };
}

const POINTS = "pts de humedad";
// Reducida y reutilizada se cuentan en puntos de humedad; reciclada y los
// saldos del depósito, en litros. La equivalencia entre ambas unidades sigue
// pendiente en el documento inicial, así que no se convierten ni se suman.
const REPORT_ROWS: [string, string, string][] = [
  ["water_reduced", "Agua reducida", POINTS],
  ["water_reused", "Agua reutilizada", POINTS],
  ["water_recycled", "Agua reciclada", "L"],
  ["water_refilled", "Agua recargada", "L"],
  ["water_spent", "Agua gastada", "L"],
  ["leaves_earned", "Hojas ganadas", ""],
  ["leaves_spent", "Hojas gastadas", ""],
];

const EQUIVALENCE_NOTE =
  "Reducida y reutilizada se miden en puntos de humedad y reciclada en litros: la equivalencia entre litros y humedad está pendiente, por eso no se suman.";

export interface ReportRowView {
  key: string;
  label: string;
  value: string;
}

export interface ReportView {
  scope: ReportScope;
  period: string;
  rows: ReportRowView[];
  note: string;
}

export function toReportView(body: unknown): ReportView {
  const rec = (body && typeof body === "object" ? body : {}) as Record<string, unknown>;
  const totals = (rec["totals"] && typeof rec["totals"] === "object"
    ? rec["totals"]
    : {}) as Record<string, unknown>;
  return {
    scope: rec["scope"] === "global" ? "global" : "me",
    period: `Época ${rec["epoch"] !== undefined ? String(rec["epoch"]) : "—"} · ticks ${formatU64(rec["from_tick"])} a ${formatU64(rec["to_tick"])}`,
    rows: REPORT_ROWS.map(([key, label, unit]) => {
      const value = formatU64(totals[key]);
      return { key, label, value: unit && value !== "—" ? `${value} ${unit}` : value };
    }),
    note: EQUIVALENCE_NOTE,
  };
}

export type ReadErrorKind = "session" | "wait" | "unavailable" | "empty" | "error";

export interface ReadErrorDesc {
  kind: ReadErrorKind;
  message: string;
  waitMs: number;
}

/**
 * Mensaje para el usuario según el estado HTTP (0: sin respuesta).
 * `kind`: session (volver a entrar), wait (esperar `waitMs`), unavailable,
 * empty (sin datos todavía) o error.
 */
export function describeReadError(status: number, retryAfterSeconds?: number): ReadErrorDesc {
  switch (status) {
    case 401:
      return { kind: "session", message: "la sesión caducó, vuelve a entrar", waitMs: 0 };
    case 429: {
      if (Number.isInteger(retryAfterSeconds) && retryAfterSeconds !== undefined && retryAfterSeconds >= 0) {
        return {
          kind: "wait",
          message: `demasiadas consultas, espera ${retryAfterSeconds} s`,
          waitMs: Math.min(retryAfterSeconds * 1000, MAX_RETRY_MS),
        };
      }
      return { kind: "wait", message: "demasiadas consultas, espera unos segundos", waitMs: DEFAULT_RETRY_MS };
    }
    case 503:
    case 0:
      return { kind: "unavailable", message: "no disponible", waitMs: 0 };
    case 404:
      return { kind: "empty", message: "todavía no hay datos de este periodo", waitMs: 0 };
    default:
      return { kind: "error", message: `error inesperado (${status})`, waitMs: 0 };
  }
}

export type LoadResult<T> =
  | { ok: true; view: T }
  | { ok: false; error: ReadErrorDesc };

async function load<T>(
  fetchFn: FetchFn,
  { url, init }: RequestSpec,
  toView: (body: unknown) => T,
): Promise<LoadResult<T>> {
  let response: Awaited<ReturnType<FetchFn>>;
  try {
    response = await fetchFn(url, init);
  } catch {
    return { ok: false, error: describeReadError(0) };
  }
  if (!response.ok) {
    const seconds = Number.parseInt(response.headers.get("retry-after") ?? "", 10);
    return {
      ok: false,
      error: describeReadError(response.status, Number.isInteger(seconds) ? seconds : undefined),
    };
  }
  let body: unknown;
  try {
    body = await response.json();
  } catch {
    return { ok: false, error: describeReadError(response.status) };
  }
  return { ok: true, view: toView(body) };
}

/**
 * @param fetchFn
 * @param token
 * @param username jugador actual, para resaltarlo
 */
export function loadRanking(
  fetchFn: FetchFn,
  token: string,
  username?: string,
): Promise<LoadResult<RankingView>> {
  return load(fetchFn, buildRankingRequest(token), (body) => toRankingView(body, username));
}

/**
 * @param fetchFn
 * @param token
 * @param scope
 */
export function loadReport(
  fetchFn: FetchFn,
  token: string,
  scope: ReportScope,
): Promise<LoadResult<ReportView>> {
  return load(fetchFn, buildReportRequest(token, scope), toReportView);
}

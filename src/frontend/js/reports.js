// Ranking (RF-10, RF-11) y reporte de las 3R (RF-12): peticiones autenticadas,
// vistas para mostrar y mensajes de error. Funciones sin DOM: `fetch` se
// inyecta para poder probarlas con node:test.

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
 * @param {unknown} value
 */
export function formatU64(value) {
  if (typeof value !== "string" || !U64_PATTERN.test(value)) return "—";
  return BigInt(value).toLocaleString("en-US").replace(/,/g, ".");
}

function authedGet(url, token) {
  return { url, init: { method: "GET", headers: { Authorization: `Bearer ${token}` } } };
}

/**
 * @param {string} token
 * @param {number} [limit]
 */
export function buildRankingRequest(token, limit = RANKING_ROWS) {
  return authedGet(`/api/ranking?limit=${encodeURIComponent(String(limit))}`, token);
}

/**
 * @param {string} token
 * @param {"me" | "global"} scope
 * @param {{fromTick?: string, toTick?: string}} [period] ticks como texto (u64)
 */
export function buildReportRequest(token, scope, { fromTick, toTick } = {}) {
  if (scope !== "me" && scope !== "global") throw new Error(`alcance no válido: ${scope}`);
  let url = `/api/report?scope=${scope}`;
  for (const [name, value] of [["from_tick", fromTick], ["to_tick", toTick]]) {
    if (value === undefined) continue;
    if (!U64_PATTERN.test(value)) throw new Error(`${name} no es un entero sin signo`);
    url += `&${name}=${value}`;
  }
  return authedGet(url, token);
}

/**
 * Filas del ranking listas para dibujar; resalta al jugador actual.
 * @param {any} body respuesta de `GET /api/ranking`
 * @param {string} currentUsername
 */
export function toRankingView(body, currentUsername) {
  const me = String(currentUsername ?? "").toLowerCase();
  const entries = Array.isArray(body?.entries) ? body.entries : [];
  return {
    tick: formatU64(body?.tick),
    rows: entries.slice(0, RANKING_ROWS).map((e) => ({
      rank: e.rank,
      username: String(e.username ?? ""),
      leaves: formatU64(e.leaves_earned),
      isMe: me !== "" && String(e.username ?? "").toLowerCase() === me,
    })),
  };
}

const POINTS = "pts de humedad";
// Reducida y reutilizada se cuentan en puntos de humedad; reciclada y los
// saldos del depósito, en litros. La equivalencia entre ambas unidades sigue
// pendiente en el documento inicial, así que no se convierten ni se suman.
const REPORT_ROWS = [
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

/**
 * @param {any} body respuesta de `GET /api/report`
 */
export function toReportView(body) {
  const totals = body?.totals ?? {};
  return {
    scope: body?.scope === "global" ? "global" : "me",
    period: `Época ${body?.epoch ?? "—"} · ticks ${formatU64(body?.from_tick)} a ${formatU64(body?.to_tick)}`,
    rows: REPORT_ROWS.map(([key, label, unit]) => {
      const value = formatU64(totals[key]);
      return { key, label, value: unit && value !== "—" ? `${value} ${unit}` : value };
    }),
    note: EQUIVALENCE_NOTE,
  };
}

/**
 * Mensaje para el usuario según el estado HTTP (0: sin respuesta).
 * `kind`: session (volver a entrar), wait (esperar `waitMs`), unavailable,
 * empty (sin datos todavía) o error.
 * @param {number} status
 * @param {number} [retryAfterSeconds]
 */
export function describeReadError(status, retryAfterSeconds) {
  switch (status) {
    case 401:
      return { kind: "session", message: "la sesión caducó, vuelve a entrar", waitMs: 0 };
    case 429: {
      if (Number.isInteger(retryAfterSeconds) && retryAfterSeconds >= 0) {
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

async function load(fetchFn, { url, init }, toView) {
  let response;
  try {
    response = await fetchFn(url, init);
  } catch {
    return { ok: false, error: describeReadError(0) };
  }
  if (!response.ok) {
    const seconds = Number.parseInt(response.headers.get("retry-after") ?? "", 10);
    return { ok: false, error: describeReadError(response.status, Number.isInteger(seconds) ? seconds : undefined) };
  }
  let body;
  try {
    body = await response.json();
  } catch {
    return { ok: false, error: describeReadError(response.status) };
  }
  return { ok: true, view: toView(body) };
}

/**
 * @param {typeof fetch} fetchFn
 * @param {string} token
 * @param {string} username jugador actual, para resaltarlo
 */
export function loadRanking(fetchFn, token, username) {
  return load(fetchFn, buildRankingRequest(token), (body) => toRankingView(body, username));
}

/**
 * @param {typeof fetch} fetchFn
 * @param {string} token
 * @param {"me" | "global"} scope
 */
export function loadReport(fetchFn, token, scope) {
  return load(fetchFn, buildReportRequest(token, scope), toReportView);
}

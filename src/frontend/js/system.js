// Panel del sistema (RNF-08): consulta GET /api/system y lo convierte en una
// vista para mostrar. Funciones sin DOM: `fetch` se inyecta para poder
// probarlas con node:test.
import { formatU64 } from "./reports.js";

export { formatU64 };

/** Ruta de la API (nginx reenvía /api/ sin reescribirla). */
export const SYSTEM_URL = "/api/system";
/** Cada cuánto se consulta (ms). El límite de la API es 120 por minuto por IP. */
export const POLL_MS = 2000;

const MAX_FAILURE_DELAY_MS = 30_000;
const DEFAULT_RETRY_MS = 10_000;
const MAX_RETRY_MS = 60_000;
const DASH = "—";

const SERVICE_LABELS = {
  nginx: "Proxy de entrada (nginx)",
  api: "API",
  hub: "Hub en tiempo real",
  world: "Simulador del valle",
  persistence: "Persistencia",
};

const DETAIL_LABELS = {
  timeout: "sin respuesta a tiempo",
  unavailable: "no disponible",
  not_serving: "no está sirviendo",
  error: "error",
  invalid_response: "respuesta no válida",
};

const BREAKER_LABELS = { closed: "Cerrado", open: "Abierto", half_open: "Semiabierto" };

/**
 * Número con un decimal, coma decimal y puntos de millar (es-VE a mano, para
 * no depender de los datos de idioma del entorno).
 * @param {unknown} value
 */
export function formatDecimal(value) {
  if (typeof value !== "number" || !Number.isFinite(value)) return DASH;
  const text = value.toLocaleString("en-US", { minimumFractionDigits: 1, maximumFractionDigits: 1 });
  return text.replace(/[,.]/g, (c) => (c === "," ? "." : ","));
}

function formatCount(value) {
  return Number.isInteger(value) && value >= 0 ? formatU64(String(value)) : DASH;
}

function formatBool(value) {
  if (typeof value !== "boolean") return DASH;
  return value ? "Sí" : "No";
}

function toServiceRow(service) {
  const up = service?.status === "up";
  const name = String(service?.name ?? "");
  const detail = up || typeof service?.detail !== "string" ? "" : (DETAIL_LABELS[service.detail] ?? service.detail);
  return { name, label: SERVICE_LABELS[name] ?? name, up, statusText: up ? "En línea" : "Caído", detail };
}

function toHubView(hub) {
  return {
    tick: formatU64(hub.tick),
    sessions: formatCount(hub.sessions),
    goroutines: formatCount(hub.goroutines),
    actionsAdmitted: formatDecimal(hub.actions_per_second?.admitted),
    actionsApplied: formatDecimal(hub.actions_per_second?.applied),
    latencyP50: formatDecimal(hub.latency_ms?.p50),
    latencyP95: formatDecimal(hub.latency_ms?.p95),
    stepLast: formatDecimal(hub.step_ms?.last),
    stepP95: formatDecimal(hub.step_ms?.p95),
    stepFailures: formatCount(hub.step_ms?.failures_total),
    queue: formatCount(hub.queue),
    paused: formatBool(hub.paused),
    breaker: BREAKER_LABELS[hub.breaker] ?? DASH,
    humidityTotal: formatU64(hub.humidity_total),
    humidityContributed: formatU64(hub.humidity_contributed),
    acceptedDrops: formatU64(hub.accepted_drops),
    activeFires: formatCount(hub.active_fires),
  };
}

/**
 * Respuesta de GET /api/system → vista. `hub` es `null` si el hub no responde.
 * @param {any} body
 */
export function toSystemView(body) {
  const hub = body?.hub;
  return {
    generatedAt: typeof body?.generated_at === "string" ? body.generated_at : "",
    services: body.services.map(toServiceRow),
    hub: hub && typeof hub === "object" ? toHubView(hub) : null,
  };
}

/**
 * Mensaje según el estado HTTP (0: sin respuesta). `kind`: wait (429, esperar
 * `waitMs`) u offline (cualquier otro fallo).
 * @param {number} status
 * @param {number} [retryAfterSeconds]
 */
export function describeSystemError(status, retryAfterSeconds) {
  if (status === 429) {
    if (Number.isInteger(retryAfterSeconds) && retryAfterSeconds >= 0) {
      const waitMs = Math.min(Math.max(retryAfterSeconds * 1000, POLL_MS), MAX_RETRY_MS);
      return { kind: "wait", message: `demasiadas consultas, reintentando en ${waitMs / 1000} s`, waitMs };
    }
    return { kind: "wait", message: "demasiadas consultas, reintentando en unos segundos", waitMs: DEFAULT_RETRY_MS };
  }
  return { kind: "offline", message: "sin conexión con la API", waitMs: 0 };
}

/**
 * Nunca rechaza: devuelve `{ ok: true, view }` o `{ ok: false, error }`.
 * @param {typeof fetch} fetchFn
 */
export async function loadSystem(fetchFn) {
  let response;
  try {
    response = await fetchFn(SYSTEM_URL, { method: "GET" });
  } catch {
    return { ok: false, error: describeSystemError(0) };
  }
  if (!response.ok) {
    const seconds = Number.parseInt(response.headers.get("retry-after") ?? "", 10);
    return { ok: false, error: describeSystemError(response.status, Number.isInteger(seconds) ? seconds : undefined) };
  }
  try {
    const body = await response.json();
    if (!body || !Array.isArray(body.services)) return { ok: false, error: describeSystemError(0) };
    return { ok: true, view: toSystemView(body) };
  } catch {
    return { ok: false, error: describeSystemError(0) };
  }
}

/**
 * Espera hasta la próxima consulta: cada POLL_MS si todo va bien, el doble tras
 * cada fallo (hasta 30 s) y, en un 429, lo que pide `Retry-After` sin alargar
 * la racha de fallos.
 */
export function createPollBackoff() {
  let failures = 0;
  return {
    /** @param {{ok: boolean, error?: {kind: string, waitMs: number}}} result */
    next(result) {
      if (result.ok) {
        failures = 0;
        return POLL_MS;
      }
      if (result.error?.kind === "wait") return result.error.waitMs;
      failures += 1;
      return Math.min(POLL_MS * 2 ** failures, MAX_FAILURE_DELAY_MS);
    },
  };
}

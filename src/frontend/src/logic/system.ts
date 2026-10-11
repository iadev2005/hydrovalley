// Panel del sistema (RNF-08): consulta GET /api/system y lo convierte en una
// vista para mostrar. Funciones sin DOM: `fetch` se inyecta para poder
// probarlas con vitest.
import { formatU64 } from "./reports.ts";
import type { FetchFn } from "./auth.ts";

export { formatU64 };

/** Ruta de la API (nginx reenvía /api/ sin reescribirla). */
export const SYSTEM_URL = "/api/system";
/** Cada cuánto se consulta (ms). El límite de la API es 120 por minuto por IP. */
export const POLL_MS = 2000;

const MAX_FAILURE_DELAY_MS = 30_000;
const DEFAULT_RETRY_MS = 10_000;
const MAX_RETRY_MS = 60_000;
const DASH = "—";

const SERVICE_LABELS: Record<string, string> = {
  nginx: "Proxy de entrada (nginx)",
  api: "API",
  hub: "Hub en tiempo real",
  world: "Simulador del valle",
  persistence: "Persistencia",
};

const DETAIL_LABELS: Record<string, string> = {
  timeout: "sin respuesta a tiempo",
  unavailable: "no disponible",
  not_serving: "no está sirviendo",
  error: "error",
  invalid_response: "respuesta no válida",
};

const BREAKER_LABELS: Record<string, string> = {
  closed: "Cerrado",
  open: "Abierto",
  half_open: "Semiabierto",
};

/**
 * Número con un decimal, coma decimal y puntos de millar (es-VE a mano, para
 * no depender de los datos de idioma del entorno).
 */
export function formatDecimal(value: unknown): string {
  if (typeof value !== "number" || !Number.isFinite(value)) return DASH;
  const text = value.toLocaleString("en-US", { minimumFractionDigits: 1, maximumFractionDigits: 1 });
  return text.replace(/[,.]/g, (c) => (c === "," ? "." : ","));
}

function formatCount(value: unknown): string {
  return typeof value === "number" && Number.isInteger(value) && value >= 0
    ? formatU64(String(value))
    : DASH;
}

function formatBool(value: unknown): string {
  if (typeof value !== "boolean") return DASH;
  return value ? "Sí" : "No";
}

export interface ServiceRowView {
  name: string;
  label: string;
  up: boolean;
  statusText: string;
  detail: string;
}

function toServiceRow(service: unknown): ServiceRowView {
  const rec = (service && typeof service === "object" ? service : {}) as Record<string, unknown>;
  const up = rec["status"] === "up";
  const name = String(rec["name"] ?? "");
  const detail =
    up || typeof rec["detail"] !== "string"
      ? ""
      : (DETAIL_LABELS[rec["detail"]] ?? rec["detail"]);
  return {
    name,
    label: SERVICE_LABELS[name] ?? name,
    up,
    statusText: up ? "En línea" : "Caído",
    detail,
  };
}

export interface HubView {
  tick: string;
  sessions: string;
  goroutines: string;
  actionsAdmitted: string;
  actionsApplied: string;
  latencyP50: string;
  latencyP95: string;
  stepLast: string;
  stepP95: string;
  stepFailures: string;
  queue: string;
  paused: string;
  breaker: string;
  humidityTotal: string;
  humidityContributed: string;
  acceptedDrops: string;
  activeFires: string;
}

function toHubView(hub: Record<string, unknown>): HubView {
  const actions = (hub["actions_per_second"] && typeof hub["actions_per_second"] === "object"
    ? hub["actions_per_second"]
    : {}) as Record<string, unknown>;
  const latency = (hub["latency_ms"] && typeof hub["latency_ms"] === "object"
    ? hub["latency_ms"]
    : {}) as Record<string, unknown>;
  const step = (hub["step_ms"] && typeof hub["step_ms"] === "object"
    ? hub["step_ms"]
    : {}) as Record<string, unknown>;

  const breakerKey = typeof hub["breaker"] === "string" ? hub["breaker"] : "";

  return {
    tick: formatU64(hub["tick"]),
    sessions: formatCount(hub["sessions"]),
    goroutines: formatCount(hub["goroutines"]),
    actionsAdmitted: formatDecimal(actions["admitted"]),
    actionsApplied: formatDecimal(actions["applied"]),
    latencyP50: formatDecimal(latency["p50"]),
    latencyP95: formatDecimal(latency["p95"]),
    stepLast: formatDecimal(step["last"]),
    stepP95: formatDecimal(step["p95"]),
    stepFailures: formatCount(step["failures_total"]),
    queue: formatCount(hub["queue"]),
    paused: formatBool(hub["paused"]),
    breaker: BREAKER_LABELS[breakerKey] ?? DASH,
    humidityTotal: formatU64(hub["humidity_total"]),
    humidityContributed: formatU64(hub["humidity_contributed"]),
    acceptedDrops: formatU64(hub["accepted_drops"]),
    activeFires: formatCount(hub["active_fires"]),
  };
}

export interface SystemView {
  generatedAt: string;
  services: ServiceRowView[];
  hub: HubView | null;
}

/**
 * Respuesta de GET /api/system → vista. `hub` es `null` si el hub no responde.
 */
export function toSystemView(body: unknown): SystemView {
  const rec = (body && typeof body === "object" ? body : {}) as Record<string, unknown>;
  const hub = rec["hub"];
  const services = Array.isArray(rec["services"]) ? (rec["services"] as unknown[]) : [];
  return {
    generatedAt: typeof rec["generated_at"] === "string" ? rec["generated_at"] : "",
    services: services.map(toServiceRow),
    hub: hub && typeof hub === "object" ? toHubView(hub as Record<string, unknown>) : null,
  };
}

export type SystemErrorKind = "wait" | "offline";

export interface SystemErrorDesc {
  kind: SystemErrorKind;
  message: string;
  waitMs: number;
}

/**
 * Mensaje según el estado HTTP (0: sin respuesta). `kind`: wait (429, esperar
 * `waitMs`) u offline (cualquier otro fallo).
 */
export function describeSystemError(status: number, retryAfterSeconds?: number): SystemErrorDesc {
  if (status === 429) {
    if (Number.isInteger(retryAfterSeconds) && retryAfterSeconds !== undefined && retryAfterSeconds >= 0) {
      const waitMs = Math.min(Math.max(retryAfterSeconds * 1000, POLL_MS), MAX_RETRY_MS);
      return { kind: "wait", message: `demasiadas consultas, reintentando en ${waitMs / 1000} s`, waitMs };
    }
    return { kind: "wait", message: "demasiadas consultas, reintentando en unos segundos", waitMs: DEFAULT_RETRY_MS };
  }
  return { kind: "offline", message: "sin conexión con la API", waitMs: 0 };
}

export type LoadSystemResult =
  | { ok: true; view: SystemView }
  | { ok: false; error: SystemErrorDesc };

/**
 * Nunca rechaza: devuelve `{ ok: true, view }` o `{ ok: false, error }`.
 */
export async function loadSystem(fetchFn: FetchFn): Promise<LoadSystemResult> {
  let response: Awaited<ReturnType<FetchFn>>;
  try {
    response = await fetchFn(SYSTEM_URL, { method: "GET" });
  } catch {
    return { ok: false, error: describeSystemError(0) };
  }
  if (!response.ok) {
    const seconds = Number.parseInt(response.headers.get("retry-after") ?? "", 10);
    return {
      ok: false,
      error: describeSystemError(response.status, Number.isInteger(seconds) ? seconds : undefined),
    };
  }
  try {
    const body = (await response.json()) as Record<string, unknown> | null;
    if (!body || !Array.isArray(body["services"])) return { ok: false, error: describeSystemError(0) };
    return { ok: true, view: toSystemView(body) };
  } catch {
    return { ok: false, error: describeSystemError(0) };
  }
}

export interface PollBackoff {
  next(result: { ok: boolean; error?: { kind: string; waitMs: number } }): number;
}

/**
 * Espera hasta la próxima consulta: cada POLL_MS si todo va bien, el doble tras
 * cada fallo (hasta 30 s) y, en un 429, lo que pide `Retry-After` sin alargar
 * la racha de fallos.
 */
export function createPollBackoff(): PollBackoff {
  let failures = 0;
  return {
    next(result: { ok: boolean; error?: { kind: string; waitMs: number } }): number {
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

// Cliente del `GET /stats` interno del hub (`HUB_STATS_ADDR`) para el panel
// del sistema. Valida la forma y copia solo los campos conocidos: la API no
// reenvía JSON arbitrario. Los enteros de 64 bits siguen siendo texto.

/** Foto del hub, con los mismos nombres y tipos que `GET /stats` (README del hub). */
export interface HubStats {
  tick: string;
  paused: boolean;
  breaker: "closed" | "open" | "half_open";
  queue: number;
  sessions: number;
  goroutines: number;
  actions_per_second: {
    admitted: number;
    applied: number;
    window_seconds: number;
  };
  latency_ms: { p50: number; p95: number; samples: number };
  step_ms: { last: number; p95: number; failures_total: number };
  humidity_total: string;
  humidity_contributed: string;
  accepted_drops: string;
  active_fires: number;
  boot_id: string;
  generated_at: string;
}

/** Resultado de consultar al hub: la foto validada o un motivo corto y seguro. */
export type HubResult =
  { readonly ok: true; readonly stats: HubStats } | { readonly ok: false; readonly detail: string };

export interface HubStatsOptions {
  /** `host:puerto` del HTTP interno del hub. */
  address: string;
  /** Plazo de la petición, cuerpo incluido, en milisegundos (1 s por defecto). */
  timeoutMs?: number;
  fetch?: typeof fetch;
}

const DEFAULT_TIMEOUT_MS = 1000;
const BREAKER_STATES: readonly string[] = ["closed", "open", "half_open"];

type Raw = Readonly<Record<string, unknown>>;

const isRecord = (value: unknown): value is Raw =>
  typeof value === "object" && value !== null && !Array.isArray(value);
const isCount = (value: unknown): value is number =>
  typeof value === "number" && Number.isSafeInteger(value) && value >= 0;
const isMeasure = (value: unknown): value is number =>
  typeof value === "number" && Number.isFinite(value) && value >= 0;
const isU64Text = (value: unknown): value is string =>
  typeof value === "string" && /^[0-9]{1,20}$/.test(value);

/** Valida la respuesta de `/stats` y copia solo los campos conocidos; `null` si no encaja. */
export function parseHubStats(raw: unknown): HubStats | null {
  if (!isRecord(raw)) return null;
  const aps = raw.actions_per_second;
  const lat = raw.latency_ms;
  const step = raw.step_ms;
  if (!isRecord(aps) || !isRecord(lat) || !isRecord(step)) return null;
  const { tick, paused, breaker, humidity_total, humidity_contributed, accepted_drops } = raw;
  if (!isU64Text(tick) || !isU64Text(humidity_total)) return null;
  if (!isU64Text(humidity_contributed) || !isU64Text(accepted_drops)) return null;
  if (typeof paused !== "boolean") return null;
  if (typeof breaker !== "string" || !BREAKER_STATES.includes(breaker)) return null;
  if (!isCount(raw.queue) || !isCount(raw.sessions) || !isCount(raw.goroutines)) return null;
  if (!isCount(raw.active_fires)) return null;
  if (!isMeasure(aps.admitted) || !isMeasure(aps.applied) || !isCount(aps.window_seconds)) {
    return null;
  }
  if (!isMeasure(lat.p50) || !isMeasure(lat.p95) || !isCount(lat.samples)) return null;
  if (!isMeasure(step.last) || !isMeasure(step.p95) || !isCount(step.failures_total)) return null;
  if (typeof raw.boot_id !== "string" || typeof raw.generated_at !== "string") return null;
  return {
    tick,
    paused,
    breaker: breaker as HubStats["breaker"],
    queue: raw.queue,
    sessions: raw.sessions,
    goroutines: raw.goroutines,
    actions_per_second: {
      admitted: aps.admitted,
      applied: aps.applied,
      window_seconds: aps.window_seconds,
    },
    latency_ms: { p50: lat.p50, p95: lat.p95, samples: lat.samples },
    step_ms: {
      last: step.last,
      p95: step.p95,
      failures_total: step.failures_total,
    },
    humidity_total,
    humidity_contributed,
    accepted_drops,
    active_fires: raw.active_fires,
    boot_id: raw.boot_id,
    generated_at: raw.generated_at,
  };
}

/**
 * Crea la consulta al hub. Nunca rechaza: `timeout`, `unavailable`,
 * `http_<código>` o `invalid_response`, sin mensajes ni direcciones.
 */
export function createHubStatsSource(options: HubStatsOptions): () => Promise<HubResult> {
  const timeoutMs = options.timeoutMs ?? DEFAULT_TIMEOUT_MS;
  const doFetch = options.fetch ?? fetch;
  const url = `http://${options.address}/stats`;

  return async () => {
    try {
      const res = await doFetch(url, {
        signal: AbortSignal.timeout(timeoutMs),
      });
      if (res.status !== 200) return { ok: false, detail: `http_${String(res.status)}` };
      let body: unknown;
      try {
        body = JSON.parse(await res.text());
      } catch (err) {
        if (isTimeout(err)) return { ok: false, detail: "timeout" };
        return { ok: false, detail: "invalid_response" };
      }
      const stats = parseHubStats(body);
      return stats ? { ok: true, stats } : { ok: false, detail: "invalid_response" };
    } catch (err) {
      return { ok: false, detail: isTimeout(err) ? "timeout" : "unavailable" };
    }
  };
}

function isTimeout(err: unknown): boolean {
  return err instanceof Error && (err.name === "TimeoutError" || err.name === "AbortError");
}

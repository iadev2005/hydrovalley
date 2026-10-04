// Arnés de fallos para la prueba de punta a punta: parar, arrancar, pausar y
// reanudar un servicio del stack con Docker Compose, y leer lo que el sistema
// muestra (GET /api/system por nginx).
//
// - stopService/startService simulan la caída del proceso: la conexión se
//   rechaza al instante.
// - pauseService/unpauseService (`docker pause`) simulan una máquina apagada:
//   el proceso se congela y las conexiones TCP quedan sin respuesta, así que
//   todo depende de los plazos.
//
// Docker Compose toma el proyecto, los archivos y las variables de entorno que
// exporta tests/e2e/run.sh (COMPOSE_PROJECT_NAME, COMPOSE_FILE,
// COMPOSE_ENV_FILES), igual que smoke.test.mjs. Las funciones puras se prueban
// sin el stack en faults.unit.test.mjs.
import { execFile } from "node:child_process";
import { promisify } from "node:util";

const run = promisify(execFile);

export const BASE_URL = (process.env.HV_BASE_URL ?? "http://localhost:8080").replace(/\/$/, "");
export const WS_BASE = BASE_URL.replace(/^http/, "ws");

export const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

// ---------------------------------------------------------------- Parte pura

/**
 * Repite `predicate` hasta que devuelva algo verdadero y lo devuelve. Un error
 * del predicado cuenta como «todavía no» (el servicio puede estar caído); si
 * se agota el plazo, el mensaje lleva la descripción y el último error.
 */
export async function waitFor(predicate, { timeoutMs = 30_000, intervalMs = 500, description = "the condition" } = {}) {
  const deadline = Date.now() + timeoutMs;
  let lastError;
  for (;;) {
    try {
      const value = await predicate();
      if (value) return value;
    } catch (err) {
      lastError = err;
    }
    if (Date.now() + intervalMs > deadline) break;
    await sleep(intervalMs);
  }
  throw new Error(`timed out after ${timeoutMs} ms waiting for ${description}${lastError ? `: ${lastError.message}` : ""}`);
}

/** Tick de 64 bits sin signo, que viaja como texto (regla 7); `null` si no lo es. */
export function parseTick(value) {
  if (typeof value === "number" && Number.isSafeInteger(value) && value >= 0) return BigInt(value);
  if (typeof value === "string" && /^\d+$/.test(value)) return BigInt(value);
  return null;
}

/** Ticks que el valle retrocedió: `antes - después`, nunca negativo. */
export function ticksLost(before, after) {
  return before > after ? before - after : 0n;
}

/**
 * Tick de la última instantánea (`tick % every == 0`, regla 8) que contiene a
 * `tick`. Tras un reinicio sin parada ordenada, Rust vuelve a ese tick.
 */
export function lastSnapshotTick(tick, every) {
  return tick - (tick % every);
}

/** El tick no avanzó entre las muestras (hace falta al menos dos). */
export function isStalled(samples) {
  return samples.length >= 2 && samples.every((tick) => tick === samples[0]);
}

const COMPOSE_ACTIONS = new Set(["stop", "start", "pause", "unpause", "kill"]);

/** Argumentos de `docker` para una acción sobre un servicio del stack. */
export function composeArgs(action, service) {
  if (!COMPOSE_ACTIONS.has(action)) throw new Error(`unsupported compose action: ${action}`);
  if (!service) throw new Error("a service name is required");
  return ["compose", action, service];
}

/** Percentil por rango más cercano; una muestra vacía es un error, no un 0. */
export function percentile(values, p) {
  if (values.length === 0) throw new Error("cannot take a percentile of an empty sample");
  const sorted = [...values].sort((x, y) => x - y);
  const rank = Math.max(1, Math.ceil((p / 100) * sorted.length));
  return sorted[rank - 1];
}

/**
 * Comprueba el `restored_tick` que informa el hub tras reiniciar Rust (CP-05):
 * múltiplo del intervalo de instantáneas (salvo parada ordenada, cuya
 * instantánea final cae en cualquier tick), no posterior al tick del fallo y a
 * no más de una instantánea de distancia. Devuelve las violaciones (vacío si
 * todo cumple).
 */
export function restoredTickViolations({ restored, tickAtFailure, every, graceful = false }) {
  const problems = [];
  if (!graceful && restored % every !== 0n) {
    problems.push(`restored_tick ${restored} is not a multiple of ${every}`);
  }
  if (restored > tickAtFailure) {
    problems.push(`restored_tick ${restored} is later than the tick at the failure ${tickAtFailure}`);
  } else if (tickAtFailure - restored > every) {
    problems.push(`gap ${tickAtFailure - restored} between the failure ${tickAtFailure} and restored_tick ${restored} exceeds ${every}`);
  }
  return problems;
}

/** Mayor tick entre los mensajes `state` (`null` si no hay ninguno). */
export function maxStateTick(frames) {
  let max = null;
  for (const f of frames) {
    if (f.type !== "state") continue;
    const tick = parseTick(f.tick);
    if (tick !== null && (max === null || tick > max)) max = tick;
  }
  return max;
}

/** Estado de un servicio dentro del cuerpo de /api/system. */
export function serviceStatus(system, name) {
  return system.services.find((s) => s.name === name);
}

/** Una línea compacta con lo medido; lo que falta se marca `n/d`, no se inventa. */
export function formatMeasurement(label, values) {
  const parts = Object.entries(values).map(([key, value]) => {
    if (value === undefined || value === null) return `${key}=n/d`;
    if (typeof value === "number") return `${key}=${value.toFixed(1)}`;
    return `${key}=${value}`;
  });
  return `MEDICION ${label}: ${parts.join(" ")}`;
}

// ------------------------------------------------------------ Envoltorios finos

async function compose(action, service) {
  await run("docker", composeArgs(action, service));
}

export const stopService = (name) => compose("stop", name);
/** SIGKILL: el proceso muere sin parada ordenada (sin instantánea final). */
export const killService = (name) => compose("kill", name);
export const startService = (name) => compose("start", name);
export const pauseService = (name) => compose("pause", name);
export const unpauseService = (name) => compose("unpause", name);

/** GET /api/system por nginx: el cuerpo, o falla si no responde 200. */
export async function systemStatus({ timeoutMs = 10_000 } = {}) {
  const res = await fetch(`${BASE_URL}/api/system`, { signal: AbortSignal.timeout(timeoutMs) });
  if (res.status !== 200) throw new Error(`GET /api/system answered ${res.status}`);
  return res.json();
}

/** Tick del hub según /api/system (`null` si el hub no contesta). */
export async function hubTick() {
  const system = await systemStatus();
  return system.hub ? parseTick(system.hub.tick) : null;
}

/** Mide cuánto tarda `fn`, en milisegundos, y devuelve también su valor. */
export async function timed(fn) {
  const start = performance.now();
  const value = await fn();
  return { value, ms: performance.now() - start };
}

/**
 * Token del administrador efímero de run.sh (inicio de sesión: no gasta uno de
 * los 5 registros por IP). Sirve como jugador: una gota no pide más rol.
 */
export async function loginAdmin() {
  const username = process.env.ADMIN_USERNAME;
  const password = process.env.E2E_ADMIN_PASSWORD;
  if (!username || !password) throw new Error("ADMIN_USERNAME and E2E_ADMIN_PASSWORD are required (run.sh sets them)");
  const res = await fetch(`${BASE_URL}/api/auth/login`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ username, password }),
  });
  if (res.status !== 200) throw new Error(`admin login answered ${res.status}`);
  return (await res.json()).token;
}

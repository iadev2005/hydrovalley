// Utilidades compartidas por los casos de fallos (faults.test.mjs y
// faults-hub-persistence.test.mjs): constantes, estado sano del stack, sesiones
// de /play, un muestreador de /api/system y la espera de un estado posterior a
// un resultado. Lo puro está en faults.mjs.
import { randomUUID } from "node:crypto";
import { PlaySession } from "./play-session.mjs";
import {
  WS_BASE,
  sleep,
  waitFor,
  parseTick,
  maxStateTick,
  systemStatus,
  startService,
  unpauseService,
} from "./faults.mjs";

export const SKIP = process.env.E2E_SKIP_FAULTS === "1" ? "E2E_SKIP_FAULTS=1" : false;
export const SNAPSHOT_EVERY = 50n;
export const OUTAGE_MS = 10_000;
export const READY_TIMEOUT_MS = Number(process.env.E2E_READY_TIMEOUT_S ?? 180) * 1000;
export const RECOVERY_TIMEOUT_MS = 30_000;
// /api/system guarda 1 s y admite 120 consultas por minuto por IP: se consulta
// como mucho una vez por segundo y pico.
export const POLL_MS = 1000;

export const isResult = (actionId) => (f) => f.type === "result" && f.action_id === actionId;

/** El stack está sano: todo `up`, hub con circuito cerrado y valle sin pausa. */
export function healthy(system) {
  return (
    system.services.every((s) => s.status === "up") &&
    system.hub &&
    system.hub.breaker === "closed" &&
    system.hub.paused === false
  );
}

export async function waitHealthy(timeoutMs = READY_TIMEOUT_MS) {
  return waitFor(async () => {
    const system = await systemStatus();
    return healthy(system) ? system : false;
  }, { timeoutMs, intervalMs: POLL_MS, description: "a healthy stack in /api/system" });
}

/**
 * Foto del hub que ya trae un `boot_id`: tras arrancar el hub, hasta su primer
 * `Step` correcto el `boot_id` está vacío y el tick vale 0, así que leer antes
 * daría identificadores ausentes. Hay que esperarla ANTES de provocar un fallo
 * para comparar contra ella.
 */
export async function waitHubIdentity(timeoutMs = READY_TIMEOUT_MS) {
  return waitFor(async () => {
    const hub = (await systemStatus()).hub;
    return hub && hub.boot_id && parseTick(hub.tick) > 0n ? hub : false;
  }, { timeoutMs, intervalMs: POLL_MS, description: "a hub snapshot with a boot_id and a tick above 0" });
}

export async function openSession(token) {
  const session = new PlaySession(`${WS_BASE}/play?token=${token}&map=false`);
  await session.opened;
  await session.first("a state frame without pause", (f) => f.type === "state" && !f.paused, 15_000);
  return session;
}

/**
 * Se reconecta como lo hace el cliente (reintenta hasta que /play acepta) y
 * devuelve la sesión nueva y el tiempo que tardó, en milisegundos.
 */
export async function reconnect(token, { timeoutMs = RECOVERY_TIMEOUT_MS } = {}) {
  const start = performance.now();
  const session = await waitFor(async () => {
    try {
      return await openSession(token);
    } catch {
      return false;
    }
  }, { timeoutMs, intervalMs: 500, description: "a new /play session" });
  return { session, ms: performance.now() - start };
}

/** Un mensaje de administración o una gota: espera su resultado `applied`. */
export async function command(session, message) {
  const actionId = randomUUID();
  session.send({ ...message, action_id: actionId });
  const result = await session.first(`the result of ${message.type} ${actionId}`, isResult(actionId), 10_000);
  if (result.status !== "applied") throw new Error(`${message.type}: ${JSON.stringify(result)}`);
  return result;
}

/** Primer estado que llega después de `result` y no puede ser anterior a él. */
export function stateAfter(session, result, timeoutMs = 10_000) {
  const index = session.frames.indexOf(result);
  const seen = maxStateTick(session.frames.slice(0, index + 1));
  return session.waitFor(
    "a state frame after the result",
    (f) => f.type === "state" && (seen === null || parseTick(f.tick) > seen),
    timeoutMs,
    { fromIndex: index + 1 },
  );
}

/**
 * Muestrea /api/system en segundo plano, una vez cada `intervalMs` más lo que
 * tarde la respuesta. Un solo consumidor del panel evita el límite por IP.
 */
export function startSampler({ intervalMs = 1100 } = {}) {
  const samples = [];
  let stopped = false;
  const loop = (async () => {
    while (!stopped) {
      const start = performance.now();
      try {
        const system = await systemStatus();
        samples.push({ at: performance.now(), ms: performance.now() - start, system });
      } catch (error) {
        samples.push({ at: performance.now(), ms: performance.now() - start, error });
      }
      await sleep(intervalMs);
    }
  })();
  return {
    samples,
    /** Primera muestra (a partir de ahora) que cumple `predicate`. */
    waitFor(predicate, { timeoutMs = 30_000, description } = {}) {
      let from = samples.length;
      return waitFor(() => {
        while (from < samples.length) {
          const sample = samples[from++];
          if (sample.system && predicate(sample.system)) return sample;
        }
        return false;
      }, { timeoutMs, intervalMs: 100, description });
    },
    async stop() {
      stopped = true;
      await loop;
    },
  };
}

/** Deja los servicios corriendo y sin pausa, pase lo que pase en el caso. */
export async function restoreServices(...names) {
  for (const name of names) {
    await unpauseService(name).catch(() => {});
    await startService(name).catch(() => {});
  }
}

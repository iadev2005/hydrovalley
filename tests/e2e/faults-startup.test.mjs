// CP-08 (RNF-09): arranque desordenado con el stack levantado por run.sh.
//
// Se paran todos los servicios (`docker compose stop`, sin borrar volúmenes: el
// administrador efímero y las claves de la prueba siguen valiendo) y se
// levantan de uno en uno, con una pausa corta entre cada uno:
//   - orden inverso al de dependencia: nginx, api, hub, world, persistence y
//     postgres, de modo que cada servicio arranca sin que exista aquello de lo
//     que depende;
//   - orden directo (postgres primero), como contraste.
// Después no se toca nada. La política `restart: unless-stopped` no puede ser
// lo que salve al sistema: se comprueba con `docker inspect` que ningún
// contenedor se reinició solo (mismo `RestartCount` y mismo `StartedAt`), así
// que los servicios reintentan hasta encontrarse en vez de caerse.
//
// «Todo conectado» significa, visto desde fuera por nginx: los cinco servicios
// `up` en /api/system, el circuito cerrado y el tick avanzando, y un jugador
// que inicia sesión, abre /play y recibe `applied` en una gota. Se mide desde
// el último servicio arrancado y desde el primero. Si algo no conecta dentro
// de CONNECT_TIMEOUT_MS el caso falla: no se inventa ninguna cifra.
//
// Corre el último porque es el más disruptivo. Imprime líneas `MEDICION …`.
import { describe, it, before, after } from "node:test";
import assert from "node:assert/strict";
import {
  SKIP,
  POLL_MS,
  healthy,
  waitHealthy,
  waitHubIdentity,
  openSession,
  command,
  restoreServices,
} from "./faults-support.mjs";
import {
  sleep,
  waitFor,
  parseTick,
  restartViolations,
  formatMeasurement,
  startService,
  stopAllServices,
  inspectServices,
  systemStatus,
  loginAdmin,
} from "./faults.mjs";

const REVERSE_ORDER = ["nginx", "api", "hub", "world", "persistence", "postgres"];
const FORWARD_ORDER = [...REVERSE_ORDER].reverse();
// Pausa entre un arranque y el siguiente.
const START_GAP_MS = 2000;
// Cota generosa para que todo conecte; si no, el caso falla.
const CONNECT_TIMEOUT_MS = 120_000;

/**
 * Para todo, arranca los servicios en `order` y espera a que todo conecte sin
 * intervención. Devuelve los tiempos (ms) desde el primer y desde el último
 * arranque, y las lecturas de `docker inspect` de antes y después.
 */
async function startupCycle(order) {
  const baseline = await inspectServices(order);
  await stopAllServices();

  const afterStarts = {};
  const firstStartAt = performance.now();
  let lastStartAt = firstStartAt;
  for (const [index, name] of order.entries()) {
    if (index > 0) await sleep(START_GAP_MS);
    await startService(name);
    const reading = await inspectServices([name]);
    afterStarts[name] = reading[name];
    lastStartAt = performance.now();
  }
  const sinceLast = () => performance.now() - lastStartAt;
  const sinceFirst = () => performance.now() - firstStartAt;
  const stages = {};
  const deadline = firstStartAt + CONNECT_TIMEOUT_MS;
  const remaining = () => Math.max(1000, deadline - performance.now());

  // 1) Los cinco servicios `up`, circuito cerrado y el tick avanzando.
  let firstTick = null;
  await waitFor(async () => {
    const system = await systemStatus();
    if (!healthy(system)) return false;
    const tick = parseTick(system.hub.tick);
    if (tick === null) return false;
    if (firstTick === null) {
      firstTick = tick;
      return false;
    }
    return tick > firstTick;
  }, { timeoutMs: remaining(), intervalMs: POLL_MS, description: "every service up and the tick advancing" });
  stages.system = { last: sinceLast(), first: sinceFirst() };

  // 2) Un jugador inicia sesión, abre /play y una gota recibe `applied`. El
  // acceso tiene límite de intentos (10 por minuto): se pide con reintentos
  // espaciados a 7 s para no saturar el cupo si la API tardara en responder,
  // y solo la sesión se sondea cada segundo.
  const token = await waitFor(async () => loginAdmin(), { timeoutMs: remaining(), intervalMs: 7000, description: "the admin login to answer 200" });
  const player = await waitFor(async () => {
    let session;
    try {
      session = await openSession(token);
      await command(session, { type: "drop", x: 8, y: 26 });
      return session;
    } catch {
      session?.close();
      return false;
    }
  }, { timeoutMs: remaining(), intervalMs: 1000, description: "a player session to get a drop applied" });
  player.close();
  stages.player = { last: sinceLast(), first: sinceFirst() };

  const finalReading = await inspectServices(order);
  return { baseline, afterStarts, finalReading, stages };
}

function report(label, order, result) {
  const restarts = Object.entries(result.finalReading)
    .map(([name, v]) => `${name}:${v.restartCount}`)
    .join(",");
  console.log(
    formatMeasurement(`CP-08 ${label}`, {
      order: order.join(">"),
      start_gap_s: START_GAP_MS / 1000,
      all_up_since_last_start_s: result.stages.system.last / 1000,
      all_up_since_first_start_s: result.stages.system.first / 1000,
      connected_since_last_start_s: result.stages.player.last / 1000,
      connected_since_first_start_s: result.stages.player.first / 1000,
      restart_count: restarts,
    }),
  );
}

describe("CP-08: everything is down and the services start in a different order", { skip: SKIP }, () => {
  before(async () => {
    await waitHealthy();
    // Con el hub ya con `boot_id`: el caso empieza desde un stack estable.
    await waitHubIdentity();
  });

  after(async () => {
    // Pase lo que pase, nada queda parado: cada `start` es inocuo si ya corre.
    await restoreServices(...REVERSE_ORDER);
    await waitHealthy(60_000).catch(() => {});
  });

  for (const [label, order] of [["reverse order", REVERSE_ORDER], ["forward order", FORWARD_ORDER]]) {
    it(`CP-08 ${label} (${order.join(" > ")}): all end up connected with no manual action and no container restarted on its own`, { timeout: 300_000 }, async () => {
      let result;
      try {
        result = await startupCycle(order);
      } finally {
        // Si el ciclo falló, que el siguiente caso y el `after` no hereden nada parado.
        await restoreServices(...order);
      }
      // Nada se reinició solo: lo que conectó fue porque reintentó (RNF-09).
      // Contra la lectura de antes de parar solo vale RestartCount (el
      // StartedAt cambia a propósito con cada arranque manual).
      for (const [name, was] of Object.entries(result.baseline)) {
        assert.ok(result.finalReading[name].restartCount <= was.restartCount, `${name} RestartCount ${was.restartCount} -> ${result.finalReading[name].restartCount}: the restart policy restarted it`);
      }
      // Tras el arranque manual: ni otro StartedAt ni más reinicios.
      assert.deepEqual(restartViolations(result.afterStarts, result.finalReading), [], "a container restarted by itself after its manual start");
      report(label, order, result);
    });
  }
});

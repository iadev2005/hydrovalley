// CP-05: reinicio del simulador (RF-23) con el stack levantado por run.sh.
//
// Tres fallos del servicio world, cada uno de unos 10 s:
//   - parado (`docker compose stop`): el proceso muere y la conexión se rechaza;
//   - abatido (`docker compose kill` y luego `stop`, para que la política de
//     reinicio no lo levante antes): SIGKILL, sin la instantánea final de la
//     parada ordenada; es el caso que mide la pérdida real de hasta 5 s;
//   - pausado (`docker compose pause`): simula la máquina apagada, la conexión
//     queda sin respuesta y todo depende de los plazos (Step 200 ms, estado del
//     panel 1 s).
//
// Solo se mira lo que un cliente puede ver: /api/system por nginx (tick, pausa,
// *Circuit Breaker*, `boot_id`, `step_ms`) y los mensajes de /play. El
// `restored_tick` no se expone a los clientes: se infiere del primer tick que
// el hub informa tras el reinicio (el `Step` de la respuesta con el nuevo
// `boot_id` devuelve el tick siguiente al restaurado). Esa lectura llega con un
// retraso de hasta ~2 s (el panel guarda 1 s y se consulta cada 1 s), unos 20
// ticks, así que los «ticks perdidos» son una cota inferior.
//
// Cada caso imprime una línea `MEDICION …` con lo medido; no se inventan cifras.
// Variables: E2E_SKIP_FAULTS=1 omite estos casos (la sesión de /play usa el
// administrador efímero de run.sh para no gastar registros por IP).
import { describe, it, before, after } from "node:test";
import assert from "node:assert/strict";
import { randomUUID } from "node:crypto";
import { PlaySession } from "./play-session.mjs";
import {
  WS_BASE,
  sleep,
  waitFor,
  parseTick,
  ticksLost,
  isStalled,
  lastSnapshotTick,
  serviceStatus,
  formatMeasurement,
  stopService,
  killService,
  startService,
  pauseService,
  unpauseService,
  systemStatus,
  timed,
  loginAdmin,
} from "./faults.mjs";

const SKIP = process.env.E2E_SKIP_FAULTS === "1" ? "E2E_SKIP_FAULTS=1" : false;
const SNAPSHOT_EVERY = 50n;
const OUTAGE_MS = 10_000;
const READY_TIMEOUT_MS = Number(process.env.E2E_READY_TIMEOUT_S ?? 180) * 1000;
const RECOVERY_TIMEOUT_MS = 30_000;
// /api/system guarda 1 s y admite 120 consultas por minuto por IP: se consulta
// una vez por segundo.
const POLL_MS = 1000;

const isResult = (actionId) => (f) => f.type === "result" && f.action_id === actionId;

/** El stack está sano: todo `up`, hub con circuito cerrado y valle sin pausa. */
function healthy(system) {
  return (
    system.services.every((s) => s.status === "up") &&
    system.hub &&
    system.hub.breaker === "closed" &&
    system.hub.paused === false
  );
}

async function restoreAll() {
  // Ni una pausa ni una parada olvidadas deben dejar el stack roto al
  // siguiente caso. `unpause` falla si no estaba pausado: se ignora.
  await unpauseService("world").catch(() => {});
  await startService("world").catch(() => {});
}

async function waitHealthy(timeoutMs = READY_TIMEOUT_MS) {
  return waitFor(async () => {
    const system = await systemStatus();
    return healthy(system) ? system : false;
  }, { timeoutMs, intervalMs: POLL_MS, description: "a healthy stack in /api/system" });
}

async function openSession(token) {
  const session = new PlaySession(`${WS_BASE}/play?token=${token}&map=false`);
  await session.opened;
  await session.first("a state frame without pause", (f) => f.type === "state" && !f.paused, 15_000);
  return session;
}

/** Espera a que el tick supere la siguiente instantánea tras `tick` (más 6 ticks para que se guarde). */
async function waitPastSnapshot(tick) {
  const target = (tick / SNAPSHOT_EVERY + 1n) * SNAPSHOT_EVERY + 6n;
  return waitFor(async () => {
    const hub = (await systemStatus()).hub;
    const now = hub && parseTick(hub.tick);
    return now !== null && now >= target ? now : false;
  }, { timeoutMs: 30_000, intervalMs: POLL_MS, description: `the tick to pass ${target}` });
}

describe("CP-05: the world service fails and comes back", { skip: SKIP }, () => {
  let token;
  const sessions = [];

  before(async () => {
    token = await loginAdmin();
    await waitHealthy();
  });

  after(async () => {
    for (const s of sessions) s.close();
    await restoreAll();
  });

  const cases = [
    { name: "killed (SIGKILL, no final snapshot)", kind: "kill", inject: async (n) => { await killService(n); await stopService(n); }, restore: startService, x: 7, y: 8 },
    { name: "stopped (process down, connection refused)", kind: "stop", inject: stopService, restore: startService, x: 3, y: 4 },
    { name: "paused (machine off, connection hangs)", kind: "pause", inject: pauseService, restore: unpauseService, x: 5, y: 6 },
  ];

  for (const c of cases) {
    it(`about ${OUTAGE_MS / 1000} s ${c.name}: the valley pauses, nothing is lost or applied twice`, { timeout: 180_000 }, async () => {
      await waitHealthy();
      const player = await openSession(token);
      sessions.push(player);
      let sessionClosed = false;
      player.closed.then(() => {
        sessionClosed = true;
      });

      try {
        // --- Preparación: la acción X se aplica y queda dentro de una instantánea.
        const before = (await systemStatus()).hub;
        const bootBefore = before.boot_id;
        const actionX = randomUUID();
        player.send({ type: "drop", action_id: actionX, x: c.x, y: c.y });
        const resultX = await player.first(`the result of X ${actionX}`, isResult(actionX), 10_000);
        assert.equal(resultX.status, "applied", JSON.stringify(resultX));
        // Cota superior del tick en que se aplicó X.
        const tickXUpper = parseTick((await systemStatus()).hub.tick);
        // Se espera a que pase la siguiente instantánea: así X está en la que
        // Rust recuperará y el reenvío debe salir `duplicate`.
        const tickT0 = await waitPastSnapshot(tickXUpper);
        const hubBefore = (await systemStatus()).hub;
        const t0 = parseTick(hubBefore.tick);
        assert.ok(t0 >= tickT0, `tick before the failure ${t0}`);

        // --- Fallo.
        await c.inject("world");
        const injectedAt = performance.now();
        const sinceInjected = () => performance.now() - injectedAt;

        // Pausa: el hub la ve, world sale `down` y /api/system sigue en 200
        // (systemStatus falla si no responde 200).
        const down = await waitFor(async () => {
          const system = await systemStatus();
          const world = serviceStatus(system, "world");
          return world.status === "down" && system.hub.paused ? system : false;
        }, { timeoutMs: 20_000, intervalMs: POLL_MS, description: "world down and the valley paused" });
        const detectMs = sinceInjected();
        assert.ok(down.hub, "the hub keeps answering while world is down");

        // *Circuit Breaker*: abierto (o semiabierto, cuando sondea) tras 3 fallos.
        const open = await waitFor(async () => {
          const hub = (await systemStatus()).hub;
          return ["open", "half_open"].includes(hub.breaker) ? hub : false;
        }, { timeoutMs: 20_000, intervalMs: POLL_MS, description: "the breaker open or half_open" });
        const breakerMs = sinceInjected();

        // Admisión durante la pausa: una gota nueva no recibe resultado ni
        // rechazo; queda pendiente (en la cola, `queue` ≥ 1).
        const actionD = randomUUID();
        const framesBeforeD = player.frames.length;
        player.send({ type: "drop", action_id: actionD, x: c.x + 1, y: c.y });
        await assert.rejects(player.waitFor(`an immediate result for D ${actionD}`, isResult(actionD), 2500), /timed out/);
        const early = player.frames.slice(framesBeforeD).filter((f) => f.type === "error" || isResult(actionD)(f));
        assert.deepEqual(early, [], "a drop sent while world is down is admitted, not rejected");

        // El tick no avanza y la cola guarda la gota; con step_ms.last se ve que
        // el plazo de Step (200 ms) manda sobre el bucle del tick.
        const samples = [];
        const stepLast = [];
        let queueMax = 0;
        for (let i = 0; i < 3; i++) {
          const hub = (await systemStatus()).hub;
          samples.push(parseTick(hub.tick));
          stepLast.push(hub.step_ms.last);
          queueMax = Math.max(queueMax, hub.queue);
          assert.equal(hub.paused, true, "the valley stays paused while world is down");
          if (i < 2) await sleep(POLL_MS + 100);
        }
        assert.ok(isStalled(samples), `the tick must not advance while paused: ${samples}`);
        assert.ok(queueMax >= 1, `the drop sent while paused waits in the queue (queue ${queueMax})`);
        assert.ok(Math.max(...stepLast) <= 500, `step_ms.last must stay near the 200 ms deadline: ${stepLast}`);
        const tickAtPause = samples[0];

        // Máquina apagada: el panel responde en torno a su plazo de 1 s.
        let systemMs;
        if (c.kind === "pause") {
          assert.ok(Math.max(...stepLast) >= 150, `Step must hit its 200 ms deadline against a hung world: ${stepLast}`);
          await sleep(POLL_MS + 100); // deja caducar la copia de 1 s
          const probe = await timed(() => systemStatus());
          systemMs = probe.ms;
          assert.equal(serviceStatus(probe.value, "world").status, "down");
          assert.ok(systemMs >= 700 && systemMs <= 2500, `/api/system took ${systemMs.toFixed(0)} ms against a hung world`);
        }

        assert.equal(sessionClosed, false, "the player's session survives the failure");

        // Resto de los ~10 s de fallo.
        await sleep(Math.max(0, OUTAGE_MS - sinceInjected()));

        // --- Recuperación.
        await c.restore("world");
        const restoredAt = performance.now();
        let worldUpMs;
        const recovered = await waitFor(async () => {
          const system = await systemStatus();
          if (worldUpMs === undefined && serviceStatus(system, "world").status === "up") {
            worldUpMs = performance.now() - restoredAt;
          }
          const hub = system.hub;
          const newBoot = c.kind !== "pause" ? hub.boot_id !== bootBefore : true;
          return healthy(system) && newBoot ? hub : false;
        }, { timeoutMs: RECOVERY_TIMEOUT_MS, intervalMs: POLL_MS, description: "world up, breaker closed and the tick advancing" });
        const recoverMs = performance.now() - restoredAt;
        const firstTick = parseTick(recovered.tick);
        const lost = ticksLost(tickAtPause, firstTick);

        // El hub detecta el reinicio (nuevo boot_id); tras una pausa el proceso
        // es el mismo y el boot_id no cambia.
        if (c.kind !== "pause") {
          assert.notEqual(recovered.boot_id, bootBefore, "the hub sees the new boot_id");
        } else {
          assert.equal(recovered.boot_id, bootBefore, "the same process resumed: boot_id does not change");
        }
        // Pérdida ≤ 5 s: el tick no retrocede más de una instantánea (50 ticks)
        // respecto del que había justo antes de parar.
        assert.ok(firstTick >= t0 - SNAPSHOT_EVERY, `first tick ${firstTick} must be >= ${t0 - SNAPSHOT_EVERY} (T0 ${t0} - 50)`);
        if (c.kind === "pause") assert.equal(lost, 0n, "a paused (not restarted) world loses no ticks");
        // Sin parada ordenada Rust vuelve a la última instantánea (tick % 50 ==
        // 0): se infiere de la primera lectura (el hub ya avanzó unos ticks
        // desde el arranque, menos de 50) y la pérdida es la distancia hasta
        // ella, como mucho una instantánea (5 s).
        let lostSinceSnapshot;
        if (c.kind === "kill") {
          const restoredSnapshot = lastSnapshotTick(firstTick, SNAPSHOT_EVERY);
          lostSinceSnapshot = tickAtPause - restoredSnapshot;
          assert.ok(lostSinceSnapshot >= 0n && lostSinceSnapshot <= SNAPSHOT_EVERY, `ticks since the restored snapshot ${restoredSnapshot}: ${lostSinceSnapshot} (max ${SNAPSHOT_EVERY} = 5 s)`);
        }
        // El tick vuelve a avanzar.
        const later = await waitFor(async () => {
          const t = parseTick((await systemStatus()).hub.tick);
          return t > firstTick ? t : false;
        }, { timeoutMs: 10_000, intervalMs: POLL_MS, description: "the tick advancing again" });
        assert.ok(later > firstTick);

        // La gota enviada en plena pausa recibe su resultado final.
        const resultD = await player.first(`the final result of D ${actionD}`, isResult(actionD), 20_000);
        assert.equal(resultD.status, "applied", JSON.stringify(resultD));

        // La sesión sigue abierta y recibe estados sin pausa.
        assert.equal(sessionClosed, false, "the session survived the world restart");
        const fromIndex = player.frames.length;
        await player.waitFor("a state frame without pause after recovery", (f) => f.type === "state" && !f.paused, 10_000, { fromIndex });

        // --- Nada se aplica dos veces: se reenvía X en una conexión nueva.
        const fresh = await openSession(token);
        sessions.push(fresh);
        const stateBefore = await fresh.waitFor("a state frame (baseline)", (f) => f.type === "state", 5000);
        assert.notEqual(stateBefore.accepted_drops, undefined, "state carries accepted_drops");
        assert.notEqual(stateBefore.humidity_contributed, undefined, "state carries humidity_contributed");
        const afterIndex = fresh.frames.length;
        fresh.send({ type: "drop", action_id: actionX, x: c.x, y: c.y });
        const resendX = await fresh.first(`the result of the resent X ${actionX}`, isResult(actionX), 10_000);
        const stateAfter = await fresh.waitFor("a state frame after the resend", (f) => f.type === "state", 5000, { fromIndex: afterIndex + 1 });
        const restoredEstimate = firstTick - 1n;
        if (resendX.status === "duplicate") {
          assert.equal(stateAfter.accepted_drops, stateBefore.accepted_drops, "a duplicate does not count as an accepted drop");
          assert.equal(stateAfter.humidity_contributed, stateBefore.humidity_contributed, "a duplicate adds no humidity");
        } else {
          // Permitido solo si X se aplicó después del tick restaurado (se perdió
          // con la ventana de 5 s y la regla 2 admite el reenvío).
          assert.equal(resendX.status, "applied", JSON.stringify(resendX));
          assert.ok(tickXUpper > restoredEstimate, `X ('applied' again) must be newer than the restored tick ${restoredEstimate}; X was applied by tick ${tickXUpper}`);
        }

        console.log(
          formatMeasurement(`CP-05 ${c.kind} world ${OUTAGE_MS / 1000}s`, {
            world_down_s: detectMs / 1000,
            breaker_open_s: breakerMs / 1000,
            world_up_s: worldUpMs === undefined ? undefined : worldUpMs / 1000,
            recover_s: recoverMs / 1000,
            tick_before: t0,
            tick_at_pause: tickAtPause,
            first_tick_after: firstTick,
            ticks_lost: lost,
            ticks_lost_vs_snapshot: lostSinceSnapshot,
            system_ms: systemMs === undefined ? undefined : Math.round(systemMs),
            step_ms_last_max: Math.max(...stepLast),
            resend_X: resendX.status,
            drop_D: resultD.status,
            breaker_seen: open.breaker,
          }),
        );
      } finally {
        await restoreAll();
        await waitHealthy().catch(() => {});
      }
    });
  }
});

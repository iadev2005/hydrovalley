// CP-06, CP-07 y CP-12 con el stack levantado por run.sh.
//
//   - CP-06 (RF-24): el hub parado (`stop`) y pausado (`pause`, la máquina
//     apagada) unos 10 s. El valle sigue intacto en Rust y el jugador se
//     reconecta y reenvía sus acciones pendientes sin que nada se aplique dos
//     veces.
//   - CP-07 (RNF-04): la persistencia parada, pausada y PostgreSQL parado unos
//     10 s. Quien juega sigue jugando (p95 < 200 ms) y un acceso nuevo recibe
//     un 503 claro dentro del plazo de la API.
//   - CP-12 (RF-23, RNF-04): la persistencia caída a través de ticks de
//     instantánea. `Step` mantiene su ritmo, el valle no se pausa, el *Circuit
//     Breaker* sigue cerrado y la instantánea siguiente se guarda al volver
//     (se comprueba reiniciando Rust y mirando su `restored_tick`).
//
// Como en faults.test.mjs, solo se mira /api/system, /api/auth/login y /play
// (por nginx) y se provoca el fallo con Docker. Los jugadores son el
// administrador efímero de run.sh en modo demo (la gota es gratis y suma
// exactamente 1 a la humedad aportada), que se apaga al terminar.
// Cada caso imprime una línea `MEDICION …` con lo medido.
import { describe, it, before, after } from "node:test";
import assert from "node:assert/strict";
import { randomUUID } from "node:crypto";
import {
  SKIP,
  SNAPSHOT_EVERY,
  OUTAGE_MS,
  RECOVERY_TIMEOUT_MS,
  POLL_MS,
  isResult,
  healthy,
  waitHealthy,
  waitHubIdentity,
  openSession,
  reconnect,
  command,
  stateAfter,
  startSampler,
  restoreServices,
} from "./faults-support.mjs";
import {
  BASE_URL,
  sleep,
  waitFor,
  parseTick,
  percentile,
  restoredTickViolations,
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

// La API tiene 3 s de presupuesto por llamada a la persistencia (2 s por
// intento): un acceso nuevo debe contestar dentro de eso, con margen.
const LOGIN_DEADLINE_MS = 4500;
const DROP_COUNT = 20;
const DROP_INTERVAL_MS = 250; // 4 gotas por segundo
const hubOf = (system) => system.hub;
// protojson omite los campos en cero: un contador sin el campo vale 0.
const counter = (frame, key) => BigInt(frame[key] ?? "0");

async function login(timeoutMs = 10_000) {
  const username = process.env.ADMIN_USERNAME;
  const password = process.env.E2E_ADMIN_PASSWORD;
  const { value: res, ms } = await timed(() =>
    fetch(`${BASE_URL}/api/auth/login`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ username, password }),
      signal: AbortSignal.timeout(timeoutMs),
    }),
  );
  const text = await res.text();
  let body;
  try {
    body = JSON.parse(text);
  } catch {
    body = undefined;
  }
  return { status: res.status, contentType: res.headers.get("content-type") ?? "", body, text, ms };
}

describe("CP-06, CP-07 and CP-12: hub and persistence faults", { skip: SKIP }, () => {
  let token;
  let admin; // sesión que enciende y apaga el modo demo
  const sessions = [];

  before(async () => {
    token = await loginAdmin();
    await waitHealthy();
    admin = await openSession(token);
    await command(admin, { type: "set_demo_mode", enabled: true });
    await admin.waitFor("a state with demo_mode on", (f) => f.type === "state" && f.demo_mode === true, 10_000);
  });

  after(async () => {
    for (const s of sessions) s.close();
    admin?.close();
    await restoreServices("hub", "persistence", "postgres", "world");
    // Deja el modo demo apagado para quien use el stack después.
    try {
      await waitHealthy(60_000);
      const last = await openSession(token);
      try {
        await command(last, { type: "set_demo_mode", enabled: false });
      } finally {
        last.close();
      }
    } catch {
      // Si no se pudo apagar, el stack se baja de todos modos al terminar.
    }
  });

  // ------------------------------------------------------------------ CP-06
  const hubCases = [
    { name: "stopped (process down)", kind: "stop", inject: stopService, restore: startService, x: 10, y: 18 },
    { name: "paused (machine off, connection hangs)", kind: "pause", inject: pauseService, restore: unpauseService, x: 11, y: 18 },
  ];

  for (const c of hubCases) {
    it(`CP-06 about ${OUTAGE_MS / 1000} s hub ${c.name}: the valley stays intact and the player reconnects`, { timeout: 180_000 }, async () => {
      await waitHealthy();
      const sampler = startSampler();
      const player = await openSession(token);
      sessions.push(player);
      let playerClosed = false;
      player.closed.then(() => {
        playerClosed = true;
      });

      try {
        // --- Preparación: A se aplicó y quedó confirmada; se anota la humedad.
        const actionA = randomUUID();
        player.send({ type: "drop", action_id: actionA, x: c.x, y: c.y });
        const resultA = await player.first(`the result of A ${actionA}`, isResult(actionA), 10_000);
        assert.equal(resultA.status, "applied", JSON.stringify(resultA));
        const baseline = await stateAfter(player, resultA);
        const humidity0 = counter(baseline, "humidity_contributed");
        // Con identificador (`boot_id`) y tick ya leídos: se captura antes del fallo.
        const hubBefore = await waitHubIdentity();
        const tickBefore = parseTick(hubBefore.tick);

        // Y: una gota enviada justo antes del fallo, cuyo resultado puede no llegar.
        const actionY = randomUUID();
        player.send({ type: "drop", action_id: actionY, x: c.x + 1, y: c.y });

        // --- Fallo.
        await c.inject("hub");
        const injectedAt = performance.now();
        const down = await sampler.waitFor(
          (system) => serviceStatus(system, "hub").status === "down",
          { timeoutMs: 20_000, description: "hub down in /api/system" },
        );
        const detectMs = performance.now() - injectedAt;
        assert.equal(hubOf(down.system), null, "hub is null while it is down");

        // Mientras el hub está caído, /api/system sigue en 200 con world y la
        // persistencia en `up` (Rust sigue en marcha, nadie le llama a Step).
        for (const name of ["world", "persistence"]) {
          assert.equal(serviceStatus(down.system, name).status, "up", `${name} stays up`);
        }
        let systemMs;
        if (c.kind === "pause") {
          await sleep(POLL_MS + 200); // deja caducar la copia de 1 s
          const probe = await timed(() => systemStatus());
          systemMs = probe.ms;
          assert.equal(hubOf(probe.value), null);
          assert.ok(systemMs >= 700 && systemMs <= 2500, `/api/system took ${systemMs.toFixed(0)} ms against a hung hub`);
        }
        if (c.kind === "stop") {
          // Parada ordenada: el hub cierra la conexión del jugador.
          await waitFor(() => playerClosed, { timeoutMs: 10_000, intervalMs: 100, description: "the player session to close" });
        }

        await sleep(Math.max(0, OUTAGE_MS - (performance.now() - injectedAt)));
        const during = sampler.samples.filter((s) => s.at >= injectedAt && s.at <= performance.now());
        assert.ok(during.every((s) => s.system), `/api/system answered 200 all along: ${during.filter((s) => s.error).map((s) => s.error.message)}`);

        // --- Recuperación.
        await c.restore("hub");
        const restoredAt = performance.now();
        // Sano Y con el primer `Step` del hub nuevo ya hecho: hasta entonces su
        // `boot_id` está vacío y el tick en 0.
        const recovered = await waitFor(async () => {
          const system = await systemStatus();
          const hub = system.hub;
          return healthy(system) && hub.boot_id && parseTick(hub.tick) > tickBefore ? system : false;
        }, { timeoutMs: RECOVERY_TIMEOUT_MS, intervalMs: POLL_MS, description: "a healthy stack with the new hub's first successful Step" });
        const recoverMs = performance.now() - restoredAt;
        const { session: fresh, ms: reconnectMs } = await reconnect(token);
        sessions.push(fresh);

        // El valle sigue intacto en Rust: mismo proceso, mismo tick restaurado y
        // el tick no retrocede (con el hub caído nadie llama a Step).
        const hubAfter = recovered.hub;
        assert.equal(hubAfter.boot_id, hubBefore.boot_id, "world was not restarted: same boot_id");
        assert.equal(hubAfter.restored_tick, hubBefore.restored_tick, "world was not restarted: same restored_tick");
        const tickAfter = parseTick(hubAfter.tick);
        assert.ok(tickAfter >= tickBefore, `the tick went back: ${tickBefore} -> ${tickAfter}`);

        // La respuesta original de Y puede haber llegado (pausa: la conexión
        // sobrevive) o no. Se le da un margen antes de reenviar.
        const originalY = await player.first(`the original result of Y ${actionY}`, isResult(actionY), 3000).catch(() => null);

        // --- Reenvío en la sesión nueva (lo que hace el cliente al reconectarse).
        const stateBeforeResend = await fresh.waitFor("a state frame (baseline)", (f) => f.type === "state", 5000);
        fresh.send({ type: "drop", action_id: actionY, x: c.x + 1, y: c.y });
        const resendY = await fresh.first(`the result of the resent Y ${actionY}`, isResult(actionY), 10_000);
        assert.ok(["applied", "duplicate"].includes(resendY.status), JSON.stringify(resendY));
        if (originalY?.status === "applied") {
          assert.equal(resendY.status, "duplicate", "Y was already applied: the resend must be a duplicate");
        }
        fresh.send({ type: "drop", action_id: actionA, x: c.x, y: c.y });
        const resendA = await fresh.first(`the result of the resent A ${actionA}`, isResult(actionA), 10_000);
        assert.equal(resendA.status, "duplicate", `A was applied before the failure: ${JSON.stringify(resendA)}`);

        const finalState = await stateAfter(fresh, resendA);
        // Y cuenta una sola vez en el valle (Rust es la autoridad) y A, nada.
        const humidityDelta = counter(finalState, "humidity_contributed") - humidity0;
        assert.equal(humidityDelta, 1n, `humidity_contributed grew by ${humidityDelta} (only Y counts, once)`);
        // La cuenta de gotas del hub: solo suma el reenvío de Y si se aplicó.
        const acceptedDelta = counter(finalState, "accepted_drops") - counter(stateBeforeResend, "accepted_drops");
        assert.equal(acceptedDelta, resendY.status === "applied" ? 1n : 0n, `accepted_drops grew by ${acceptedDelta}`);

        console.log(
          formatMeasurement(`CP-06 ${c.kind} hub ${OUTAGE_MS / 1000}s`, {
            hub_down_s: detectMs / 1000,
            system_ms: systemMs === undefined ? undefined : Math.round(systemMs),
            recover_s: recoverMs / 1000,
            reconnect_s: reconnectMs / 1000,
            tick_before: tickBefore,
            tick_after: tickAfter,
            world_boot_same: hubAfter.boot_id === hubBefore.boot_id,
            Y_original: originalY ? originalY.status : "sin_resultado",
            Y_resend: resendY.status,
            A_resend: resendA.status,
            humidity_delta: humidityDelta,
            accepted_delta: acceptedDelta,
          }),
        );
      } finally {
        await sampler.stop();
        await restoreServices("hub");
        await waitHealthy().catch(() => {});
      }
    });
  }

  // ------------------------------------------------------------------ CP-07
  const persistenceCases = [
    { name: "persistence stopped (process down)", kind: "persistence-stop", service: "persistence", inject: stopService, restore: startService },
    { name: "persistence paused (machine off, connection hangs)", kind: "persistence-pause", service: "persistence", inject: pauseService, restore: unpauseService },
    { name: "postgres stopped", kind: "postgres-stop", service: "postgres", inject: stopService, restore: startService },
  ];

  for (const c of persistenceCases) {
    it(`CP-07 about ${OUTAGE_MS / 1000} s ${c.name}: players keep playing under 200 ms and a new login gets a clear notice`, { timeout: 180_000 }, async () => {
      await waitHealthy();
      const sampler = startSampler();
      const player = await openSession(token);
      sessions.push(player);
      let playerClosed = false;
      player.closed.then(() => {
        playerClosed = true;
      });

      try {
        const hubBefore = (await systemStatus()).hub;
        const failuresBefore = hubBefore.step_ms.failures_total;

        // --- Fallo.
        await c.inject(c.service);
        const injectedAt = performance.now();
        const down = await sampler.waitFor(
          (system) => serviceStatus(system, "persistence").status === "down",
          { timeoutMs: 30_000, description: `persistence down in /api/system after ${c.kind}` },
        );
        const detectMs = performance.now() - injectedAt;
        const detail = serviceStatus(down.system, "persistence").detail;
        assert.ok(down.system.hub, "the hub keeps answering while persistence is down");

        // --- «Quien juega sigue jugando en menos de 200 ms»: 20 gotas a 4 por
        // segundo; se mide de enviar a resultado final.
        const latencies = [];
        const statuses = [];
        for (let i = 0; i < DROP_COUNT; i++) {
          const actionId = randomUUID();
          const sentAt = performance.now();
          player.send({ type: "drop", action_id: actionId, x: i % 16, y: 20 + Math.floor(i / 16) });
          const result = await player.first(`the result of drop ${i} (${actionId})`, isResult(actionId), 10_000);
          latencies.push(performance.now() - sentAt);
          statuses.push(result.status);
          await sleep(Math.max(0, DROP_INTERVAL_MS - (performance.now() - sentAt)));
        }
        assert.deepEqual([...new Set(statuses)], ["applied"], `every drop is applied: ${statuses}`);
        const p95 = percentile(latencies, 95);
        const p50 = percentile(latencies, 50);
        const max = Math.max(...latencies);
        assert.ok(p95 < 200, `p95 of the drop latency during the outage is ${p95.toFixed(1)} ms (max ${max.toFixed(1)} ms)`);
        assert.equal(playerClosed, false, "the player's session survives the outage");

        // --- «Un acceso nuevo recibe un aviso claro». Se evita el limitador de
        // acceso (10 por usuario e IP por minuto): un solo intento por fallo.
        const attempt = await login();
        assert.equal(attempt.status, 503, `login during the outage: ${attempt.status} ${attempt.text}`);
        assert.ok(attempt.contentType.startsWith("application/json"), attempt.contentType);
        assert.equal(typeof attempt.body?.error, "string", attempt.text);
        assert.ok(attempt.body.error.length > 0);
        assert.ok(attempt.ms < LOGIN_DEADLINE_MS, `login took ${attempt.ms.toFixed(0)} ms (deadline ${LOGIN_DEADLINE_MS} ms)`);

        // Resto de los ~10 s.
        await sleep(Math.max(0, OUTAGE_MS - (performance.now() - injectedAt)));

        // El valle no se pausó, el circuito no se abrió y /api/system respondió siempre 200.
        const during = sampler.samples.filter((s) => s.at >= injectedAt);
        // Con la persistencia pausada cada consulta del panel tarda ~1 s (su
        // plazo), así que caben pocas muestras en 10 s.
        assert.ok(during.length >= 3, `samples during the outage: ${during.length}`);
        assert.ok(during.every((s) => s.system), `/api/system answered 200 all along: ${during.filter((s) => s.error).map((s) => s.error.message)}`);
        assert.ok(during.every((s) => s.system.hub.breaker === "closed" && s.system.hub.paused === false), "breaker closed and valley not paused during the outage");
        assert.equal(serviceStatus(during.at(-1).system, "world").status, "up");

        // --- Recuperación: todo vuelve a `up` y un acceso nuevo funciona.
        await c.restore(c.service);
        const restoredAt = performance.now();
        await waitFor(async () => healthy(await systemStatus()), { timeoutMs: RECOVERY_TIMEOUT_MS, intervalMs: POLL_MS, description: "every service up again" });
        const recoverMs = performance.now() - restoredAt;
        const loginAfter = await login();
        assert.equal(loginAfter.status, 200, `login after the recovery: ${loginAfter.status} ${loginAfter.text}`);
        const afterDrop = randomUUID();
        player.send({ type: "drop", action_id: afterDrop, x: 15, y: 31 });
        const afterResult = await player.first(`the result of ${afterDrop}`, isResult(afterDrop), 10_000);
        assert.equal(afterResult.status, "applied");
        const failuresAfter = (await systemStatus()).hub.step_ms.failures_total;
        assert.equal(failuresAfter, failuresBefore, "no Step failed because of the persistence outage");

        console.log(
          formatMeasurement(`CP-07 ${c.kind} ${OUTAGE_MS / 1000}s`, {
            persistence_down_s: detectMs / 1000,
            persistence_detail: detail ?? "n/d",
            drops: latencies.length,
            drop_p50_ms: p50,
            drop_p95_ms: p95,
            drop_max_ms: max,
            login_status: attempt.status,
            login_ms: Math.round(attempt.ms),
            login_error: attempt.body.error,
            recover_s: recoverMs / 1000,
            login_after: loginAfter.status,
            step_failures: failuresAfter - failuresBefore,
          }),
        );
      } finally {
        await sampler.stop();
        await restoreServices(c.service);
        await waitHealthy().catch(() => {});
      }
    });
  }

  // ------------------------------------------------------------------ CP-12
  it(`CP-12 persistence down across snapshot ticks: Step keeps its rhythm and the next snapshot is saved on return`, { timeout: 240_000 }, async () => {
    await waitHealthy();
    const sampler = startSampler();
    const player = await openSession(token);
    sessions.push(player);
    let playerClosed = false;
    player.closed.then(() => {
      playerClosed = true;
    });

    try {
      // Se corta justo después de una instantánea (tick % 50 en 1..10): los
      // ~100 ticks del fallo cruzan al menos una instantánea futura.
      const start = await sampler.waitFor((system) => {
        const tick = parseTick(system.hub?.tick);
        return tick !== null && tick % SNAPSHOT_EVERY >= 1n && tick % SNAPSHOT_EVERY <= 10n;
      }, { timeoutMs: 30_000, description: "a tick just after a snapshot tick" });
      const startHub = start.system.hub;
      const failuresBefore = startHub.step_ms.failures_total;
      const stateIndex = player.frames.length;

      await stopService("persistence");
      const injectedAt = performance.now();
      await sampler.waitFor((system) => serviceStatus(system, "persistence").status === "down", { timeoutMs: 20_000, description: "persistence down" });
      await sleep(Math.max(0, OUTAGE_MS - (performance.now() - injectedAt)));

      const during = sampler.samples.filter((s) => s.at >= injectedAt);
      assert.ok(during.every((s) => s.system), `/api/system answered 200 all along: ${during.filter((s) => s.error).map((s) => s.error.message)}`);
      const ticks = during.map((s) => parseTick(s.system.hub.tick));
      const firstTick = ticks[0];
      const lastTick = ticks.at(-1);
      const crossed = lastTick / SNAPSHOT_EVERY - firstTick / SNAPSHOT_EVERY;
      assert.ok(crossed >= 1n, `the outage must cross at least one snapshot tick (ticks ${firstTick}..${lastTick})`);
      const seconds = (during.at(-1).at - during[0].at) / 1000;
      const ticksPerSecond = Number(lastTick - firstTick) / seconds;
      assert.ok(ticksPerSecond >= 8, `the tick keeps its 100 ms rhythm: ${ticksPerSecond.toFixed(1)} ticks/s`);
      assert.ok(during.every((s) => s.system.hub.breaker === "closed" && s.system.hub.paused === false), "breaker closed and valley not paused during the outage");
      const stepLastMax = Math.max(...during.map((s) => s.system.hub.step_ms.last));
      const stepP95Max = Math.max(...during.map((s) => s.system.hub.step_ms.p95));
      assert.ok(stepLastMax < 100, `Step stays under the tick period: step_ms.last max ${stepLastMax}`);
      assert.equal(during.at(-1).system.hub.step_ms.failures_total, failuresBefore, "no Step failed during the outage");
      assert.equal(playerClosed, false, "the player's session survives");
      const pausedFrames = player.frames.slice(stateIndex).filter((f) => f.type === "state" && f.paused);
      assert.deepEqual(pausedFrames, [], "no state frame announced a pause");

      // --- Vuelve la persistencia. Se espera a que lleve unos segundos arriba
      // y se toma la primera instantánea posterior: debe guardarse.
      await startService("persistence");
      const restoredAt = performance.now();
      await waitFor(async () => healthy(await systemStatus()), { timeoutMs: RECOVERY_TIMEOUT_MS, intervalMs: POLL_MS, description: "every service up again" });
      const recoverMs = performance.now() - restoredAt;
      await sleep(2500);
      const tickBack = parseTick((await systemStatus()).hub.tick);
      const nextSnapshot = (tickBack / SNAPSHOT_EVERY + 1n) * SNAPSHOT_EVERY;
      await sampler.waitFor((system) => parseTick(system.hub?.tick) >= nextSnapshot + 8n, { timeoutMs: 30_000, description: `the tick to pass the snapshot ${nextSnapshot}` });

      // --- Prueba de punta a punta: se abate world y se mira de qué tick se
      // recupera. Si la instantánea posterior a la caída no se hubiera
      // guardado, sería anterior al fallo en más de 50 ticks.
      const bootBefore = (await waitHubIdentity()).boot_id;
      await killService("world");
      await stopService("world");
      const paused = await sampler.waitFor((system) => serviceStatus(system, "world").status === "down" && system.hub?.paused, { timeoutMs: 20_000, description: "world down and the valley paused" });
      await sleep(POLL_MS + 200);
      const tickAtFailure = parseTick((await systemStatus()).hub.tick);
      assert.equal(paused.system.hub.paused, true);
      await startService("world");
      const back = await waitFor(async () => {
        const system = await systemStatus();
        return healthy(system) && system.hub.boot_id !== bootBefore ? system.hub : false;
      }, { timeoutMs: RECOVERY_TIMEOUT_MS, intervalMs: POLL_MS, description: "world back with a new boot_id" });
      const restored = parseTick(back.restored_tick);
      assert.notEqual(restored, null, back.restored_tick);
      assert.deepEqual(restoredTickViolations({ restored, tickAtFailure, every: SNAPSHOT_EVERY }), [], `restored_tick ${restored}, tick at the failure ${tickAtFailure}`);
      assert.ok(restored >= nextSnapshot, `restored_tick ${restored} must be a snapshot saved after the outage (first one: ${nextSnapshot}; outage ticks ${firstTick}..${lastTick})`);

      console.log(
        formatMeasurement(`CP-12 persistence-stop ${OUTAGE_MS / 1000}s`, {
          outage_ticks: `${firstTick}..${lastTick}`,
          snapshot_ticks_crossed: crossed,
          ticks_per_s: ticksPerSecond,
          step_ms_last_max: stepLastMax,
          step_ms_p95_max: stepP95Max,
          step_failures: 0,
          recover_s: recoverMs / 1000,
          first_snapshot_after: nextSnapshot,
          tick_at_world_failure: tickAtFailure,
          restored_tick: restored,
        }),
      );
    } finally {
      await sampler.stop();
      await restoreServices("persistence", "world");
      await waitHealthy().catch(() => {});
    }
  });
});

// RF-15: Si el servicio de chat se cae o congela, el juego (/play) continúa
// sin interrupción ni latencia apreciable.
//
// Invariantes verificadas:
// 1. Con el servicio chat detenido (`docker compose stop chat`):
//    - Las acciones de /play (gotas) se aplican inmediatamente (< 500 ms).
//    - El simulador del valle continúa avanzando ticks con normalidad.
// 2. Con el servicio chat congelado (`docker compose pause chat`):
//    - Las acciones de /play no sufren bloqueo por llamadas o dependencias síncronas.
// 3. Tras restablecer el servicio (`docker compose unpause / start`):
//    - El chat se recupera y permite enviar y recibir mensajes con normalidad.
import { describe, it, before, after } from "node:test";
import assert from "node:assert/strict";
import { randomUUID } from "node:crypto";
import { ChatSession } from "./chat-session.mjs";
import {
  SKIP,
  OUTAGE_MS,
  waitHealthy,
  openSession,
} from "./faults-support.mjs";
import {
  sleep,
  waitFor,
  parseTick,
  formatMeasurement,
  stopService,
  startService,
  pauseService,
  unpauseService,
  loginAdmin,
} from "./faults.mjs";

async function restoreChat() {
  await unpauseService("chat").catch(() => {});
  await startService("chat").catch(() => {});
}

describe("chat resilience and decoupling (RF-15)", { skip: SKIP }, () => {
  let adminToken;
  let play;

  before(async () => {
    await waitHealthy();
    adminToken = await loginAdmin();
    play = await openSession(adminToken);
    // Asegurar modo demo activo para gotas sin coste
    play.send({ type: "set_demo_mode", action_id: randomUUID(), enabled: true });
    await play.first("demo mode enabled", (f) => f.type === "state" && f.demo_mode === true, 10_000);
  });

  after(async () => {
    try {
      play?.close();
    } catch {}
    await restoreChat();
  });

  it(
    "mantiene el juego y la simulación operativos cuando el chat está detenido o congelado",
    { timeout: 90_000 },
    async () => {
      // 1. Línea base: enviar una gota con el stack normal
      const baseActionId = randomUUID();
      const baseStart = performance.now();
      play.send({ type: "drop", action_id: baseActionId, x: 2, y: 2 });
      const baseResult = await play.first("baseline drop result", (f) => f.type === "result" && f.action_id === baseActionId, 5000);
      const baseMs = performance.now() - baseStart;
      assert.equal(baseResult.status, "applied");

      // 2. Detener el contenedor de chat (proceso caído)
      await stopService("chat");

      const stoppedActionId = randomUUID();
      const stoppedStart = performance.now();
      play.send({ type: "drop", action_id: stoppedActionId, x: 3, y: 3 });
      const stoppedResult = await play.first("drop while chat is stopped", (f) => f.type === "result" && f.action_id === stoppedActionId, 5000);
      const stoppedMs = performance.now() - stoppedStart;

      assert.equal(stoppedResult.status, "applied", "La gota debe aplicarse con éxito aunque el chat esté caído");
      assert.ok(stoppedMs < 500, `Latencia de gota con chat caído (${stoppedMs.toFixed(1)} ms) debe ser < 500 ms`);

      // Verificar que los ticks siguen avanzando en /play
      const state1 = await play.waitFor("state frame 1", (f) => f.type === "state", 5000);
      await sleep(1500);
      const state2 = await play.waitFor("state frame 2", (f) => f.type === "state", 5000);
      assert.ok(
        parseTick(state2.tick) > parseTick(state1.tick),
        "El simulador del valle debe seguir avanzando ticks mientras el chat está caído",
      );

      // 3. Pausar el contenedor de chat (máquina congelada / sin respuesta TCP)
      await startService("chat");
      await sleep(2000);
      await pauseService("chat");

      const pausedActionId = randomUUID();
      const pausedStart = performance.now();
      play.send({ type: "drop", action_id: pausedActionId, x: 4, y: 4 });
      const pausedResult = await play.first("drop while chat is paused", (f) => f.type === "result" && f.action_id === pausedActionId, 5000);
      const pausedMs = performance.now() - pausedStart;

      assert.equal(pausedResult.status, "applied", "La gota debe aplicarse con éxito aunque el chat esté congelado");
      assert.ok(pausedMs < 500, `Latencia de gota con chat congelado (${pausedMs.toFixed(1)} ms) debe ser < 500 ms`);

      // Descongelar el contenedor de chat
      await unpauseService("chat");

      // 4. Esperar recuperación del chat y validar funcionamiento normal
      const chatRecovered = await waitFor(
        async () => {
          try {
            const cs = new ChatSession(`${process.env.HV_BASE_URL?.replace(/^http/, "ws") ?? "wss://localhost:8080"}/chat?token=${adminToken}`);
            await cs.opened;
            const testId = randomUUID();
            cs.sendMessage("resilience-recovery-test", testId);
            const msg = await cs.first("recovery chat message", (f) => f.type === "message" && f.message_id === testId, 5000);
            cs.close();
            return Boolean(msg);
          } catch {
            return false;
          }
        },
        { timeoutMs: 30_000, intervalMs: 1500, description: "chat service recovery after unpause" },
      );

      assert.ok(chatRecovered, "El servicio de chat debe recuperarse completamente tras restablecer el contenedor");

      console.log(
        formatMeasurement("RF-15 (chat outage)", {
          baseline_ms: baseMs,
          chat_stopped_ms: stoppedMs,
          chat_paused_ms: pausedMs,
        }),
      );
    },
  );
});

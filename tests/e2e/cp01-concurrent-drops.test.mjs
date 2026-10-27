// CP-01: 100 acciones simultáneas de varios jugadores (RF-08).
//
// En modo demo (la gota es gratis), 8 jugadores sueltan 100 gotas en total
// sobre 100 casillas distintas. Cada jugador manda su parte (12 o 13) de una
// sola ráfaga, sin esperar resultados, y todos a la vez: es la concurrencia
// máxima que admite el límite de 15 gotas por segundo y por jugador (RF-09)
// sin que el limitador intervenga. Invariantes:
//   1. Las 100 acciones terminan en `applied` (ninguna `rate_limited`).
//   2. Δ humedad aportada == Δ gotas aceptadas del hub == 100, y se mantiene
//      tras una espera (si se contara de más, aparecería).
//   3. El depósito de cada jugador sigue lleno (la gota es gratis).
//
// Los jugadores salen de POST /api/admin/bot-accounts (no del registro
// público, limitado a 5 por 10 minutos). run.sh lo ejecuta en su propia
// invocación de `node --test` después del humo.
import { describe, it, before } from "node:test";
import assert from "node:assert/strict";
import { randomUUID } from "node:crypto";
import {
  loginAdmin,
  openReadySession,
  provisionPlayers,
  sleep,
  waitForHealthyStack,
} from "./support.mjs";

const PLAYERS = 8;
const TOTAL_DROPS = 100;
// El hub difunde el estado una vez por segundo: la espera de asentamiento
// cubre al menos dos difusiones.
const SETTLE_MS = 2500;
const RESULT_TIMEOUT_MS = 30_000;

const counter = (frame, field) => BigInt(frame[field] ?? "0");

describe("concurrent drops (CP-01 / RF-08)", () => {
  before(async () => {
    await waitForHealthyStack();
  });

  it("aplica 100 gotas simultáneas de 8 jugadores en modo demo sin perder ni contar de más", { timeout: 90_000 }, async () => {
    const adminToken = await loginAdmin();
    const admin = await openReadySession(adminToken, { map: false });
    const sessions = [];
    try {
      admin.send({ type: "set_demo_mode", action_id: randomUUID(), enabled: true });
      await admin.first("demo mode enabled", (f) => f.type === "state" && f.demo_mode === true, 15_000);

      const accounts = await provisionPlayers(adminToken, PLAYERS);
      for (const account of accounts) {
        sessions.push(await openReadySession(account.token, { map: false }));
      }

      // Línea base: dos estados en modo demo con 1,5 s de por medio, para ver
      // que nada más está soltando gotas. Los jugadores entran al mundo con su
      // primer comando, así que su depósito se compara con la capacidad.
      const demoState = (f) => f.type === "state" && f.demo_mode === true;
      const earlier = await admin.waitFor("a demo-mode state", demoState, 10_000);
      await sleep(1500);
      const baseline = await admin.waitFor("a second demo-mode state", demoState, 10_000);
      const humidity0 = counter(baseline, "humidity_contributed");
      const accepted0 = counter(baseline, "accepted_drops");
      assert.equal(accepted0, counter(earlier, "accepted_drops"), "hay otras gotas en curso: la línea base no es estable");

      // 100 gotas repartidas en 8 jugadores: 4 mandan 13 y 4 mandan 12.
      const plan = sessions.map((_, i) => (i < TOTAL_DROPS % PLAYERS ? Math.ceil(TOTAL_DROPS / PLAYERS) : Math.floor(TOTAL_DROPS / PLAYERS)));
      assert.equal(plan.reduce((a, b) => a + b, 0), TOTAL_DROPS);
      // Una casilla distinta por gota, en la mitad izquierda del valle y lejos de
      // las esquinas: el río y las rocas de la expansión de jugabilidad caen en
      // las columnas 17 en adelante, y los edificios (y su basura) en el borde;
      // todos rechazarían la gota.
      let tile = 0;
      const actions = plan.map((n) =>
        Array.from({ length: n }, () => {
          const t = tile++;
          return { id: randomUUID(), x: t % 16, y: 8 + Math.floor(t / 16) };
        }),
      );

      const startedAt = Date.now();
      // Todos a la vez y de una ráfaga: sin esperas entre envíos.
      await Promise.all(
        sessions.map(async (session, i) => {
          for (const a of actions[i]) session.send({ type: "drop", action_id: a.id, x: a.x, y: a.y });
        }),
      );
      const sendMs = Date.now() - startedAt;

      const results = await Promise.all(
        sessions.map(async (session, i) => {
          const tally = { applied: 0, other: [] };
          for (const a of actions[i]) {
            const r = await session.first(`result of ${a.id}`, (f) => f.type === "result" && f.action_id === a.id, RESULT_TIMEOUT_MS);
            if (r.status === "applied") tally.applied++;
            else tally.other.push(r.status);
          }
          return tally;
        }),
      );
      const applied = results.reduce((sum, r) => sum + r.applied, 0);
      const others = results.flatMap((r) => r.other);

      // Estado posterior a las gotas: solo cuentan los mensajes que llegan
      // después de este punto, no los que ya estaban en el búfer.
      const fromIndex = admin.frames.length;
      const target = BigInt(TOTAL_DROPS);
      await admin.waitFor(
        "counters to reach the target",
        (f) => f.type === "state" && counter(f, "accepted_drops") - accepted0 >= target && counter(f, "humidity_contributed") - humidity0 >= target,
        20_000,
        { fromIndex },
      );
      // Asentamiento: si se contara de más, el exceso aparecería en el estado siguiente.
      const settledFrom = admin.frames.length;
      await sleep(SETTLE_MS);
      const final = admin.frames.slice(settledFrom).filter((f) => f.type === "state").at(-1);
      assert.ok(final, "no llegó ningún estado tras la espera de asentamiento");

      const humidityDelta = counter(final, "humidity_contributed") - humidity0;
      const acceptedDelta = counter(final, "accepted_drops") - accepted0;

      const finalPlayers = await Promise.all(
        sessions.map((s) => s.waitFor("a final state with the player", (f) => f.type === "state" && f.player, 5000, { fromIndex: settledFrom })),
      );
      const waterChanged = finalPlayers.filter((f) => f.player.water !== f.player.tank_capacity).length;

      console.log(
        `MEDICION CP-01: jugadores=${PLAYERS} enviadas=${TOTAL_DROPS} aplicadas=${applied} otros_resultados=${others.length} delta_humedad=${humidityDelta} delta_gotas_aceptadas=${acceptedDelta} jugadores_con_agua_cambiada=${waterChanged} envio_ms=${sendMs}`,
      );

      assert.deepEqual(others, [], "ninguna gota debe terminar en otro resultado que applied (ni rate_limited)");
      assert.equal(applied, TOTAL_DROPS, "las 100 gotas terminan applied");
      assert.equal(humidityDelta, BigInt(TOTAL_DROPS), "Δ humedad aportada == 100");
      assert.equal(acceptedDelta, BigInt(TOTAL_DROPS), "Δ gotas aceptadas == 100");
      assert.equal(waterChanged, 0, "en modo demo el depósito no cambia");
    } finally {
      for (const s of sessions) s.close();
      // Pase lo que pase, el modo demo se apaga para no contaminar otros casos.
      admin.send({ type: "set_demo_mode", action_id: randomUUID(), enabled: false });
      await admin.waitFor("demo mode disabled", (f) => f.type === "state" && f.demo_mode === false, 10_000).catch(() => {});
      admin.close();
    }
  });
});

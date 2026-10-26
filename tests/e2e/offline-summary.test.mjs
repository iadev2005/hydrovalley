// Resumen al volver (B6, RF-25): lo que hicieron los aspersores del jugador
// mientras no estaba llega en el primer `state` de su siguiente conexión y se
// borra con su siguiente acción aplicada.
//
// Corre contra el stack por defecto (catálogo de la imagen), sin el modo demo:
//   1. Un jugador hace crecer una casilla hasta árbol con 18 gotas (1 + 2 + 4 +
//      8 = 15 hojas) y compra un aspersor (10 hojas) junto a ella.
//   2. Se desconecta y espera 14 s: el aspersor riega cada 2 s y a la quinta
//      vez (10 s) las ocho casillas vecinas pasan a pasto.
//   3. Al reconectarse, el primer `state` con su vista trae `offline_summary`
//      con agua reutilizada y casillas crecidas (u64 como texto, regla 7).
//   4. Suelta una gota (acción aplicada) y se reconecta enseguida: el resumen
//      empezó de cero, así que no trae casillas crecidas y el agua es menor.
// La imagen y catalog/catalog.json no se tocan. Con `tests/e2e/run.sh` corre
// en la fase de las pruebas de fallos; sirve con cualquier simulador
// (E2E_WORLD_IMPL=rust|go).

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

// Esquina del valle poco usada por las demás pruebas.
const TREE = { x: 28, y: 28 };
const SPRINKLER = { x: 29, y: 28 };
const AWAY_MS = 14_000;

async function act(session, message) {
  const actionId = randomUUID();
  session.send({ ...message, action_id: actionId });
  const result = await session.first(
    `the result of ${actionId}`,
    (f) => f.type === "result" && f.action_id === actionId,
    10_000,
  );
  assert.equal(result.status, "applied", JSON.stringify(result));
}

/** El primer `state` que lleva la vista del jugador: el que puede traer el resumen. */
function firstPlayerState(session) {
  return session.first("a state with the player view", (f) => f.type === "state" && f.player, 10_000);
}

describe("offline summary (B6, RF-25)", () => {
  before(async () => {
    await waitForHealthyStack();
  });

  it("brings what the sprinkler did while away and clears it with the next action", { timeout: 120_000 }, async () => {
    const [account] = await provisionPlayers(await loginAdmin(), 1, "e2e-summary");

    // 1. Hojas para un aspersor: una casilla hasta árbol (15 hojas).
    const first = await openReadySession(account.token, { map: false });
    for (let i = 0; i < 18; i++) {
      await act(first, { type: "drop", ...TREE });
    }
    await first.waitFor("the leaves of the tree", (f) => f.type === "state" && BigInt(f.player?.leaves ?? 0) >= 10n, 10_000);
    await act(first, { type: "place_installation", installation: "sprinkler", ...SPRINKLER });
    const placed = await firstPlayerState(first);
    assert.equal(placed.player.offline_summary, undefined, "a new player has nothing to catch up on");
    first.close();

    // 2. Ausente mientras el aspersor trabaja.
    await sleep(AWAY_MS);

    // 3. El primer estado de la nueva conexión trae el resumen.
    const returned = await openReadySession(account.token, { map: false });
    const firstFrame = await firstPlayerState(returned);
    const summary = firstFrame.player.offline_summary;
    assert.ok(summary, "the first state after returning must carry the summary");
    for (const key of ["water_reused", "tiles_grown"]) {
      assert.equal(typeof summary[key], "string", `${key} travels as text (rule 7)`);
      assert.ok(BigInt(summary[key]) > 0n, `${key} must be above zero: ${JSON.stringify(summary)}`);
    }
    // Nueve casillas de 25 de humedad por riego, siete riegos como mínimo.
    assert.ok(BigInt(summary.water_reused) >= 7n * 9n * 25n, JSON.stringify(summary));

    // Los estados siguientes de la misma conexión no lo repiten.
    const later = await returned.waitFor(
      "a later state",
      (f) => f.type === "state" && f.player,
      10_000,
      { fromIndex: returned.frames.indexOf(firstFrame) + 1 },
    );
    assert.equal(later.player.offline_summary, undefined);

    // 4. Una acción aplicada lo borra: el resumen de la siguiente conexión
    // cuenta solo lo que pasó desde esa acción.
    await act(returned, { type: "drop", x: 0, y: 0 });
    returned.close();
    const again = await openReadySession(account.token, { map: false });
    const next = (await firstPlayerState(again)).player.offline_summary;
    again.close();
    assert.equal(BigInt(next?.tiles_grown ?? 0), 0n, `the action must restart the summary: ${JSON.stringify(next)}`);
    assert.ok(
      BigInt(next?.water_reused ?? 0) < BigInt(summary.water_reused),
      `the summary after the action (${JSON.stringify(next)}) must be smaller than before (${JSON.stringify(summary)})`,
    );

    console.log(`MEDICION B6: tiles_grown=${summary.tiles_grown} water_reused=${summary.water_reused} leaves_earned=${summary.leaves_earned ?? "0"}`);
  });
});

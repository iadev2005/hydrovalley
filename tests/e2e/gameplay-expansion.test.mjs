// Expansión de jugabilidad (nivel 2, RF-32 a RF-34), de punta a punta: terreno
// natural, una gota por etapa, cesto de reciclaje con su durabilidad, edificios
// que ensucian y limpieza de la basura con su recompensa.
//
// Corre contra el stack por defecto (catalog/catalog.json sin cambios), con el
// modo demo apagado. Sirve con cualquier simulador (E2E_WORLD_IMPL=rust|go).
// El río y las rocas caen siempre desde la columna 17, así que las casillas
// «libres» de la prueba se buscan en la mitad izquierda, lejos de las esquinas
// donde aparecen los edificios.

import { describe, it, before, after } from "node:test";
import assert from "node:assert/strict";
import { randomUUID } from "node:crypto";
import { loginAdmin, openReadySession, provisionPlayers, waitForHealthyStack } from "./support.mjs";

const STAGE_DRY = 1;
const STAGE_WATER = 6;
const STAGE_ROCK = 7;

const index = (state, x, y) => y * state.width + x;

/** Envía una acción y devuelve su resultado final. */
async function send(session, message) {
  const actionId = randomUUID();
  session.send({ ...message, action_id: actionId });
  return session.first(
    `the result of ${message.type} ${actionId}`,
    (f) => f.type === "result" && f.action_id === actionId,
    10_000,
  );
}

async function sendApplied(session, message) {
  const result = await send(session, message);
  assert.equal(result.status, "applied", `${message.type}: ${JSON.stringify(result)}`);
}

/** Una casilla de tierra seca sin nada encima en la mitad izquierda, lejos de las esquinas. */
function freeTile(state, skip = []) {
  for (let y = 8; y < 24; y += 1) {
    for (let x = 6; x < 15; x += 1) {
      const i = index(state, x, y);
      const taken = skip.some((t) => Math.abs(t.x - x) <= 2 && Math.abs(t.y - y) <= 2);
      if (
        !taken &&
        state.stages[i] === STAGE_DRY &&
        state.humidity[i] === "0" &&
        !(state.buildings?.[i] ?? false) &&
        (state.trash?.[i] ?? 0) === 0 &&
        !state.installations?.some((inst) => inst.x === x && inst.y === y)
      ) {
        return { x, y };
      }
    }
  }
  throw new Error("no hay una casilla libre en la mitad izquierda del valle");
}

const isMapState = (f) => f.type === "state" && f.stages?.length === 32 * 32;

describe("gameplay expansion (RF-32, RF-33, RF-34)", () => {
  let admin;
  let player;
  const used = [];

  before(async () => {
    await waitForHealthyStack();
    const adminToken = await loginAdmin();
    admin = await openReadySession(adminToken, { map: false });
    // Sin modo demo no aparecen edificios ni basura.
    await sendApplied(admin, { type: "set_demo_mode", enabled: false });
    const [account] = await provisionPlayers(adminToken, 1, "e2e-expansion");
    player = await openReadySession(account.token, { map: true });
  });

  after(() => {
    player?.close();
    admin?.close();
  });

  it("has a river and rocks that refuse drops and installations", async () => {
    const state = await player.first("a state with the map", isMapState, 10_000);
    const natural = state.stages
      .map((stage, i) => ({ stage, x: i % state.width, y: Math.floor(i / state.width) }))
      .filter((tile) => tile.stage === STAGE_WATER || tile.stage === STAGE_ROCK);
    const water = natural.filter((tile) => tile.stage === STAGE_WATER);
    const rocks = natural.filter((tile) => tile.stage === STAGE_ROCK);
    assert.ok(water.length >= 32, `el río debe cruzar el valle: ${water.length} casillas de agua`);
    assert.ok(rocks.length >= 1, "debe haber rocas");
    assert.ok(natural.every((tile) => tile.x >= 17), "el terreno natural cae desde la columna 17");

    const dropOnWater = await send(player, { type: "drop", x: water[0].x, y: water[0].y });
    assert.deepEqual([dropOnWater.status, dropOnWater.reason], ["rejected", "tile_occupied"], JSON.stringify(dropOnWater));
    const placeOnRock = await send(player, {
      type: "place_installation",
      installation: "sprinkler",
      x: rocks[0].x,
      y: rocks[0].y,
    });
    assert.deepEqual([placeOnRock.status, placeOnRock.reason], ["rejected", "tile_occupied"], JSON.stringify(placeOnRock));
  });

  it("rejects cleaning a tile without trash and a position outside the valley", async () => {
    const state = await player.waitFor("a fresh state with the map", isMapState, 10_000);
    const tile = freeTile(state);
    const clean = await send(player, { type: "clean_trash", ...tile });
    assert.deepEqual([clean.status, clean.reason], ["rejected", "invalid_command"], JSON.stringify(clean));
    const outside = await send(player, { type: "clean_trash", x: 40, y: 3 });
    assert.deepEqual([outside.status, outside.reason], ["rejected", "out_of_bounds"], JSON.stringify(outside));
  });

  it("grows two tiles in four drops each (one per stage) and buys a trash bin with the 15 leaves", async () => {
    const state = await player.waitFor("a fresh state with the map", isMapState, 10_000);
    const tile = freeTile(state);
    used.push(tile);
    const second = freeTile(state, used);
    used.push(second);
    // El depósito del catálogo real (10 L) alcanza justo para las ocho gotas.
    for (const target of [tile, second]) {
      for (let i = 0; i < 4; i += 1) await sendApplied(player, { type: "drop", ...target });
    }
    const grown = await player.waitFor(
      "the two tiles as trees and the 22 leaves",
      (f) =>
        isMapState(f) &&
        f.stages[index(f, tile.x, tile.y)] === 5 &&
        f.stages[index(f, second.x, second.y)] === 5 &&
        BigInt(f.player?.leaves ?? "0") >= 22n,
      10_000,
    );
    assert.equal(BigInt(grown.player.leaves), 22n, "2 × (1 + 2 + 3 + 5) hojas");

    const binTile = { x: tile.x + 1, y: tile.y };
    await sendApplied(player, { type: "place_installation", installation: "trash_bin", ...binTile });
    const placed = await player.waitFor(
      "the trash bin with its durability",
      (f) => isMapState(f) && f.installations?.some((i) => i.installation === "trash_bin" && i.x === binTile.x && i.y === binTile.y),
      10_000,
    );
    const bin = placed.installations.find((i) => i.installation === "trash_bin" && i.x === binTile.x && i.y === binTile.y);
    const durability = Number(bin.remaining_durability ?? 0);
    assert.ok(durability >= 1 && durability <= 30, `durabilidad ${durability}: empieza en 30 y baja con cada limpieza`);
    // Cada compra del mismo tipo sube el precio (installation_price_increase_percent, redondeo hacia arriba).
    assert.ok(BigInt(placed.player.next_trash_bin_price) > 15n, JSON.stringify(placed.player));
  });

  it("makes buildings appear, dirty their neighbour and pays the leaves for cleaning it", { timeout: 120_000 }, async () => {
    // Humedad aportada de sobra (500) para que aparezca el primer edificio.
    const state = await player.waitFor("a fresh state with the map", isMapState, 10_000);
    const tile = freeTile(state, used);
    used.push(tile);
    // Las gotas anteriores vaciaron casi todo el depósito: se espera a la recarga.
    await player.waitFor("water for three drops", (f) => f.type === "state" && BigInt(f.player?.water ?? "0") >= 3n, 20_000);
    for (let i = 0; i < 3; i += 1) await sendApplied(player, { type: "drop", ...tile });

    const dirty = await player.waitFor(
      "a building and some trash next to it",
      (f) => isMapState(f) && f.buildings?.some(Boolean) && f.trash?.some((level) => level > 0),
      60_000,
    );
    const at = dirty.trash.findIndex((level) => level > 0);
    const target = { x: at % dirty.width, y: Math.floor(at / dirty.width) };

    const leavesBefore = BigInt(dirty.player?.leaves ?? "0");
    // Hasta dejarla limpia: cada acción quita un nivel (la basura nueva llega cada trash_interval_seconds).
    let cleaned = false;
    for (let i = 0; i < 12 && !cleaned; i += 1) {
      const result = await send(player, { type: "clean_trash", ...target });
      if (result.status === "rejected") {
        assert.equal(result.reason, "invalid_command", JSON.stringify(result));
        cleaned = true; // ya estaba limpia: otra limpieza (o un cesto) llegó antes
      } else {
        assert.equal(result.status, "applied", JSON.stringify(result));
        const now = await player.waitFor("a state after cleaning", isMapState, 5_000);
        cleaned = (now.trash[index(now, target.x, target.y)] ?? 0) === 0;
      }
    }
    assert.ok(cleaned, "la casilla debe quedar limpia");
    const paid = await player.waitFor(
      "the reward for cleaning",
      (f) => f.type === "state" && BigInt(f.player?.leaves ?? "0") >= leavesBefore + 1n,
      10_000,
    );
    assert.ok(BigInt(paid.player.leaves) >= leavesBefore + 1n);
  });
});

import { describe, it } from "node:test";
import assert from "node:assert/strict";
import {
  buildTiles,
  createPendingActions,
  describeActionResult,
  PENDING_MAX_AGE_MS,
  planResend,
  selectionView,
  isAdminRole,
  playerPanel,
  stageName,
  tileAt,
  tokenRole,
} from "../js/valley.js";

const b64url = (obj) =>
  Buffer.from(JSON.stringify(obj)).toString("base64").replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");

describe("tokenRole / isAdminRole", () => {
  it("reads the role from the token payload", () => {
    const token = `${b64url({ alg: "EdDSA" })}.${b64url({ player_id: "p", username: "u", role: "admin", exp: 1 })}.sig`;
    assert.equal(tokenRole(token), "admin");
    assert.equal(isAdminRole(tokenRole(token)), true);
  });

  it("returns an empty role for malformed tokens and never counts them as admin", () => {
    for (const token of ["", "abc", "a.b.c", "a..c", undefined, null, 5]) {
      assert.equal(tokenRole(token), "", String(token));
    }
    const player = `x.${b64url({ role: "player" })}.y`;
    assert.equal(isAdminRole(tokenRole(player)), false);
    assert.equal(isAdminRole(""), false);
  });
});

describe("stageName", () => {
  it("names the five stages and falls back to dry land", () => {
    assert.deepEqual([1, 2, 3, 4, 5].map(stageName), ["tierra seca", "pasto", "arbusto", "flores", "árbol"]);
    assert.equal(stageName(0), "tierra seca");
    assert.equal(stageName(99), "tierra seca");
  });
});

describe("buildTiles", () => {
  const state = {
    width: 2,
    height: 2,
    stages: [1, 2, 3, 5],
    humidity: [0n, 75n, 10n, 0n],
    fires: [{ x: 1, y: 0, drops_received: 2 }],
    installations: [{ x: 0, y: 1, installation: "sprinkler", owner_id: "p1" }],
  };

  it("joins stage, humidity, fire and installation per tile, row by row", () => {
    const tiles = buildTiles(state);
    assert.equal(tiles.length, 4);
    assert.deepEqual(tiles[1], {
      x: 1,
      y: 0,
      stage: 2,
      humidity: 75n,
      fire: { drops_received: 2 },
      installation: null,
    });
    assert.deepEqual(tiles[2].installation, { kind: "sprinkler", owner_id: "p1" });
    assert.equal(tiles[2].fire, null);
    assert.equal(tiles[3].stage, 5);
  });

  it("returns no tiles for a state without map (map=false)", () => {
    assert.deepEqual(buildTiles({ width: 32, height: 32, stages: [], humidity: [], fires: [], installations: [] }), []);
    assert.deepEqual(buildTiles(null), []);
  });

  it("ignores fires and installations outside the map", () => {
    const tiles = buildTiles({ ...state, fires: [{ x: 9, y: 9, drops_received: 1 }], installations: [] });
    assert.equal(tiles.filter((t) => t.fire).length, 0);
  });
});

describe("tileAt", () => {
  it("maps pixels to tile coordinates and rejects the outside", () => {
    assert.deepEqual(tileAt(0, 0, 16, 32), { x: 0, y: 0 });
    assert.deepEqual(tileAt(31, 17, 16, 32), { x: 1, y: 1 });
    assert.deepEqual(tileAt(511, 511, 16, 32), { x: 31, y: 31 });
    assert.equal(tileAt(512, 0, 16, 32), null);
    assert.equal(tileAt(-1, 0, 16, 32), null);
    assert.equal(tileAt(Number.NaN, 0, 16, 32), null);
  });
});

describe("playerPanel", () => {
  it("formats water, leaves, prices and owned upgrades", () => {
    const panel = playerPanel({
      water: 5n,
      tank_capacity: 20n,
      leaves: 3n,
      upgrades: ["bigger_drop"],
      next_sprinkler_price: 10n,
      next_drip_price: 25n,
      next_treatment_plant_price: 40n,
    });
    assert.deepEqual(panel, {
      water: "5 / 20",
      waterRatio: 0.25,
      leaves: "3",
      prices: { sprinkler: "10", drip: "25", treatment_plant: "40" },
      owned: { bigger_drop: true, bigger_tank: false },
    });
  });

  it("shows placeholders before the player joins the world", () => {
    const panel = playerPanel(null);
    assert.equal(panel.water, "—");
    assert.equal(panel.waterRatio, 0);
    assert.equal(panel.leaves, "—");
    assert.equal(panel.prices.drip, "—");
    assert.deepEqual(panel.owned, { bigger_drop: false, bigger_tank: false });
  });

  it("does not divide by a zero capacity", () => {
    assert.equal(playerPanel({ water: 0n, tank_capacity: 0n, leaves: 0n, upgrades: [] }).waterRatio, 0);
  });
});

describe("describeActionResult", () => {
  it("treats applied and duplicate as success", () => {
    assert.deepEqual(describeActionResult({ status: "applied", reason: "" }), { ok: true, text: "aplicada" });
    assert.deepEqual(describeActionResult({ status: "duplicate", reason: "" }), { ok: true, text: "aplicada" });
  });

  it("explains each rejection reason", () => {
    const text = (reason) => describeActionResult({ status: "rejected", reason }).text;
    assert.match(text("insufficient_water"), /agua/);
    assert.match(text("insufficient_leaves"), /hojas/);
    assert.match(text("out_of_bounds"), /fuera del valle/);
    assert.match(text("tile_occupied"), /ocupada/);
    assert.match(text("upgrade_already_owned"), /mejora/);
    assert.match(text("something_new"), /something_new/);
    assert.equal(describeActionResult({ status: "rejected", reason: "out_of_bounds" }).ok, false);
  });

  it("explains the immediate rejections and expiry", () => {
    for (const [status, pattern] of [
      ["expired", /caducó/],
      ["rate_limited", /demasiadas gotas/],
      ["paused", /pausa/],
      ["forbidden", /administrador/],
    ]) {
      const result = describeActionResult({ status, reason: "" });
      assert.equal(result.ok, false, status);
      assert.match(result.text, pattern, status);
    }
  });

  it("shows an unrecognised status verbatim", () => {
    assert.equal(describeActionResult({ status: "weird", reason: "" }).text, "resultado desconocido (weird)");
  });
});

describe("createPendingActions", () => {
  const frame = (id) => JSON.stringify({ type: "drop", action_id: id, x: 1, y: 1 });

  it("keeps an action pending until its final result, then clears it", () => {
    const pending = createPendingActions();
    pending.add("a", frame("a"), "Soltar gota");
    assert.equal(pending.size, 1);
    assert.equal(pending.has("a"), true);
    const entry = pending.resolve({ action_id: "a", status: "applied" });
    assert.equal(entry.label, "Soltar gota");
    assert.equal(entry.frame, frame("a"));
    assert.equal(pending.size, 0);
  });

  it("clears on every final status, including duplicate and the immediate rejections", () => {
    for (const status of ["applied", "duplicate", "rejected", "expired", "rate_limited", "paused", "forbidden"]) {
      const pending = createPendingActions();
      pending.add("a", frame("a"), "x");
      assert.notEqual(pending.resolve({ action_id: "a", status }), null, status);
      assert.equal(pending.size, 0, status);
    }
  });

  it("ignores results for unknown actions and keeps others pending", () => {
    const pending = createPendingActions();
    pending.add("a", frame("a"), "x");
    assert.equal(pending.resolve({ action_id: "zzz", status: "applied" }), null);
    assert.equal(pending.resolve({ action_id: "a", status: "mystery" }), null);
    assert.equal(pending.size, 1);
  });

  it("settles an action by id (error frames, expiry) and ignores unknown ids", () => {
    const pending = createPendingActions();
    pending.add("a", frame("a"), "x");
    assert.notEqual(pending.settle("a"), null);
    assert.equal(pending.size, 0);
    assert.equal(pending.settle("a"), null);
    assert.equal(pending.settle(""), null);
  });
});

describe("planResend", () => {
  const entry = (id, type, addedAt) => ({
    actionId: id,
    label: id,
    frame: JSON.stringify({ type, action_id: id }),
    type,
    addedAt,
  });

  it("uses the hub's 30 s queue expiry as the age limit", () => {
    assert.equal(PENDING_MAX_AGE_MS, 30_000);
  });

  it("resends recent player commands in order and drops the old and the admin ones", () => {
    const now = 100_000;
    const entries = [
      entry("a", "drop", now - 1_000),
      entry("old", "drop", now - PENDING_MAX_AGE_MS - 1),
      entry("demo", "set_demo_mode", now - 1_000),
      entry("b", "place_installation", now - 5_000),
      entry("reset", "reset_valley", now - 1_000),
      entry("edge", "buy_upgrade", now - PENDING_MAX_AGE_MS),
    ];
    const plan = planResend(entries, now);
    assert.deepEqual(plan.resend.map((e) => e.actionId), ["a", "b", "edge"]);
    assert.deepEqual(plan.expired.map((e) => e.actionId), ["old"]);
    assert.deepEqual(plan.adminDropped.map((e) => e.actionId), ["demo", "reset"]);
  });

  it("counts an old admin command as expired, not as admin-dropped", () => {
    const plan = planResend([entry("r", "reset_valley", 0)], PENDING_MAX_AGE_MS + 1);
    assert.deepEqual(plan.expired.map((e) => e.actionId), ["r"]);
    assert.equal(plan.adminDropped.length, 0);
  });

  it("returns empty lists for nothing pending", () => {
    assert.deepEqual(planResend([], 0), { resend: [], expired: [], adminDropped: [] });
  });
});

describe("pending actions on reconnect", () => {
  it("removes expired and admin entries and keeps the resent ones pending", () => {
    let clock = 0;
    const pending = createPendingActions({ now: () => clock });
    const f = (type, id) => JSON.stringify({ type, action_id: id });
    pending.add("old", f("drop", "old"), "gota vieja");
    clock = 20_000;
    pending.add("reset", f("reset_valley", "reset"), "Reiniciar valle");
    pending.add("new", f("drop", "new"), "gota nueva");
    clock = 31_000;
    const plan = pending.planReconnect();
    assert.deepEqual(plan.resend.map((e) => e.actionId), ["new"]);
    assert.deepEqual(plan.expired.map((e) => e.actionId), ["old"]);
    assert.deepEqual(plan.adminDropped.map((e) => e.actionId), ["reset"]);
    assert.equal(pending.size, 1);
    assert.equal(pending.has("new"), true);
  });
});

describe("selectionView", () => {
  const state = {
    width: 2,
    height: 2,
    stages: [1, 2, 3, 5],
    humidity: [0n, 75n, 10n, 0n],
    fires: [{ x: 1, y: 0, drops_received: 2 }],
    installations: [{ x: 0, y: 1, installation: "sprinkler", owner_id: "p1" }],
  };

  it("clears the label and info when nothing is selected", () => {
    assert.deepEqual(selectionView(state, null), { label: "ninguna", info: "Haz clic en una casilla." });
    assert.deepEqual(selectionView(null, null), { label: "ninguna", info: "Haz clic en una casilla." });
  });

  it("describes the selected tile, indexing by the state's width", () => {
    const view = selectionView(state, { x: 1, y: 0 });
    assert.equal(view.label, "(1, 0)");
    assert.equal(view.info, "pasto, humedad 75, en llamas (2 gotas recibidas)");
    assert.equal(selectionView(state, { x: 0, y: 1 }).info, "arbusto, humedad 10, instalación: sprinkler");
  });

  it("says there is no map data when the state has no map", () => {
    assert.equal(selectionView(null, { x: 3, y: 3 }).info, "Sin datos del mapa todavía.");
  });
});

import { describe, it, expect } from "vitest";
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
} from "./valley.ts";

const b64url = (obj: unknown): string =>
  btoa(JSON.stringify(obj)).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");

describe("tokenRole / isAdminRole", () => {
  it("reads the role from the token payload", () => {
    const token = `${b64url({ alg: "EdDSA" })}.${b64url({ player_id: "p", username: "u", role: "admin", exp: 1 })}.sig`;
    expect(tokenRole(token)).toBe("admin");
    expect(isAdminRole(tokenRole(token))).toBe(true);
  });

  it("returns an empty role for malformed tokens and never counts them as admin", () => {
    for (const token of ["", "abc", "a.b.c", "a..c", undefined, null, 5]) {
      expect(tokenRole(token)).toBe("");
    }
    const player = `x.${b64url({ role: "player" })}.y`;
    expect(isAdminRole(tokenRole(player))).toBe(false);
    expect(isAdminRole("")).toBe(false);
  });
});

describe("stageName", () => {
  it("names the five stages and falls back to dry land", () => {
    expect([1, 2, 3, 4, 5].map(stageName)).toEqual(["tierra seca", "pasto", "arbusto", "flores", "árbol"]);
    expect(stageName(0)).toBe("tierra seca");
    expect(stageName(99)).toBe("tierra seca");
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
    expect(tiles.length).toBe(4);
    expect(tiles[1]).toEqual({
      x: 1,
      y: 0,
      stage: 2,
      humidity: 75n,
      fire: { drops_received: 2 },
      installation: null,
    });
    expect(tiles[2]?.installation).toEqual({ kind: "sprinkler", owner_id: "p1" });
    expect(tiles[2]?.fire).toBeNull();
    expect(tiles[3]?.stage).toBe(5);
  });

  it("returns no tiles for a state without map (map=false)", () => {
    expect(buildTiles({ width: 32, height: 32, stages: [], humidity: [], fires: [], installations: [] })).toEqual([]);
    expect(buildTiles(null)).toEqual([]);
  });

  it("ignores fires and installations outside the map", () => {
    const tiles = buildTiles({ ...state, fires: [{ x: 9, y: 9, drops_received: 1 }], installations: [] });
    expect(tiles.filter((t) => t.fire).length).toBe(0);
  });
});

describe("tileAt", () => {
  it("maps pixels to tile coordinates and rejects the outside", () => {
    expect(tileAt(0, 0, 16, 32)).toEqual({ x: 0, y: 0 });
    expect(tileAt(31, 17, 16, 32)).toEqual({ x: 1, y: 1 });
    expect(tileAt(511, 511, 16, 32)).toEqual({ x: 31, y: 31 });
    expect(tileAt(512, 0, 16, 32)).toBeNull();
    expect(tileAt(-1, 0, 16, 32)).toBeNull();
    expect(tileAt(Number.NaN, 0, 16, 32)).toBeNull();
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
    expect(panel).toEqual({
      water: "5 / 20",
      waterRatio: 0.25,
      leaves: "3",
      prices: { sprinkler: "10", drip: "25", treatment_plant: "40" },
      owned: { bigger_drop: true, bigger_tank: false },
    });
  });

  it("shows placeholders before the player joins the world", () => {
    const panel = playerPanel(null);
    expect(panel.water).toBe("—");
    expect(panel.waterRatio).toBe(0);
    expect(panel.leaves).toBe("—");
    expect(panel.prices.drip).toBe("—");
    expect(panel.owned).toEqual({ bigger_drop: false, bigger_tank: false });
  });

  it("does not divide by a zero capacity", () => {
    expect(playerPanel({ water: 0n, tank_capacity: 0n, leaves: 0n, upgrades: [] }).waterRatio).toBe(0);
  });
});

describe("describeActionResult", () => {
  it("treats applied and duplicate as success", () => {
    expect(describeActionResult({ status: "applied", reason: "" })).toEqual({ ok: true, text: "aplicada" });
    expect(describeActionResult({ status: "duplicate", reason: "" })).toEqual({ ok: true, text: "aplicada" });
  });

  it("explains each rejection reason", () => {
    const text = (reason: string) => describeActionResult({ status: "rejected", reason }).text;
    expect(text("insufficient_water")).toMatch(/agua/);
    expect(text("insufficient_leaves")).toMatch(/hojas/);
    expect(text("out_of_bounds")).toMatch(/fuera del valle/);
    expect(text("tile_occupied")).toMatch(/ocupada/);
    expect(text("upgrade_already_owned")).toMatch(/mejora/);
    expect(text("something_new")).toMatch(/something_new/);
    expect(describeActionResult({ status: "rejected", reason: "out_of_bounds" }).ok).toBe(false);
  });

  it("explains the immediate rejections and expiry", () => {
    for (const [status, pattern] of [
      ["expired", /caducó/],
      ["rate_limited", /demasiadas gotas/],
      ["paused", /pausa/],
      ["forbidden", /administrador/],
    ] as const) {
      const result = describeActionResult({ status, reason: "" });
      expect(result.ok).toBe(false);
      expect(result.text).toMatch(pattern);
    }
  });

  it("shows an unrecognised status verbatim", () => {
    expect(describeActionResult({ status: "weird", reason: "" }).text).toBe("resultado desconocido (weird)");
  });
});

describe("createPendingActions", () => {
  const frame = (id: string) => JSON.stringify({ type: "drop", action_id: id, x: 1, y: 1 });

  it("keeps an action pending until its final result, then clears it", () => {
    const pending = createPendingActions();
    pending.add("a", frame("a"), "Soltar gota");
    expect(pending.size).toBe(1);
    expect(pending.has("a")).toBe(true);
    const entry = pending.resolve({ action_id: "a", status: "applied" });
    expect(entry?.label).toBe("Soltar gota");
    expect(entry?.frame).toBe(frame("a"));
    expect(pending.size).toBe(0);
  });

  it("clears on every final status, including duplicate and the immediate rejections", () => {
    for (const status of ["applied", "duplicate", "rejected", "expired", "rate_limited", "paused", "forbidden"]) {
      const pending = createPendingActions();
      pending.add("a", frame("a"), "x");
      expect(pending.resolve({ action_id: "a", status })).not.toBeNull();
      expect(pending.size).toBe(0);
    }
  });

  it("ignores results for unknown actions and keeps others pending", () => {
    const pending = createPendingActions();
    pending.add("a", frame("a"), "x");
    expect(pending.resolve({ action_id: "zzz", status: "applied" })).toBeNull();
    expect(pending.resolve({ action_id: "a", status: "mystery" })).toBeNull();
    expect(pending.size).toBe(1);
  });

  it("settles an action by id (error frames, expiry) and ignores unknown ids", () => {
    const pending = createPendingActions();
    pending.add("a", frame("a"), "x");
    expect(pending.settle("a")).not.toBeNull();
    expect(pending.size).toBe(0);
    expect(pending.settle("a")).toBeNull();
    expect(pending.settle("")).toBeNull();
  });
});

describe("planResend", () => {
  const entry = (id: string, type: string, addedAt: number) => ({
    actionId: id,
    label: id,
    frame: JSON.stringify({ type, action_id: id }),
    type,
    addedAt,
  });

  it("uses the hub's 30 s queue expiry as the age limit", () => {
    expect(PENDING_MAX_AGE_MS).toBe(30_000);
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
    expect(plan.resend.map((e) => e.actionId)).toEqual(["a", "b", "edge"]);
    expect(plan.expired.map((e) => e.actionId)).toEqual(["old"]);
    expect(plan.adminDropped.map((e) => e.actionId)).toEqual(["demo", "reset"]);
  });

  it("counts an old admin command as expired, not as admin-dropped", () => {
    const plan = planResend([entry("r", "reset_valley", 0)], PENDING_MAX_AGE_MS + 1);
    expect(plan.expired.map((e) => e.actionId)).toEqual(["r"]);
    expect(plan.adminDropped.length).toBe(0);
  });

  it("returns empty lists for nothing pending", () => {
    expect(planResend([], 0)).toEqual({ resend: [], expired: [], adminDropped: [] });
  });
});

describe("pending actions on reconnect", () => {
  it("removes expired and admin entries and keeps the resent ones pending", () => {
    let clock = 0;
    const pending = createPendingActions({ now: () => clock });
    const f = (type: string, id: string) => JSON.stringify({ type, action_id: id });
    pending.add("old", f("drop", "old"), "gota vieja");
    clock = 20_000;
    pending.add("reset", f("reset_valley", "reset"), "Reiniciar valle");
    pending.add("new", f("drop", "new"), "gota nueva");
    clock = 31_000;
    const plan = pending.planReconnect();
    expect(plan.resend.map((e) => e.actionId)).toEqual(["new"]);
    expect(plan.expired.map((e) => e.actionId)).toEqual(["old"]);
    expect(plan.adminDropped.map((e) => e.actionId)).toEqual(["reset"]);
    expect(pending.size).toBe(1);
    expect(pending.has("new")).toBe(true);
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
    expect(selectionView(state, null)).toEqual({ label: "ninguna", info: "Haz clic en una casilla." });
    expect(selectionView(null, null)).toEqual({ label: "ninguna", info: "Haz clic en una casilla." });
  });

  it("describes the selected tile, indexing by the state's width", () => {
    const view = selectionView(state, { x: 1, y: 0 });
    expect(view.label).toBe("(1, 0)");
    expect(view.info).toBe("pasto, humedad 75, en llamas (2 gotas recibidas)");
    expect(selectionView(state, { x: 0, y: 1 }).info).toBe("arbusto, humedad 10, instalación: sprinkler");
  });

  it("says there is no map data when the state has no map", () => {
    expect(selectionView(null, { x: 3, y: 3 }).info).toBe("Sin datos del mapa todavía.");
  });
});

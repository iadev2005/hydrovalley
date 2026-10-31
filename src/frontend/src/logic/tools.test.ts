import { describe, expect, it } from "vitest";
import type { StateFrame } from "./play.ts";
import {
  TOOLS,
  hasWaterForDrop,
  interactionFor,
  isInstallationTool,
  placementFor,
  tileTooltip,
} from "./tools.ts";

/** Valle de 3 × 1: (0,0) pasto, (1,0) según `stage`, (2,0) tierra seca. */
function state(overrides: Partial<StateFrame> = {}, stage = 2): StateFrame {
  return {
    type: "state",
    tick: 1n,
    paused: false,
    demo_mode: false,
    width: 3,
    height: 1,
    stages: [2, stage, 1],
    humidity: [10n, 40n, 0n],
    fires: [],
    installations: [],
    buildings: [false, false, false],
    trash: [0, 0, 0],
    player: null,
    ...overrides,
  };
}

const AT = { x: 1, y: 0 };

describe("interactionFor", () => {
  it("riega una casilla con planta o tierra", () => {
    expect(interactionFor(state(), AT)).toEqual({ kind: "drop", label: "Regar" });
    expect(interactionFor(state({}, 1), AT).kind).toBe("drop");
  });

  it("recoge la basura en lugar de regar", () => {
    const action = interactionFor(state({ trash: [0, 2, 0] }), AT);
    expect(action).toEqual({ kind: "clean_trash", label: "Recoger basura" });
  });

  it("echa agua al fuego y cuenta las gotas recibidas", () => {
    const action = interactionFor(
      state({ fires: [{ x: 1, y: 0, drops_received: 1 }] }, 3),
      AT,
    );
    expect(action.kind).toBe("drop");
    expect(action.label).toBe("Echar agua al fuego (1 gota recibida)");
  });

  it("riega aunque haya una instalación, como acepta el simulador", () => {
    const action = interactionFor(
      state({
        installations: [
          { x: 1, y: 0, installation: "sprinkler", owner_id: "p", remaining_durability: 5 },
        ],
      }),
      AT,
    );
    expect(action.kind).toBe("drop");
  });

  it("no hace nada en el río, en una roca ni en un edificio", () => {
    expect(interactionFor(state({}, 6), AT)).toEqual({
      kind: "none",
      label: "El río no se riega",
    });
    expect(interactionFor(state({}, 7), AT).kind).toBe("none");
    expect(interactionFor(state({ buildings: [false, true, false] }), AT).kind).toBe("none");
  });

  it("no hace nada sin mapa o fuera del valle", () => {
    expect(interactionFor(null, AT).kind).toBe("none");
    expect(interactionFor(state(), { x: 5, y: 0 }).kind).toBe("none");
  });
});

describe("placementFor", () => {
  it("permite colocar en tierra o planta libre", () => {
    expect(placementFor(state(), AT, "drip")).toEqual({
      ok: true,
      label: "Colocar riego por goteo",
    });
  });

  it("rechaza las casillas que el simulador da por ocupadas", () => {
    const installed = state({
      installations: [{ x: 1, y: 0, installation: "drip", owner_id: "p", remaining_durability: 3 }],
    });
    expect(placementFor(installed, AT, "sprinkler")).toEqual({
      ok: false,
      label: "Ya hay una instalación aquí",
    });
    expect(placementFor(state({}, 6), AT, "sprinkler").ok).toBe(false);
    expect(placementFor(state({}, 7), AT, "sprinkler").ok).toBe(false);
    expect(placementFor(state({ trash: [0, 1, 0] }), AT, "sprinkler").label).toBe(
      "Primero recoge la basura",
    );
    expect(placementFor(state({ buildings: [false, true, false] }), AT, "sprinkler").ok).toBe(
      false,
    );
  });
});

describe("tileTooltip", () => {
  it("describe la casilla, su humedad y lo que hará el clic", () => {
    expect(tileTooltip(state(), AT, "interact")).toEqual({
      title: "Pasto",
      coord: "(1, 0)",
      humidity: "40",
      detail: null,
      action: "Clic: regar",
      ok: true,
    });
  });

  it("con una estructura elegida avisa si se puede colocar", () => {
    const tip = tileTooltip(state({}, 6), AT, "sprinkler");
    expect(tip?.title).toBe("Río");
    expect(tip?.humidity).toBeNull();
    expect(tip?.action).toBe("No se puede construir en el río");
    expect(tip?.ok).toBe(false);
  });

  it("muestra los usos que le quedan a una instalación", () => {
    const tip = tileTooltip(
      state({
        installations: [
          { x: 1, y: 0, installation: "treatment_plant", owner_id: "p", remaining_durability: 7 },
        ],
      }),
      AT,
      "interact",
    );
    expect(tip?.title).toBe("Planta de tratamiento sobre pasto");
    expect(tip?.detail).toBe("Le quedan 7 usos");
  });

  it("no hay tooltip sin mapa", () => {
    expect(tileTooltip(null, AT, "interact")).toBeNull();
  });
});

describe("TOOLS", () => {
  it("empieza por Interactuar y describe cada instalación", () => {
    expect(TOOLS[0]?.id).toBe("interact");
    expect(TOOLS.map((t) => t.id)).toEqual([
      "interact",
      "sprinkler",
      "drip",
      "treatment_plant",
      "trash_bin",
    ]);
    for (const tool of TOOLS) expect(tool.description.length).toBeGreaterThan(20);
    expect(isInstallationTool("drip")).toBe(true);
    expect(isInstallationTool("interact")).toBe(false);
  });
});

describe("hasWaterForDrop", () => {
  it("no deja regar con el depósito vacío", () => {
    expect(hasWaterForDrop({ demo_mode: false, player: { water: 0n } })).toBe(false);
    expect(hasWaterForDrop({ demo_mode: false, player: { water: 1n } })).toBe(true);
  });

  it("en modo demo la gota no gasta agua", () => {
    expect(hasWaterForDrop({ demo_mode: true, player: { water: 0n } })).toBe(true);
  });

  it("antes del primer comando el servidor decide", () => {
    expect(hasWaterForDrop(state())).toBe(true);
    expect(hasWaterForDrop(null)).toBe(true);
  });
});

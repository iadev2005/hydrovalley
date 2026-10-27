import { describe, expect, it } from "vitest";
import { BUILDING_COLOUR, INSTALLATION_LETTER, STAGE_COLOURS, TRASH_COLOURS } from "./ValleyCanvas.tsx";

// La vista cenital es la salida de emergencia de la isométrica (A4): tiene que
// poder dibujar todo lo que dibuja Phaser, también el terreno natural, los
// edificios, la basura y el cesto de reciclaje.
describe("top-down fallback palette", () => {
  it("has a distinct colour for each of the seven tile stages, water and rock included", () => {
    expect(STAGE_COLOURS).toHaveLength(7);
    expect(new Set(STAGE_COLOURS).size).toBe(7);
  });

  it("has a colour for buildings and one for each trash level", () => {
    expect(BUILDING_COLOUR).toMatch(/^#[0-9a-f]{6}$/);
    expect(TRASH_COLOURS).toHaveLength(3);
    expect(new Set(TRASH_COLOURS).size).toBe(3);
  });

  it("has a letter for every installation the hub accepts", () => {
    expect(INSTALLATION_LETTER).toEqual({ sprinkler: "A", drip: "G", treatment_plant: "P", trash_bin: "C" });
  });
});

import { describe, it, expect } from "vitest";
import { describeOfflineSummary } from "./offlineSummary.ts";
import type { OfflineSummary } from "./play.ts";

function summary(overrides: Partial<OfflineSummary> = {}): OfflineSummary {
  return {
    leaves_earned: 0n,
    water_reused: 0n,
    fires_extinguished: 0n,
    tiles_grown: 0n,
    ...overrides,
  };
}

describe("describeOfflineSummary", () => {
  it("has nothing to show without a summary or with everything at zero", () => {
    expect(describeOfflineSummary(null)).toBeNull();
    expect(describeOfflineSummary(summary())).toBeNull();
  });

  it("follows the wording of the document: tiles grown and a fire put out", () => {
    const text = describeOfflineSummary(
      summary({ tiles_grown: 38n, fires_extinguished: 1n, leaves_earned: 120n, water_reused: 3_450n }),
    );
    expect(text?.headline).toBe(
      "Mientras no estabas, tus aspersores hicieron crecer 38 casillas y apagaron un incendio.",
    );
    expect(text?.details).toEqual([
      { label: "Hojas ganadas", value: "120" },
      { label: "Agua reutilizada", value: "3.450 pts de humedad" },
    ]);
  });

  it("uses the singular for one tile and the plural for several fires", () => {
    expect(describeOfflineSummary(summary({ tiles_grown: 1n }))?.headline).toBe(
      "Mientras no estabas, tus aspersores hicieron crecer 1 casilla.",
    );
    expect(describeOfflineSummary(summary({ fires_extinguished: 2n }))?.headline).toBe(
      "Mientras no estabas, tus aspersores apagaron 2 incendios.",
    );
  });

  it("says they watered the valley when no tile grew and no fire went out", () => {
    const text = describeOfflineSummary(summary({ water_reused: 225n }));
    expect(text?.headline).toBe("Mientras no estabas, tus aspersores regaron el valle.");
    expect(text?.details).toEqual([{ label: "Agua reutilizada", value: "225 pts de humedad" }]);
  });

  it("keeps the precision of a u64 and skips the rows that are zero", () => {
    const text = describeOfflineSummary(summary({ tiles_grown: 18446744073709551615n }));
    expect(text?.headline).toContain("18.446.744.073.709.551.615 casillas");
    expect(text?.details).toEqual([]);
  });
});

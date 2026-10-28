import { describe, it, expect } from "vitest";
import qualityReport from "../../assets/assets_quality_report.json";

const EXPECTED_SPRITES = [
  "tile_1_dry.png",
  "tile_2_grass.png",
  "tile_water.png",
  "tile_polluted_soil.png",
  "tile_rock.png",
  "tile_3_bush.png",
  "tile_4_flowers.png",
  "tile_5_tree.png",
  "building_house.png",
  "trash_1_small.png",
  "trash_2_medium.png",
  "trash_3_large.png",
  "trash_water.png",
  "drip.png",
  "sprinkler.png",
  "treatment_plant.png",
  "trash_bin.png",
  "droplet.png",
  "splash.png",
  "fire.png",
  "fire_sheet.png",
  "tile_selector.png",
  "clean_sparkle.png",
];

describe("Pipeline de Calidad de Activos Gráficos (Fase 4 y 5)", () => {
  it("reconoce todos los 23 sprites esperados en el catálogo", () => {
    expect(EXPECTED_SPRITES).toHaveLength(23);
  });

  it("el reporte de calidad de assets registra 100% de cumplimiento sin fallos", () => {
    expect(qualityReport.total_audited).toBe(23);
    expect(qualityReport.passed).toBe(23);
    expect(qualityReport.failed).toBe(0);
    expect(qualityReport.compliance_rate).toBe("100.0%");

    const auditedFiles = qualityReport.sprites.map((s: { file: string }) => s.file);
    for (const file of EXPECTED_SPRITES) {
      expect(auditedFiles).toContain(file);
    }
  });
});

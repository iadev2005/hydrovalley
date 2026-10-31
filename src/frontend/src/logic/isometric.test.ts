import { describe, it, expect } from "vitest";
import {
  gridToIsometric,
  isometricToGrid,
  getIsometricDepth,
  getIsometricTileBounds,
  getValleyIsometricDimensions,
  ISO_TILE_WIDTH,
  ISO_TILE_HEIGHT,
  VALLEY_GRID_SIZE,
} from "./isometric.ts";

describe("gridToIsometric", () => {
  it("projects (0, 0) to origin", () => {
    expect(gridToIsometric(0, 0)).toEqual({ x: 0, y: 0 });
    expect(gridToIsometric(0, 0, 500, 200)).toEqual({ x: 500, y: 200 });
  });

  it("projects adjacent tiles following 2:1 ratio (dx = +/-16, dy = 8)", () => {
    // (1, 0) goes down-right
    expect(gridToIsometric(1, 0)).toEqual({ x: 16, y: 8 });
    // (0, 1) goes down-left
    expect(gridToIsometric(0, 1)).toEqual({ x: -16, y: 8 });
    // (1, 1) goes straight down
    expect(gridToIsometric(1, 1)).toEqual({ x: 0, y: 16 });
  });

  it("projects valley boundary corners correctly", () => {
    const originX = 512;
    const originY = 50;

    // Top corner (0, 0)
    expect(gridToIsometric(0, 0, originX, originY)).toEqual({ x: 512, y: 50 });

    // Right corner (31, 0)
    expect(gridToIsometric(31, 0, originX, originY)).toEqual({
      x: 512 + 31 * 16,
      y: 50 + 31 * 8,
    });

    // Left corner (0, 31)
    expect(gridToIsometric(0, 31, originX, originY)).toEqual({
      x: 512 - 31 * 16,
      y: 50 + 31 * 8,
    });

    // Bottom corner (31, 31)
    expect(gridToIsometric(31, 31, originX, originY)).toEqual({
      x: 512,
      y: 50 + 62 * 8,
    });
  });
});

describe("isometricToGrid (conversión inversa y clics)", () => {
  const originX = 512;
  const originY = 100;

  it("is exactly invertible for tile centers across all corners and grid points", () => {
    const testCases = [
      { x: 0, y: 0 },
      { x: 31, y: 0 },
      { x: 0, y: 31 },
      { x: 31, y: 31 },
      { x: 14, y: 9 }, // ejemplo del documento / proto
      { x: 16, y: 16 },
      { x: 7, y: 23 },
    ];

    for (const coord of testCases) {
      const screen = gridToIsometric(coord.x, coord.y, originX, originY);
      const picked = isometricToGrid(screen.x, screen.y, originX, originY);
      expect(picked).toEqual(coord);
    }
  });

  it("identifies points inside the diamond near the borders", () => {
    const center = gridToIsometric(10, 10, originX, originY);

    // Slightly towards top vertex
    expect(isometricToGrid(center.x, center.y - 7, originX, originY)).toEqual({
      x: 10,
      y: 10,
    });
    // Slightly towards bottom vertex
    expect(isometricToGrid(center.x, center.y + 7, originX, originY)).toEqual({
      x: 10,
      y: 10,
    });
    // Slightly towards left vertex
    expect(isometricToGrid(center.x - 14, center.y, originX, originY)).toEqual({
      x: 10,
      y: 10,
    });
    // Slightly towards right vertex
    expect(isometricToGrid(center.x + 14, center.y, originX, originY)).toEqual({
      x: 10,
      y: 10,
    });
  });

  it("rejects points falling outside the 32x32 valley", () => {
    // Points far above
    expect(isometricToGrid(512, -100, originX, originY)).toBeNull();
    // Point above tile (0, 0)
    expect(isometricToGrid(originX, originY - 12, originX, originY)).toBeNull();
    // Points far left
    expect(isometricToGrid(-500, 300, originX, originY)).toBeNull();
    // Points far right
    expect(isometricToGrid(2000, 300, originX, originY)).toBeNull();
    // Points far below
    expect(isometricToGrid(originX, 1000, originX, originY)).toBeNull();
  });

  it("handles non-finite and invalid inputs safely", () => {
    expect(isometricToGrid(Number.NaN, 100)).toBeNull();
    expect(isometricToGrid(100, Number.NaN)).toBeNull();
    expect(isometricToGrid(Number.POSITIVE_INFINITY, 100)).toBeNull();
    expect(isometricToGrid(100, Number.NEGATIVE_INFINITY)).toBeNull();
    expect(isometricToGrid(100, 100, Number.NaN)).toBeNull();
  });
});

describe("getIsometricDepth (orden de dibujado)", () => {
  it("sorts back-to-front by increasing Y and X", () => {
    const d00 = getIsometricDepth(0, 0);
    const d10 = getIsometricDepth(1, 0);
    const d01 = getIsometricDepth(0, 1);
    const d3131 = getIsometricDepth(31, 31);

    expect(d00).toBeLessThan(d10);
    expect(d10).toBeLessThan(d01);
    expect(d01).toBeLessThan(d3131);
    expect(d3131).toBe(31 * 32 + 31);
  });
});

describe("getIsometricTileBounds", () => {
  it("returns diamond vertices matching tile dimensions (32x16)", () => {
    const bounds = getIsometricTileBounds(5, 5, 200, 100);

    const center = gridToIsometric(5, 5, 200, 100);
    expect(bounds.center).toEqual(center);
    expect(bounds.top).toEqual({ x: center.x, y: center.y - 8 });
    expect(bounds.right).toEqual({ x: center.x + 16, y: center.y });
    expect(bounds.bottom).toEqual({ x: center.x, y: center.y + 8 });
    expect(bounds.left).toEqual({ x: center.x - 16, y: center.y });
  });
});

describe("getValleyIsometricDimensions", () => {
  it("computes total map screen bounding dimensions for 32x32 tiles", () => {
    const dims = getValleyIsometricDimensions(
      VALLEY_GRID_SIZE,
      ISO_TILE_WIDTH,
      ISO_TILE_HEIGHT,
    );
    expect(dims.totalWidth).toBe(1024);
    expect(dims.totalHeight).toBe(512);
  });
});

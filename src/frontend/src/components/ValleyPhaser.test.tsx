// @vitest-environment happy-dom
import { createRef } from "react";
import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { ValleyPhaser, type ValleyPhaserHandle } from "./ValleyPhaser.tsx";

describe("ValleyPhaser", () => {
  it("renders the valley container and camera toolbar", () => {
    const handleSelectTile = vi.fn();

    render(
      <ValleyPhaser
        gameState={null}
        selected={null}
        onSelectTile={handleSelectTile}
      />,
    );

    const valley = document.getElementById("valley");
    expect(valley).toBeDefined();
    expect(valley?.getAttribute("aria-label")).toBe(
      "Valle isométrico de 32 por 32 casillas",
    );
    expect(screen.getByRole("button", { name: "⊙ Centrar vista" })).toBeDefined();
    expect(screen.getByText(/Arrastra para mover/)).toBeDefined();
  });

  it("exposes imperative handle methods without error", () => {
    const handleSelectTile = vi.fn();
    const ref = createRef<ValleyPhaserHandle>();

    render(
      <ValleyPhaser
        ref={ref}
        gameState={null}
        selected={null}
        onSelectTile={handleSelectTile}
      />,
    );

    expect(ref.current).toBeDefined();
    expect(ref.current).not.toBeNull();
    const handle = ref.current;
    if (!handle) {
      throw new Error("Phaser ref handle was not assigned");
    }

    expect(typeof handle.spawnPendingDrop).toBe("function");
    expect(typeof handle.resolvePendingDrop).toBe("function");
    expect(typeof handle.clearPendingDrops).toBe("function");
    expect(typeof handle.resetCamera).toBe("function");

    // Call them to ensure safe execution even without active Phaser scene
    expect(() => {
      handle.spawnPendingDrop("act-1", 5, 5);
      handle.resolvePendingDrop("act-1", "applied");
      handle.clearPendingDrops();
      handle.resetCamera();
    }).not.toThrow();
  });
});


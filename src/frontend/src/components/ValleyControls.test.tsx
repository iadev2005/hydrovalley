// @vitest-environment happy-dom
import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { ValleyControls } from "./ValleyControls.tsx";
import type { StateFrame } from "../logic/play.ts";

const mockState: StateFrame = {
  type: "state",
  tick: 10n,
  paused: false,
  demo_mode: false,
  width: 32,
  height: 32,
  stages: Array.from({ length: 1024 }, () => 1),
  humidity: Array.from({ length: 1024 }, () => 0n),
  fires: [],
  installations: [],
  buildings: [],
  trash: [],
  player: {
    water: 85n,
    tank_capacity: 100n,
    leaves: 250n,
    upgrades: [],
    next_sprinkler_price: 50n,
    next_drip_price: 120n,
    next_treatment_plant_price: 300n,
    next_trash_bin_price: 15n,
    offline_summary: null,
  },
};

describe("ValleyControls", () => {
  it("renders player panel water, leaves and prices", () => {
    render(
      <ValleyControls
        gameState={mockState}
        selected={{ x: 5, y: 10 }}
        isOpen={true}
        pendingCount={2}
        actionMessage={{ text: "Gota aceptada", ok: true }}
        onDrop={vi.fn()}
        onPlaceInstallation={vi.fn()}
        onBuyUpgrade={vi.fn()}
      />,
    );

    expect(screen.getByText("85 / 100")).toBeDefined();
    expect(screen.getByText("250")).toBeDefined();
    expect(screen.getByText("50")).toBeDefined();
    expect(screen.getByText("120")).toBeDefined();
    expect(screen.getByText("300")).toBeDefined();
    expect(screen.getByText("2")).toBeDefined(); // pending count
    expect(screen.getByText("Gota aceptada")).toBeDefined();
  });

  it("disables tile action buttons when no tile is selected or socket closed", () => {
    const { rerender } = render(
      <ValleyControls
        gameState={mockState}
        selected={null}
        isOpen={true}
        pendingCount={0}
        actionMessage={null}
        onDrop={vi.fn()}
        onPlaceInstallation={vi.fn()}
        onBuyUpgrade={vi.fn()}
      />,
    );

    const dropBtn = screen.getByRole("button", { name: "Soltar gota" }) as HTMLButtonElement;
    expect(dropBtn.disabled).toBe(true);

    rerender(
      <ValleyControls
        gameState={mockState}
        selected={{ x: 5, y: 10 }}
        isOpen={false}
        pendingCount={0}
        actionMessage={null}
        onDrop={vi.fn()}
        onPlaceInstallation={vi.fn()}
        onBuyUpgrade={vi.fn()}
      />,
    );

    expect(dropBtn.disabled).toBe(true);
  });

  it("enables buttons and triggers callbacks when open and tile selected", () => {
    const onDrop = vi.fn();
    const onPlaceInstallation = vi.fn();
    const onBuyUpgrade = vi.fn();

    render(
      <ValleyControls
        gameState={mockState}
        selected={{ x: 5, y: 10 }}
        isOpen={true}
        pendingCount={0}
        actionMessage={null}
        onDrop={onDrop}
        onPlaceInstallation={onPlaceInstallation}
        onBuyUpgrade={onBuyUpgrade}
      />,
    );

    const dropBtn = screen.getByRole("button", { name: "Soltar gota" });
    fireEvent.click(dropBtn);
    expect(onDrop).toHaveBeenCalledTimes(1);

    const sprinklerBtn = screen.getByRole("button", { name: /Colocar aspersor/i });
    fireEvent.click(sprinklerBtn);
    expect(onPlaceInstallation).toHaveBeenCalledWith("sprinkler");

    const buyDropBtn = screen.getByRole("button", { name: "Comprar gota grande" });
    fireEvent.click(buyDropBtn);
    expect(onBuyUpgrade).toHaveBeenCalledWith("bigger_drop");
  });

  it("renders durability indicator and progress bar when selected tile has an installation", () => {
    const stateWithInst: StateFrame = {
      ...mockState,
      installations: [
        {
          x: 5,
          y: 10,
          installation: "sprinkler",
          owner_id: "ana",
          remaining_durability: 25,
        },
      ],
    };

    render(
      <ValleyControls
        gameState={stateWithInst}
        selected={{ x: 5, y: 10 }}
        isOpen={true}
        pendingCount={0}
        actionMessage={null}
        onDrop={vi.fn()}
        onPlaceInstallation={vi.fn()}
        onBuyUpgrade={vi.fn()}
      />,
    );

    expect(screen.getByText("25")).toBeDefined();
    expect(screen.getByText(/usos restantes/i)).toBeDefined();
    const bar = document.getElementById("durability-bar") as HTMLProgressElement;
    expect(bar).toBeDefined();
    expect(bar.value).toBe(25);
    expect(bar.max).toBe(30);
  });

  it("handles trash bin placement and trash cleaning", () => {
    const onPlaceInstallation = vi.fn();
    const onCleanTrash = vi.fn();
    const trashArr = Array.from({ length: 1024 }, () => 0);
    trashArr[10 * 32 + 5] = 2; // tile (5, 10) has trash level 2

    const stateWithTrash: StateFrame = {
      ...mockState,
      trash: trashArr,
    };

    render(
      <ValleyControls
        gameState={stateWithTrash}
        selected={{ x: 5, y: 10 }}
        isOpen={true}
        pendingCount={0}
        actionMessage={null}
        onDrop={vi.fn()}
        onPlaceInstallation={onPlaceInstallation}
        onCleanTrash={onCleanTrash}
        onBuyUpgrade={vi.fn()}
      />,
    );

    const trashBinBtn = screen.getByRole("button", { name: /Colocar cesto de reciclaje/i });
    fireEvent.click(trashBinBtn);
    expect(onPlaceInstallation).toHaveBeenCalledWith("trash_bin");

    const cleanTrashBtn = screen.getByRole("button", { name: /Limpiar basura/i });
    expect(cleanTrashBtn).toBeDefined();
    fireEvent.click(cleanTrashBtn);
    expect(onCleanTrash).toHaveBeenCalledTimes(1);
  });
});

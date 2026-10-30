// @vitest-environment happy-dom
import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { ValleyTelemetryHUD } from "./ValleyTelemetryHUD.tsx";
import type { StateFrame } from "../logic/play.ts";

function createMockFrame(overrides: Partial<StateFrame> = {}): StateFrame {
  return {
    type: "state",
    tick: 120n,
    paused: false,
    demo_mode: false,
    width: 32,
    height: 32,
    stages: new Array(1024).fill(1),
    humidity: new Array(1024).fill(50n),
    fires: [],
    installations: [{ x: 1, y: 1, installation: "aspersor", owner_id: "u1" }],
    player: {
      water: 25n,
      tank_capacity: 50n,
      leaves: 350n,
      upgrades: [],
      next_sprinkler_price: 20n,
      next_drip_price: 30n,
      next_treatment_plant_price: 50n,
      offline_summary: null,
    },
    ...overrides,
  };
}

describe("ValleyTelemetryHUD component", () => {
  it("muestra el mensaje de espera cuando gameState es nulo", () => {
    render(<ValleyTelemetryHUD gameState={null} />);
    expect(screen.getByText("📡 Esperando telemetría del valle...")).toBeDefined();
  });

  it("renderiza métricas clave correctamente con gameState", () => {
    const frame = createMockFrame();
    render(<ValleyTelemetryHUD gameState={frame} />);

    expect(screen.getByText("Bajo control")).toBeDefined();
    expect(screen.getByText("25 / 50")).toBeDefined();
    expect(screen.getByText("100%")).toBeDefined();
    expect(screen.getByText("350")).toBeDefined();
    expect(screen.getByText("1")).toBeDefined();
  });

  it("muestra alerta y botón cliqueable si hay fuegos activos", () => {
    const onFocus = vi.fn();
    const frame = createMockFrame({
      fires: [
        { x: 3, y: 3, drops_received: 0 },
        { x: 4, y: 4, drops_received: 0 },
      ],
    });

    render(<ValleyTelemetryHUD gameState={frame} onFocusFire={onFocus} />);

    const fireButton = screen.getByRole("button", {
      name: /Fuegos activos/i,
    });
    expect(fireButton).toBeDefined();

    fireEvent.click(fireButton);
    expect(onFocus).toHaveBeenCalledTimes(1);
  });

  it("renderiza el tick y las insignias de Pausa y Demo cuando están activas", () => {
    const frame = createMockFrame({
      tick: 340n,
      paused: true,
      demo_mode: true,
    });

    render(<ValleyTelemetryHUD gameState={frame} />);

    expect(screen.getByText("t-340")).toBeDefined();
    expect(screen.getByText("Pausa")).toBeDefined();
    expect(screen.getByText("Demo")).toBeDefined();
  });
});

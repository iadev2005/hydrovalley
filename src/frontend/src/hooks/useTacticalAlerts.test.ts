// @vitest-environment happy-dom
import { describe, it, expect, vi } from "vitest";
import { renderHook, act } from "@testing-library/react";
import { useTacticalAlerts } from "./useTacticalAlerts.ts";
import type { StateFrame } from "../logic/play.ts";

function createMockFrame(overrides: Partial<StateFrame> = {}): StateFrame {
  return {
    type: "state",
    tick: 100n,
    paused: false,
    demo_mode: false,
    width: 32,
    height: 32,
    stages: new Array(1024).fill(1),
    humidity: new Array(1024).fill(50n),
    fires: [],
    installations: [],
    player: {
      water: 10n,
      tank_capacity: 50n,
      leaves: 100n,
      upgrades: [],
      next_sprinkler_price: 20n,
      next_drip_price: 30n,
      next_treatment_plant_price: 50n,
      offline_summary: null,
    },
    ...overrides,
  };
}

describe("useTacticalAlerts hook", () => {
  it("inicia sin alertas cuando gameState es null", () => {
    const { result } = renderHook(() => useTacticalAlerts({ gameState: null }));
    expect(result.current.alerts).toHaveLength(0);
  });

  it("alerta sobre incendios activos en el primer frame si existen", () => {
    const initialFrame = createMockFrame({
      fires: [{ x: 5, y: 5, drops_received: 0 }],
    });

    const { result } = renderHook(() => useTacticalAlerts({ gameState: initialFrame }));
    expect(result.current.alerts).toHaveLength(1);
    expect(result.current.alerts[0]?.variant).toBe("danger");
    expect(result.current.alerts[0]?.title).toContain("Incendios en curso");
  });

  it("genera alerta de nuevo foco de incendio cuando aumentan los fuegos", () => {
    const frame1 = createMockFrame({ fires: [] });
    let currentFrame = frame1;

    const { result, rerender } = renderHook(() =>
      useTacticalAlerts({ gameState: currentFrame }),
    );
    expect(result.current.alerts).toHaveLength(0);

    // Nuevo foco de incendio
    currentFrame = createMockFrame({
      fires: [{ x: 10, y: 10, drops_received: 0 }],
    });
    rerender();

    expect(result.current.alerts).toHaveLength(1);
    expect(result.current.alerts[0]?.title).toContain("¡Nuevo incendio detectado!");
    expect(result.current.alerts[0]?.variant).toBe("danger");
  });

  it("genera alerta de éxito cuando se extingue un fuego", () => {
    const frame1 = createMockFrame({
      fires: [{ x: 10, y: 10, drops_received: 0 }],
    });
    let currentFrame = frame1;

    const { result, rerender } = renderHook(() =>
      useTacticalAlerts({ gameState: currentFrame }),
    );

    // Apagado
    currentFrame = createMockFrame({ fires: [] });
    rerender();

    expect(result.current.alerts[0]?.title).toContain("Fuego extinguido");
    expect(result.current.alerts[0]?.variant).toBe("success");
  });

  it("alerta cuando el agua se agota", () => {
    const frame1 = createMockFrame();
    let currentFrame = frame1;

    const { result, rerender } = renderHook(() =>
      useTacticalAlerts({ gameState: currentFrame }),
    );

    currentFrame = createMockFrame({
      player: frame1.player
        ? {
            ...frame1.player,
            water: 0n,
          }
        : null,
    });
    rerender();

    expect(result.current.alerts[0]?.variant).toBe("warning");
    expect(result.current.alerts[0]?.title).toContain("Reserva de agua agotada");
  });

  it("permite descartar una alerta con dismissAlert()", () => {
    const { result } = renderHook(() => useTacticalAlerts({ gameState: null }));

    act(() => {
      result.current.addAlert({
        variant: "info",
        icon: "ℹ️",
        title: "Test",
        message: "Mensaje de prueba",
      });
    });

    expect(result.current.alerts).toHaveLength(1);
    const alertId = result.current.alerts[0]?.id ?? "";

    act(() => {
      result.current.dismissAlert(alertId);
    });

    expect(result.current.alerts).toHaveLength(0);
  });

  it("ejecuta onFocusFire cuando se activa la acción de la alerta", () => {
    const onFocus = vi.fn();
    const frame1 = createMockFrame({ fires: [] });
    let currentFrame = frame1;

    const { result, rerender } = renderHook(() =>
      useTacticalAlerts({ gameState: currentFrame, onFocusFire: onFocus }),
    );

    currentFrame = createMockFrame({
      fires: [{ x: 3, y: 3, drops_received: 0 }],
    });
    rerender();

    const alert = result.current.alerts[0];
    expect(alert?.actionLabel).toBe("Enfocar (F)");
    alert?.onAction?.();

    expect(onFocus).toHaveBeenCalled();
  });
});

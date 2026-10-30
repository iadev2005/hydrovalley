// @vitest-environment happy-dom
import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { TacticalToasts } from "./TacticalToasts.tsx";
import type { TacticalAlert } from "../hooks/useTacticalAlerts.ts";

describe("TacticalToasts component", () => {
  it("no renderiza nada si la lista de alertas está vacía", () => {
    const { container } = render(
      <TacticalToasts alerts={[]} onDismiss={() => {}} />,
    );
    expect(container.firstChild).toBeNull();
  });

  it("renderiza correctamente las alertas con sus textos e iconos", () => {
    const alerts: TacticalAlert[] = [
      {
        id: "alert-1",
        variant: "danger",
        icon: "🚨",
        title: "¡Alerta de Fuego!",
        message: "Se inició un incendio.",
      },
    ];

    render(<TacticalToasts alerts={alerts} onDismiss={() => {}} />);

    expect(screen.getByText("¡Alerta de Fuego!")).toBeDefined();
    expect(screen.getByText("Se inició un incendio.")).toBeDefined();
  });

  it("permite ejecutar la acción de la alerta al pulsar su botón", () => {
    const onAction = vi.fn();
    const alerts: TacticalAlert[] = [
      {
        id: "alert-1",
        variant: "danger",
        icon: "🔥",
        title: "Fuego",
        message: "Fuego activo",
        actionLabel: "Enfocar (F)",
        onAction,
      },
    ];

    render(<TacticalToasts alerts={alerts} onDismiss={() => {}} />);

    const actionBtn = screen.getByRole("button", { name: "Enfocar (F)" });
    fireEvent.click(actionBtn);

    expect(onAction).toHaveBeenCalledTimes(1);
  });

  it("llama a onDismiss con el id correcto al pulsar el botón de cerrar", () => {
    const onDismiss = vi.fn();
    const alerts: TacticalAlert[] = [
      {
        id: "alert-123",
        variant: "info",
        icon: "ℹ️",
        title: "Info",
        message: "Información",
      },
    ];

    render(<TacticalToasts alerts={alerts} onDismiss={onDismiss} />);

    const dismissBtn = screen.getByRole("button", { name: "Cerrar aviso" });
    fireEvent.click(dismissBtn);

    expect(onDismiss).toHaveBeenCalledWith("alert-123");
  });
});

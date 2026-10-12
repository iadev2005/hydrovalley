// @vitest-environment happy-dom
import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { AdminPanel } from "./AdminPanel.tsx";

describe("AdminPanel", () => {
  it("renders demo toggle with proper label and handles click", () => {
    const onToggleDemo = vi.fn();
    const { rerender } = render(
      <AdminPanel
        isOpen={true}
        demoMode={false}
        onToggleDemo={onToggleDemo}
        onResetValley={vi.fn()}
      />,
    );

    const toggleBtn = screen.getByRole("button", { name: "Activar modo demo" });
    fireEvent.click(toggleBtn);
    expect(onToggleDemo).toHaveBeenCalledTimes(1);

    rerender(
      <AdminPanel
        isOpen={true}
        demoMode={true}
        onToggleDemo={onToggleDemo}
        onResetValley={vi.fn()}
      />,
    );
    expect(screen.getByRole("button", { name: "Desactivar modo demo" })).toBeDefined();
  });

  it("requires confirmation before executing valley reset", () => {
    const onResetValley = vi.fn();
    render(
      <AdminPanel
        isOpen={true}
        demoMode={false}
        onToggleDemo={vi.fn()}
        onResetValley={onResetValley}
      />,
    );

    const resetBtn = screen.getByRole("button", { name: "Reiniciar valle" });
    fireEvent.click(resetBtn);

    // Confirmation prompt appears
    expect(screen.getByText(/Se borrará el progreso de todos los jugadores/i)).toBeDefined();
    expect(onResetValley).not.toHaveBeenCalled();

    // Cancel cancels
    const cancelBtn = screen.getByRole("button", { name: "Cancelar" });
    fireEvent.click(cancelBtn);
    expect(onResetValley).not.toHaveBeenCalled();

    // Reopen and confirm
    fireEvent.click(resetBtn);
    const confirmBtn = screen.getByRole("button", { name: "Sí, reiniciar" });
    fireEvent.click(confirmBtn);
    expect(onResetValley).toHaveBeenCalledTimes(1);
  });
});

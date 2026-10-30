// @vitest-environment happy-dom
import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { ToolPalette } from "./ToolPalette.tsx";

const prices = { sprinkler: "12", drip: "25", treatment_plant: "40", trash_bin: "15" };

function renderPalette(overrides: Partial<Parameters<typeof ToolPalette>[0]> = {}) {
  const props = {
    tool: "interact" as const,
    prices,
    leaves: 20n,
    owned: { bigger_drop: false, bigger_tank: true },
    isOpen: true,
    onSelectTool: vi.fn(),
    onBuyUpgrade: vi.fn(),
    ...overrides,
  };
  render(<ToolPalette {...props} />);
  return props;
}

describe("ToolPalette", () => {
  it("marca la herramienta activa y muestra su descripción", () => {
    renderPalette({ tool: "drip" });
    expect(
      screen.getByRole("button", { name: /Riego por goteo/ }).getAttribute("aria-pressed"),
    ).toBe("true");
    expect(
      screen.getByRole("button", { name: /Interactuar/ }).getAttribute("aria-pressed"),
    ).toBe("false");
    expect(screen.getByText("Herramienta activa")).toBeDefined();
    expect(screen.getByText(/las gotas rinden el doble/)).toBeDefined();
  });

  it("al pasar el ratón cambia la tarjeta a «Para qué sirve»", () => {
    renderPalette();
    fireEvent.mouseEnter(screen.getByRole("button", { name: /Papelera/ }));
    expect(screen.getByText("Para qué sirve")).toBeDefined();
    expect(screen.getByText(/Recoge sola la basura/)).toBeDefined();
  });

  it("enseña el precio siguiente y atenúa lo que no alcanza", () => {
    renderPalette();
    expect(document.getElementById("price-sprinkler")?.textContent).toBe("12");
    const plant = screen.getByRole("button", { name: /Planta de tratamiento/ });
    expect(plant.className).toContain("tool-poor");
    const sprinkler = screen.getByRole("button", { name: /Aspersor/ });
    expect(sprinkler.className).not.toContain("tool-poor");
  });

  it("elige herramienta y compra mejoras", () => {
    const props = renderPalette();
    fireEvent.click(screen.getByRole("button", { name: /Aspersor/ }));
    expect(props.onSelectTool).toHaveBeenCalledWith("sprinkler");

    const drop = document.getElementById("buy-bigger_drop") as HTMLButtonElement;
    fireEvent.click(drop);
    expect(props.onBuyUpgrade).toHaveBeenCalledWith("bigger_drop");
    const tank = document.getElementById("buy-bigger_tank") as HTMLButtonElement;
    expect(tank.disabled).toBe(true);
    expect(tank.textContent).toBe("Comprada");
  });

  it("no deja comprar con la conexión cerrada", () => {
    renderPalette({ isOpen: false });
    expect((document.getElementById("buy-bigger_drop") as HTMLButtonElement).disabled).toBe(true);
  });
});

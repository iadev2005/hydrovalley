// @vitest-environment happy-dom
import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { AuthPanel } from "./AuthPanel.tsx";

describe("AuthPanel", () => {
  it("renders access form with username and password fields", () => {
    render(
      <AuthPanel
        onLogin={vi.fn()}
        onRegister={vi.fn()}
        isBusy={false}
        isRetryLocked={false}
        error=""
        onClearError={vi.fn()}
      />,
    );

    expect(screen.getByLabelText(/Usuario/i)).toBeDefined();
    expect(screen.getByLabelText(/Contraseña/i)).toBeDefined();
    expect(screen.getByRole("button", { name: "Entrar" })).toBeDefined();
    expect(screen.getByRole("button", { name: "Registrarse" })).toBeDefined();
  });

  it("shows local error if submitted with empty credentials", async () => {
    const onLogin = vi.fn();
    render(
      <AuthPanel
        onLogin={onLogin}
        onRegister={vi.fn()}
        isBusy={false}
        isRetryLocked={false}
        error=""
        onClearError={vi.fn()}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Registrarse" }));
    expect(screen.getByText("escribe el usuario y la contraseña")).toBeDefined();
    expect(onLogin).not.toHaveBeenCalled();
  });

  it("calls onLogin when form is submitted with credentials", async () => {
    const onLogin = vi.fn().mockResolvedValue(true);
    render(
      <AuthPanel
        onLogin={onLogin}
        onRegister={vi.fn()}
        isBusy={false}
        isRetryLocked={false}
        error=""
        onClearError={vi.fn()}
      />,
    );

    fireEvent.change(screen.getByLabelText(/Usuario/i), {
      target: { value: "  alice  " },
    });
    fireEvent.change(screen.getByLabelText(/Contraseña/i), {
      target: { value: "secret123" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Entrar" }));

    expect(onLogin).toHaveBeenCalledWith({
      username: "alice",
      password: "secret123",
    });
  });

  it("disables buttons when isBusy or isRetryLocked is true", () => {
    const { rerender } = render(
      <AuthPanel
        onLogin={vi.fn()}
        onRegister={vi.fn()}
        isBusy={true}
        isRetryLocked={false}
        error=""
        onClearError={vi.fn()}
      />,
    );

    expect((screen.getByRole("button", { name: "Entrar" }) as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByRole("button", { name: "Registrarse" }) as HTMLButtonElement).disabled).toBe(true);

    rerender(
      <AuthPanel
        onLogin={vi.fn()}
        onRegister={vi.fn()}
        isBusy={false}
        isRetryLocked={true}
        error=""
        onClearError={vi.fn()}
      />,
    );

    expect((screen.getByRole("button", { name: "Entrar" }) as HTMLButtonElement).disabled).toBe(true);
  });

  it("displays server error message with role alert", () => {
    render(
      <AuthPanel
        onLogin={vi.fn()}
        onRegister={vi.fn()}
        isBusy={false}
        isRetryLocked={false}
        error="credenciales inválidas"
        onClearError={vi.fn()}
      />,
    );

    const alertEl = screen.getByRole("alert");
    expect(alertEl.textContent).toBe("credenciales inválidas");
  });
});

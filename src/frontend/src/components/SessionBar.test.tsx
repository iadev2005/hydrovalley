// @vitest-environment happy-dom
import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { SessionBar } from "./SessionBar.tsx";

describe("SessionBar", () => {
  it("renders session username and triggers logout callback", () => {
    const onLogout = vi.fn();
    render(<SessionBar username="bob" onLogout={onLogout} />);

    expect(screen.getByText("bob")).toBeDefined();
    const logoutBtn = screen.getByRole("button", { name: "Salir" });
    fireEvent.click(logoutBtn);
    expect(onLogout).toHaveBeenCalledTimes(1);
  });
});

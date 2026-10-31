// @vitest-environment happy-dom
import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { OfflineSummaryPanel } from "./OfflineSummaryPanel.tsx";
import type { OfflineSummary } from "../logic/play.ts";

const summary: OfflineSummary = {
  leaves_earned: 12n,
  water_reused: 450n,
  fires_extinguished: 1n,
  tiles_grown: 38n,
};

describe("OfflineSummaryPanel", () => {
  it("shows the sentence and the counts, and closes on request", () => {
    const onDismiss = vi.fn();
    render(<OfflineSummaryPanel summary={summary} onDismiss={onDismiss} />);

    expect(screen.getByRole("status")).toBeDefined();
    expect(
      screen.getByText(
        "Mientras no estabas, tus aspersores hicieron crecer 38 casillas y apagaron un incendio.",
      ),
    ).toBeDefined();
    expect(screen.getByText("Hojas ganadas")).toBeDefined();
    expect(screen.getByText("12")).toBeDefined();

    fireEvent.click(screen.getByRole("button", { name: "Cerrar" }));
    expect(onDismiss).toHaveBeenCalledTimes(1);
  });

  it("focuses the close button on mount", () => {
    render(<OfflineSummaryPanel summary={summary} onDismiss={() => {}} />);
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "Cerrar" }));
  });

  it("calls dismiss when Escape is pressed", () => {
    const onDismiss = vi.fn();
    render(<OfflineSummaryPanel summary={summary} onDismiss={onDismiss} />);
    fireEvent.keyDown(document, { key: "Escape" });
    expect(onDismiss).toHaveBeenCalledTimes(1);
  });

  it("keeps Tab inside the dialog", () => {
    render(<OfflineSummaryPanel summary={summary} onDismiss={() => {}} />);
    const close = screen.getByRole("button", { name: "Cerrar" });
    expect(fireEvent.keyDown(close, { key: "Tab" })).toBe(false);
    expect(fireEvent.keyDown(close, { key: "Tab", shiftKey: true })).toBe(false);
  });

  it("labels the duration as time without connection", () => {
    render(
      <OfflineSummaryPanel summary={summary} offlineDurationMs={65_000} onDismiss={() => {}} />,
    );
    expect(screen.getByText(/Tiempo sin conexión/)).toBeDefined();
  });

  it("renders nothing when there is nothing to show", () => {
    const { container } = render(<OfflineSummaryPanel summary={null} onDismiss={() => {}} />);
    expect(container.innerHTML).toBe("");
  });
});

// @vitest-environment happy-dom
import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import { SystemDashboard } from "./SystemDashboard.tsx";
import type { SystemView } from "../logic/system.ts";

const mockSystemView: SystemView = {
  generatedAt: "2026-10-11T12:00:00Z",
  services: [
    { name: "hub", label: "Hub", statusText: "ok", up: true, detail: "10ms" },
    { name: "auth", label: "Auth", statusText: "caído", up: false, detail: "" },
  ],
  hub: {
    sessions: "5",
    goroutines: "42",
    actionsAdmitted: "12",
    actionsApplied: "12",
    latencyP50: "4",
    latencyP95: "15",
    tick: "99",
    stepLast: "8",
    stepP95: "12",
    stepFailures: "0",
    queue: "1",
    paused: "no",
    breaker: "cerrado",
    humidityTotal: "8000",
    humidityContributed: "1500",
    acceptedDrops: "300",
    activeFires: "2",
    bootId: "boot-1",
    restoredTick: "50",
  },
};

describe("SystemDashboard", () => {
  it("renders services with up and down classes", () => {
    render(
      <SystemDashboard
        metrics={mockSystemView}
        error={null}
        lastUpdated="Última actualización: 12:00:00"
      />,
    );

    expect(screen.getByText(/Hub: ok/)).toBeDefined();
    expect(screen.getByText(/Auth: caído/)).toBeDefined();
    expect(screen.getByText("Última actualización: 12:00:00")).toBeDefined();
  });

  it("renders hub metrics and valley stats", () => {
    render(
      <SystemDashboard
        metrics={mockSystemView}
        error={null}
        lastUpdated="Última actualización: 12:00:00"
      />,
    );

    expect(document.querySelector('[data-field="sessions"]')?.textContent).toBe("5");
    expect(document.querySelector('[data-field="goroutines"]')?.textContent).toBe("42");
    expect(document.querySelector('[data-field="humidityTotal"]')?.textContent).toBe("8000");
  });

  it("renders missing hub message when hub is null", () => {
    render(
      <SystemDashboard
        metrics={{ ...mockSystemView, hub: null }}
        error={null}
        lastUpdated="Última actualización: 12:00:00"
      />,
    );

    const missing = document.getElementById("hub-missing");
    expect(missing).toBeDefined();
    expect(missing?.hasAttribute("hidden")).toBe(false);
  });

  it("renders error alert when error is present", () => {
    render(
      <SystemDashboard
        metrics={null}
        error="Servicio no disponible"
        lastUpdated="Última actualización: —"
      />,
    );

    expect(screen.getByRole("alert").textContent).toBe("Servicio no disponible");
  });
});

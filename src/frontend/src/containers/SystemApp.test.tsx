// @vitest-environment happy-dom
import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { SystemApp } from "./SystemApp.tsx";
import type { FetchFn } from "../logic/auth.ts";

describe("SystemApp", () => {
  it("renders system page heading and services list", () => {
    const fetchFn: FetchFn = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      headers: new Headers({ "content-type": "application/json" }),
      json: async () => ({
        timestamp: "2026-10-11T12:00:00Z",
        services: [{ name: "hub", up: true, latency_ms: 10 }],
      }),
    });

    render(<SystemApp metricsOptions={{ fetchFn }} />);

    expect(screen.getByRole("heading", { level: 1, name: "Panel del sistema" })).toBeDefined();
    expect(screen.getByRole("heading", { level: 2, name: "Servicios" })).toBeDefined();
  });
});

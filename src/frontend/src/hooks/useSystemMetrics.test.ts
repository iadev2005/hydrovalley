// @vitest-environment happy-dom
import { describe, it, expect, vi } from "vitest";
import { renderHook, act } from "@testing-library/react";
import { useSystemMetrics } from "./useSystemMetrics.ts";
import type { FetchFn } from "../logic/auth.ts";

const MOCK_BODY = {
  generated_at: "2026-10-03T12:00:00.000Z",
  services: [
    { name: "nginx", status: "up" },
    { name: "api", status: "up" },
    { name: "hub", status: "up" },
    { name: "world", status: "up" },
    { name: "persistence", status: "up" },
  ],
  hub: null,
};

function fakeFetch(status: number, body?: unknown, headers: Record<string, string> = {}): FetchFn {
  return async () => ({
    status,
    ok: status >= 200 && status < 300,
    headers: { get: (name: string) => headers[name.toLowerCase()] ?? null },
    json: async () => {
      if (body === undefined) throw new SyntaxError("no body");
      return body;
    },
  });
}

describe("useSystemMetrics", () => {
  it("fetches system metrics on mount", async () => {
    const fetchFn = fakeFetch(200, MOCK_BODY);
    const { result } = renderHook(() => useSystemMetrics({ fetchFn }));

    await act(async () => {
      await Promise.resolve();
    });

    expect(result.current.metrics?.services.length).toBe(5);
    expect(result.current.error).toBeNull();
    expect(result.current.lastUpdated).toContain("Última actualización:");
  });

  it("handles offline error", async () => {
    const fetchFn = fakeFetch(503, {});
    const { result } = renderHook(() => useSystemMetrics({ fetchFn }));

    await act(async () => {
      await Promise.resolve();
    });

    expect(result.current.metrics).toBeNull();
    expect(result.current.error).toBe("sin conexión con la API");
  });

  it("polls periodically", async () => {
    vi.useFakeTimers();
    let callCount = 0;
    const fetchFn: FetchFn = async () => {
      callCount += 1;
      return {
        status: 200,
        ok: true,
        headers: { get: () => null },
        json: async () => MOCK_BODY,
      };
    };

    renderHook(() => useSystemMetrics({ fetchFn }));

    await act(async () => {
      await Promise.resolve();
    });
    expect(callCount).toBe(1);

    await act(async () => {
      vi.advanceTimersByTime(2000);
      await Promise.resolve();
    });
    expect(callCount).toBe(2);

    vi.useRealTimers();
  });
});

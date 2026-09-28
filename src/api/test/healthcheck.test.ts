import { describe, expect, it, vi } from "vitest";
import { healthUrl, probeHealth } from "../src/health-probe.js";

describe("healthUrl", () => {
  it.each([
    ["0.0.0.0", 3000, "http://127.0.0.1:3000/api/health"],
    ["::", 3000, "http://127.0.0.1:3000/api/health"],
    ["10.68.17.1", 4000, "http://10.68.17.1:4000/api/health"],
    ["::1", 3000, "http://[::1]:3000/api/health"],
    ["fe80::1", 8080, "http://[fe80::1]:8080/api/health"],
  ])("maps host %s port %d to %s", (host, port, expected) => {
    expect(healthUrl(host, port)).toBe(expected);
  });
});

describe("probeHealth", () => {
  const config = { host: "0.0.0.0", port: 3000 };

  it("returns 0 on a 2xx response and calls the loopback URL", async () => {
    const fetchFn = vi.fn(() => Promise.resolve(new Response("{}", { status: 200 })));
    await expect(probeHealth(config, { fetch: fetchFn })).resolves.toBe(0);
    expect(fetchFn).toHaveBeenCalledWith(
      "http://127.0.0.1:3000/api/health",
      expect.objectContaining({ signal: expect.any(AbortSignal) as unknown }),
    );
  });

  it.each([404, 500, 503])("returns 1 on a %d response", async (status) => {
    const fetchFn = () => Promise.resolve(new Response("", { status }));
    await expect(probeHealth(config, { fetch: fetchFn })).resolves.toBe(1);
  });

  it("returns 1 on a network error", async () => {
    const fetchFn = () => Promise.reject(new TypeError("fetch failed"));
    await expect(probeHealth(config, { fetch: fetchFn })).resolves.toBe(1);
  });

  it("returns 1 when the server does not answer within the timeout", async () => {
    // Nunca responde por sí solo: solo termina cuando se aborta la señal.
    const fetchFn = (_url: string, init: { signal: AbortSignal }) =>
      new Promise<Response>((_resolve, reject) => {
        init.signal.addEventListener("abort", () => {
          reject(new DOMException("timed out", "TimeoutError"));
        });
      });
    const started = Date.now();
    await expect(probeHealth(config, { fetch: fetchFn, timeoutMs: 20 })).resolves.toBe(1);
    expect(Date.now() - started).toBeLessThan(1000);
  });
});

import { describe, it, expect } from "vitest";
import {
  createPollBackoff,
  describeSystemError,
  formatDecimal,
  loadSystem,
  POLL_MS,
  SYSTEM_URL,
  toSystemView,
} from "./system.ts";
import type { FetchFn } from "./auth.ts";

const HUB = {
  tick: "1234",
  paused: false,
  breaker: "closed",
  queue: 12,
  sessions: 6,
  goroutines: 25,
  actions_per_second: { admitted: 1.1, applied: 1, window_seconds: 10 },
  latency_ms: { p50: 77.8, p95: 107.3, samples: 11 },
  step_ms: { last: 2.5, p95: 2.57, failures_total: 3 },
  humidity_total: "18446744073709551615",
  humidity_contributed: "99",
  accepted_drops: "1234567",
  active_fires: 2,
  boot_id: "x",
  generated_at: "2026-10-03T12:00:00Z",
};

const BODY = {
  generated_at: "2026-10-03T12:00:00.000Z",
  services: [
    { name: "nginx", status: "up" },
    { name: "api", status: "up" },
    { name: "hub", status: "up" },
    { name: "world", status: "down", detail: "unavailable" },
    { name: "persistence", status: "up" },
  ],
  hub: HUB,
};

interface CallRecord {
  url: string;
  init?: RequestInit;
}

function fakeFetch(
  status: number,
  body?: unknown,
  headers: Record<string, string> = {},
): FetchFn & { calls: CallRecord[] } {
  const calls: CallRecord[] = [];
  const fn: FetchFn = async (url: string, init?: RequestInit) => {
    calls.push({ url, init });
    return {
      status,
      ok: status >= 200 && status < 300,
      headers: { get: (name: string) => headers[name.toLowerCase()] ?? null },
      json: async () => {
        if (body === undefined) throw new SyntaxError("no body");
        return body;
      },
    };
  };
  return Object.assign(fn, { calls });
}

describe("formatDecimal", () => {
  it("shows one decimal with a comma and groups thousands", () => {
    expect(formatDecimal(77.84)).toBe("77,8");
    expect(formatDecimal(0)).toBe("0,0");
    expect(formatDecimal(1234.56)).toBe("1.234,6");
  });

  it("shows a dash for anything that is not a finite number", () => {
    for (const v of [undefined, null, "1", NaN, Infinity]) {
      expect(formatDecimal(v)).toBe("—");
    }
  });
});

describe("toSystemView", () => {
  it("turns each service into a row with text, not only colour", () => {
    const view = toSystemView(BODY);
    expect(
      view.services.map((s) => [s.name, s.up, s.statusText]),
    ).toEqual([
      ["nginx", true, "En línea"],
      ["api", true, "En línea"],
      ["hub", true, "En línea"],
      ["world", false, "Caído"],
      ["persistence", true, "En línea"],
    ]);
    expect(view.services[3]?.detail).toBe("no disponible");
    expect(view.services[0]?.detail).toBe("");
  });

  it("falls back to the raw detail code and the raw name when unknown", () => {
    const view = toSystemView({
      ...BODY,
      services: [{ name: "extra", status: "down", detail: "weird" }],
    });
    expect(view.services[0]?.label).toBe("extra");
    expect(view.services[0]?.detail).toBe("weird");
  });

  it("formats hub fields; u64 stay exact", () => {
    const { hub } = toSystemView(BODY);
    expect(hub).not.toBeNull();
    if (hub) {
      expect(hub.tick).toBe("1.234");
      expect(hub.sessions).toBe("6");
      expect(hub.goroutines).toBe("25");
      expect(hub.actionsAdmitted).toBe("1,1");
      expect(hub.actionsApplied).toBe("1,0");
      expect(hub.latencyP50).toBe("77,8");
      expect(hub.latencyP95).toBe("107,3");
      expect(hub.stepLast).toBe("2,5");
      expect(hub.stepP95).toBe("2,6");
      expect(hub.stepFailures).toBe("3");
      expect(hub.queue).toBe("12");
      expect(hub.paused).toBe("No");
      expect(hub.breaker).toBe("Cerrado");
      expect(hub.humidityTotal).toBe("18.446.744.073.709.551.615");
      expect(hub.humidityContributed).toBe("99");
      expect(hub.acceptedDrops).toBe("1.234.567");
      expect(hub.activeFires).toBe("2");
    }
  });

  it("describes paused and the breaker states", () => {
    const open = toSystemView({ ...BODY, hub: { ...HUB, paused: true, breaker: "open" } }).hub;
    expect(open).not.toBeNull();
    if (open) {
      expect(open.paused).toBe("Sí");
      expect(open.breaker).toBe("Abierto");
    }
    expect(toSystemView({ ...BODY, hub: { ...HUB, breaker: "half_open" } }).hub?.breaker).toBe("Semiabierto");
  });

  it("keeps the services and no hub when the hub is null", () => {
    const view = toSystemView({ ...BODY, hub: null });
    expect(view.hub).toBeNull();
    expect(view.services.length).toBe(5);
  });

  it("keeps the update time as the API sent it", () => {
    expect(toSystemView(BODY).generatedAt).toBe("2026-10-03T12:00:00.000Z");
  });

  it("shows dashes for missing hub fields instead of throwing", () => {
    const { hub } = toSystemView({ ...BODY, hub: { tick: "1" } });
    expect(hub).not.toBeNull();
    if (hub) {
      expect(hub.tick).toBe("1");
      expect(hub.latencyP50).toBe("—");
      expect(hub.humidityTotal).toBe("—");
      expect(hub.paused).toBe("—");
    }
  });
});

describe("describeSystemError", () => {
  it("honours Retry-After on 429, at least one poll and at most a minute", () => {
    expect(describeSystemError(429, 10)).toEqual({
      kind: "wait",
      message: "demasiadas consultas, reintentando en 10 s",
      waitMs: 10_000,
    });
    expect(describeSystemError(429, 0).waitMs).toBe(POLL_MS);
    expect(describeSystemError(429, 9999).waitMs).toBe(60_000);
  });

  it("waits a default time on 429 without a usable Retry-After", () => {
    expect(describeSystemError(429, undefined).waitMs).toBe(10_000);
  });

  it("reports no connection for network errors and any other status", () => {
    for (const status of [0, 500, 502, 503, 404]) {
      const e = describeSystemError(status);
      expect(e.kind).toBe("offline");
      expect(e.message).toBe("sin conexión con la API");
    }
  });
});

describe("loadSystem", () => {
  it("requests /api/system with GET and returns the view", async () => {
    const fetchFn = fakeFetch(200, BODY);
    const result = await loadSystem(fetchFn);
    expect(fetchFn.calls[0]?.url).toBe(SYSTEM_URL);
    expect(SYSTEM_URL).toBe("/api/system");
    expect(fetchFn.calls[0]?.init?.method).toBe("GET");
    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(result.view.services.length).toBe(5);
    }
  });

  it("maps a 429 with Retry-After to a wait", async () => {
    const result = await loadSystem(fakeFetch(429, {}, { "retry-after": "7" }));
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.error.kind).toBe("wait");
      expect(result.error.waitMs).toBe(7000);
    }
  });

  it("maps a rejected fetch to offline", async () => {
    const result = await loadSystem(async () => {
      throw new TypeError("network");
    });
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.error.kind).toBe("offline");
    }
  });

  it("maps a non-200 answer, invalid JSON and a wrong shape to offline", async () => {
    for (const fetchFn of [
      fakeFetch(503, {}),
      fakeFetch(200, undefined),
      fakeFetch(200, { services: "x" }),
      fakeFetch(200, null),
    ]) {
      const result = await loadSystem(fetchFn);
      expect(result.ok).toBe(false);
      if (!result.ok) {
        expect(result.error.kind).toBe("offline");
      }
    }
  });
});

describe("createPollBackoff", () => {
  it("polls every POLL_MS while it works", () => {
    const b = createPollBackoff();
    expect(POLL_MS).toBe(2000);
    expect(b.next({ ok: true })).toBe(POLL_MS);
    expect(b.next({ ok: true })).toBe(POLL_MS);
  });

  it("doubles after each failure up to 30 s and resets on success", () => {
    const b = createPollBackoff();
    const offline = { ok: false, error: { kind: "offline", waitMs: 0 } };
    const delays = Array.from({ length: 6 }, () => b.next(offline));
    expect(delays).toEqual([4000, 8000, 16_000, 30_000, 30_000, 30_000]);
    expect(b.next({ ok: true })).toBe(POLL_MS);
    expect(b.next(offline)).toBe(4000);
  });

  it("uses the Retry-After wait on 429 without growing the failure streak", () => {
    const b = createPollBackoff();
    expect(b.next({ ok: false, error: { kind: "wait", waitMs: 12_000 } })).toBe(12_000);
    expect(b.next({ ok: false, error: { kind: "offline", waitMs: 0 } })).toBe(4000);
  });
});

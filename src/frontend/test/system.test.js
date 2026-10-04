import { describe, it } from "node:test";
import assert from "node:assert/strict";
import {
  createPollBackoff,
  describeSystemError,
  formatDecimal,
  loadSystem,
  POLL_MS,
  SYSTEM_URL,
  toSystemView,
} from "../js/system.js";

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

function fakeFetch(status, body, headers = {}) {
  const calls = [];
  const fn = async (url, init) => {
    calls.push({ url, init });
    return {
      status,
      ok: status >= 200 && status < 300,
      headers: { get: (name) => headers[name.toLowerCase()] ?? null },
      json: async () => {
        if (body === undefined) throw new SyntaxError("no body");
        return body;
      },
    };
  };
  fn.calls = calls;
  return fn;
}

describe("formatDecimal", () => {
  it("shows one decimal with a comma and groups thousands", () => {
    assert.equal(formatDecimal(77.84), "77,8");
    assert.equal(formatDecimal(0), "0,0");
    assert.equal(formatDecimal(1234.56), "1.234,6");
  });

  it("shows a dash for anything that is not a finite number", () => {
    for (const v of [undefined, null, "1", NaN, Infinity]) assert.equal(formatDecimal(v), "—", String(v));
  });
});

describe("toSystemView", () => {
  it("turns each service into a row with text, not only colour", () => {
    const view = toSystemView(BODY);
    assert.deepEqual(
      view.services.map((s) => [s.name, s.up, s.statusText]),
      [
        ["nginx", true, "En línea"],
        ["api", true, "En línea"],
        ["hub", true, "En línea"],
        ["world", false, "Caído"],
        ["persistence", true, "En línea"],
      ],
    );
    assert.equal(view.services[3].detail, "no disponible");
    assert.equal(view.services[0].detail, "");
  });

  it("falls back to the raw detail code and the raw name when unknown", () => {
    const view = toSystemView({ ...BODY, services: [{ name: "extra", status: "down", detail: "weird" }] });
    assert.equal(view.services[0].label, "extra");
    assert.equal(view.services[0].detail, "weird");
  });

  it("formats hub fields; u64 stay exact", () => {
    const { hub } = toSystemView(BODY);
    assert.equal(hub.tick, "1.234");
    assert.equal(hub.sessions, "6");
    assert.equal(hub.goroutines, "25");
    assert.equal(hub.actionsAdmitted, "1,1");
    assert.equal(hub.actionsApplied, "1,0");
    assert.equal(hub.latencyP50, "77,8");
    assert.equal(hub.latencyP95, "107,3");
    assert.equal(hub.stepLast, "2,5");
    assert.equal(hub.stepP95, "2,6");
    assert.equal(hub.stepFailures, "3");
    assert.equal(hub.queue, "12");
    assert.equal(hub.paused, "No");
    assert.equal(hub.breaker, "Cerrado");
    assert.equal(hub.humidityTotal, "18.446.744.073.709.551.615");
    assert.equal(hub.humidityContributed, "99");
    assert.equal(hub.acceptedDrops, "1.234.567");
    assert.equal(hub.activeFires, "2");
  });

  it("describes paused and the breaker states", () => {
    const open = toSystemView({ ...BODY, hub: { ...HUB, paused: true, breaker: "open" } }).hub;
    assert.equal(open.paused, "Sí");
    assert.equal(open.breaker, "Abierto");
    assert.equal(toSystemView({ ...BODY, hub: { ...HUB, breaker: "half_open" } }).hub.breaker, "Semiabierto");
  });

  it("keeps the services and no hub when the hub is null", () => {
    const view = toSystemView({ ...BODY, hub: null });
    assert.equal(view.hub, null);
    assert.equal(view.services.length, 5);
  });

  it("keeps the update time as the API sent it", () => {
    assert.equal(toSystemView(BODY).generatedAt, "2026-10-03T12:00:00.000Z");
  });

  it("shows dashes for missing hub fields instead of throwing", () => {
    const { hub } = toSystemView({ ...BODY, hub: { tick: "1" } });
    assert.equal(hub.tick, "1");
    assert.equal(hub.latencyP50, "—");
    assert.equal(hub.humidityTotal, "—");
    assert.equal(hub.paused, "—");
  });
});

describe("describeSystemError", () => {
  it("honours Retry-After on 429, at least one poll and at most a minute", () => {
    assert.deepEqual(describeSystemError(429, 10), {
      kind: "wait",
      message: "demasiadas consultas, reintentando en 10 s",
      waitMs: 10_000,
    });
    assert.equal(describeSystemError(429, 0).waitMs, POLL_MS);
    assert.equal(describeSystemError(429, 9999).waitMs, 60_000);
  });

  it("waits a default time on 429 without a usable Retry-After", () => {
    assert.equal(describeSystemError(429, undefined).waitMs, 10_000);
  });

  it("reports no connection for network errors and any other status", () => {
    for (const status of [0, 500, 502, 503, 404]) {
      const e = describeSystemError(status);
      assert.equal(e.kind, "offline", String(status));
      assert.equal(e.message, "sin conexión con la API");
    }
  });
});

describe("loadSystem", () => {
  it("requests /api/system with GET and returns the view", async () => {
    const fetchFn = fakeFetch(200, BODY);
    const result = await loadSystem(fetchFn);
    assert.equal(fetchFn.calls[0].url, SYSTEM_URL);
    assert.equal(SYSTEM_URL, "/api/system");
    assert.equal(fetchFn.calls[0].init.method, "GET");
    assert.equal(result.ok, true);
    assert.equal(result.view.services.length, 5);
  });

  it("maps a 429 with Retry-After to a wait", async () => {
    const result = await loadSystem(fakeFetch(429, {}, { "retry-after": "7" }));
    assert.equal(result.ok, false);
    assert.equal(result.error.kind, "wait");
    assert.equal(result.error.waitMs, 7000);
  });

  it("maps a rejected fetch to offline", async () => {
    const result = await loadSystem(async () => {
      throw new TypeError("network");
    });
    assert.equal(result.ok, false);
    assert.equal(result.error.kind, "offline");
  });

  it("maps a non-200 answer, invalid JSON and a wrong shape to offline", async () => {
    for (const fetchFn of [fakeFetch(503, {}), fakeFetch(200, undefined), fakeFetch(200, { services: "x" }), fakeFetch(200, null)]) {
      const result = await loadSystem(fetchFn);
      assert.equal(result.ok, false);
      assert.equal(result.error.kind, "offline");
    }
  });
});

describe("createPollBackoff", () => {
  it("polls every POLL_MS while it works", () => {
    const b = createPollBackoff();
    assert.equal(POLL_MS, 2000);
    assert.equal(b.next({ ok: true }), POLL_MS);
    assert.equal(b.next({ ok: true }), POLL_MS);
  });

  it("doubles after each failure up to 30 s and resets on success", () => {
    const b = createPollBackoff();
    const offline = { ok: false, error: { kind: "offline", waitMs: 0 } };
    const delays = Array.from({ length: 6 }, () => b.next(offline));
    assert.deepEqual(delays, [4000, 8000, 16_000, 30_000, 30_000, 30_000]);
    assert.equal(b.next({ ok: true }), POLL_MS);
    assert.equal(b.next(offline), 4000);
  });

  it("uses the Retry-After wait on 429 without growing the failure streak", () => {
    const b = createPollBackoff();
    assert.equal(b.next({ ok: false, error: { kind: "wait", waitMs: 12_000 } }), 12_000);
    assert.equal(b.next({ ok: false, error: { kind: "offline", waitMs: 0 } }), 4000);
  });
});

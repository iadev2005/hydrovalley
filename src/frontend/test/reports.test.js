import { describe, it } from "node:test";
import assert from "node:assert/strict";
import {
  buildRankingRequest,
  buildReportRequest,
  describeReadError,
  formatU64,
  loadRanking,
  loadReport,
  REFRESH_MS,
  toRankingView,
  toReportView,
} from "../js/reports.js";

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

describe("formatU64", () => {
  it("groups digits of a u64 string without losing precision", () => {
    assert.equal(formatU64("0"), "0");
    assert.equal(formatU64("1234567"), "1.234.567");
    assert.equal(formatU64("18446744073709551615"), "18.446.744.073.709.551.615");
  });

  it("shows a dash for anything that is not an unsigned integer string", () => {
    for (const v of [undefined, null, "", "-1", "1.5", "abc", 12, {}]) {
      assert.equal(formatU64(v), "—", String(v));
    }
  });
});

describe("request builders", () => {
  it("builds an authenticated ranking request with the bearer token", () => {
    const req = buildRankingRequest("tok", 10);
    assert.equal(req.url, "/api/ranking?limit=10");
    assert.equal(req.init.headers.Authorization, "Bearer tok");
    assert.equal(req.init.method, "GET");
  });

  it("builds report requests for both scopes and rejects unknown ones", () => {
    assert.equal(buildReportRequest("t", "me").url, "/api/report?scope=me");
    assert.equal(buildReportRequest("t", "global").url, "/api/report?scope=global");
    assert.equal(
      buildReportRequest("t", "me", { fromTick: "50", toTick: "100" }).url,
      "/api/report?scope=me&from_tick=50&to_tick=100",
    );
    assert.throws(() => buildReportRequest("t", "other"));
    assert.throws(() => buildReportRequest("t", "me", { fromTick: "1x" }));
  });

  it("refreshes every 10 seconds", () => {
    assert.equal(REFRESH_MS, 10_000);
  });
});

describe("toRankingView", () => {
  const body = {
    tick: "1200",
    entries: [
      { rank: 1, player_id: "p1", username: "Ana", leaves_earned: "1500" },
      { rank: 2, player_id: "p2", username: "Beto", leaves_earned: "7" },
    ],
  };

  it("maps entries and highlights the current player ignoring case", () => {
    const view = toRankingView(body, "ana");
    assert.equal(view.tick, "1.200");
    assert.deepEqual(view.rows, [
      { rank: 1, username: "Ana", leaves: "1.500", isMe: true },
      { rank: 2, username: "Beto", leaves: "7", isMe: false },
    ]);
  });

  it("keeps at most ten rows and tolerates a missing list", () => {
    const many = { tick: "1", entries: Array.from({ length: 15 }, (_, i) => ({ rank: i + 1, username: `u${i}`, leaves_earned: "0" })) };
    assert.equal(toRankingView(many, "").rows.length, 10);
    assert.deepEqual(toRankingView({ tick: "1" }, "x").rows, []);
  });
});

describe("toReportView", () => {
  const body = {
    scope: "me",
    epoch: 2,
    from_tick: "0",
    to_tick: "150",
    totals: {
      water_reduced: "10",
      water_reused: "2000",
      water_recycled: "3",
      water_refilled: "40",
      water_spent: "6",
      leaves_earned: "5",
      leaves_spent: "0",
    },
  };

  it("labels every counter with its unit", () => {
    const view = toReportView(body);
    assert.equal(view.period, "Época 2 · ticks 0 a 150");
    const byKey = Object.fromEntries(view.rows.map((r) => [r.key, r]));
    assert.deepEqual(byKey.water_reduced, { key: "water_reduced", label: "Agua reducida", value: "10 pts de humedad" });
    assert.equal(byKey.water_reused.value, "2.000 pts de humedad");
    assert.equal(byKey.water_recycled.value, "3 L");
    assert.equal(byKey.water_refilled.value, "40 L");
    assert.equal(byKey.water_spent.value, "6 L");
    assert.equal(byKey.leaves_earned.value, "5");
    assert.equal(byKey.leaves_spent.value, "0");
    assert.equal(view.rows.length, 7);
    assert.match(view.note, /equivalencia entre litros y humedad está pendiente/);
  });
});

describe("describeReadError", () => {
  it("sends the player back to the login panel on 401", () => {
    assert.deepEqual(describeReadError(401), { kind: "session", message: "la sesión caducó, vuelve a entrar", waitMs: 0 });
  });

  it("waits Retry-After on 429, defaulting and capping", () => {
    assert.deepEqual(describeReadError(429, 7), { kind: "wait", message: "demasiadas consultas, espera 7 s", waitMs: 7000 });
    assert.equal(describeReadError(429).waitMs, 10_000);
    assert.equal(describeReadError(429, 100_000).waitMs, 600_000);
  });

  it("says unavailable on 503 and on network failure", () => {
    assert.equal(describeReadError(503).message, "no disponible");
    assert.equal(describeReadError(503).kind, "unavailable");
    assert.equal(describeReadError(0).kind, "unavailable");
  });

  it("explains 404 and unexpected statuses", () => {
    assert.equal(describeReadError(404).kind, "empty");
    assert.match(describeReadError(404).message, /todavía no hay datos/);
    assert.equal(describeReadError(500).kind, "error");
  });
});

describe("loadRanking / loadReport", () => {
  it("returns a view on success", async () => {
    const f = fakeFetch(200, { tick: "5", entries: [{ rank: 1, player_id: "p", username: "Ana", leaves_earned: "3" }] });
    const result = await loadRanking(f, "tok", "Ana");
    assert.equal(result.ok, true);
    assert.equal(result.view.rows[0].isMe, true);
    assert.equal(f.calls[0].init.headers.Authorization, "Bearer tok");
  });

  it("returns the mapped error with Retry-After on 429", async () => {
    const result = await loadRanking(fakeFetch(429, {}, { "retry-after": "3" }), "tok", "Ana");
    assert.equal(result.ok, false);
    assert.equal(result.error.waitMs, 3000);
  });

  it("maps a thrown fetch to unavailable and a bad body to an error", async () => {
    const boom = async () => {
      throw new TypeError("network");
    };
    assert.equal((await loadReport(boom, "tok", "me")).error.kind, "unavailable");
    assert.equal((await loadReport(fakeFetch(200, undefined), "tok", "me")).error.kind, "error");
  });

  it("loads a report view", async () => {
    const f = fakeFetch(200, {
      scope: "global",
      epoch: 1,
      from_tick: "0",
      to_tick: "50",
      totals: { water_reduced: "0", water_reused: "0", water_recycled: "0", water_refilled: "0", water_spent: "1", leaves_earned: "0", leaves_spent: "0" },
    });
    const result = await loadReport(f, "tok", "global");
    assert.equal(result.ok, true);
    assert.equal(f.calls[0].url, "/api/report?scope=global");
  });
});

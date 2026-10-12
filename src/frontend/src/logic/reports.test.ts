import { describe, it, expect } from "vitest";
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
  type ReportScope,
} from "./reports.ts";
import type { FetchFn } from "./auth.ts";

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

describe("formatU64", () => {
  it("groups digits of a u64 string without losing precision", () => {
    expect(formatU64("0")).toBe("0");
    expect(formatU64("1234567")).toBe("1.234.567");
    expect(formatU64("18446744073709551615")).toBe("18.446.744.073.709.551.615");
  });

  it("shows a dash for anything that is not an unsigned integer string", () => {
    for (const v of [undefined, null, "", "-1", "1.5", "abc", 12, {}]) {
      expect(formatU64(v)).toBe("—");
    }
  });
});

describe("request builders", () => {
  it("builds an authenticated ranking request with the bearer token", () => {
    const req = buildRankingRequest("tok", 10);
    expect(req.url).toBe("/api/ranking?limit=10");
    expect((req.init.headers as Record<string, string>)?.["Authorization"]).toBe("Bearer tok");
    expect(req.init.method).toBe("GET");
  });

  it("builds report requests for both scopes and rejects unknown ones", () => {
    expect(buildReportRequest("t", "me").url).toBe("/api/report?scope=me");
    expect(buildReportRequest("t", "global").url).toBe("/api/report?scope=global");
    expect(
      buildReportRequest("t", "me", { fromTick: "50", toTick: "100" }).url,
    ).toBe("/api/report?scope=me&from_tick=50&to_tick=100");
    expect(() => buildReportRequest("t", "other")).toThrow();
    expect(() => buildReportRequest("t", "me", { fromTick: "1x" })).toThrow();
  });

  it("refreshes every 10 seconds", () => {
    expect(REFRESH_MS).toBe(10_000);
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
    expect(view.tick).toBe("1.200");
    expect(view.rows).toEqual([
      { rank: 1, username: "Ana", leaves: "1.500", isMe: true },
      { rank: 2, username: "Beto", leaves: "7", isMe: false },
    ]);
  });

  it("keeps at most ten rows and tolerates a missing list", () => {
    const many = {
      tick: "1",
      entries: Array.from({ length: 15 }, (_, i) => ({
        rank: i + 1,
        username: `u${i}`,
        leaves_earned: "0",
      })),
    };
    expect(toRankingView(many, "").rows.length).toBe(10);
    expect(toRankingView({ tick: "1" }, "x").rows).toEqual([]);
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
    expect(view.period).toBe("Época 2 · ticks 0 a 150");
    const byKey = Object.fromEntries(view.rows.map((r) => [r.key, r]));
    expect(byKey["water_reduced"]).toEqual({
      key: "water_reduced",
      label: "Agua reducida",
      value: "10 pts de humedad",
    });
    expect(byKey["water_reused"]?.value).toBe("2.000 pts de humedad");
    expect(byKey["water_recycled"]?.value).toBe("3 L");
    expect(byKey["water_refilled"]?.value).toBe("40 L");
    expect(byKey["water_spent"]?.value).toBe("6 L");
    expect(byKey["leaves_earned"]?.value).toBe("5");
    expect(byKey["leaves_spent"]?.value).toBe("0");
    expect(view.rows.length).toBe(7);
    expect(view.note).toMatch(/equivalencia entre litros y humedad está pendiente/);
  });
});

describe("describeReadError", () => {
  it("sends the player back to the login panel on 401", () => {
    expect(describeReadError(401)).toEqual({
      kind: "session",
      message: "la sesión caducó, vuelve a entrar",
      waitMs: 0,
    });
  });

  it("waits Retry-After on 429, defaulting and capping", () => {
    expect(describeReadError(429, 7)).toEqual({
      kind: "wait",
      message: "demasiadas consultas, espera 7 s",
      waitMs: 7000,
    });
    expect(describeReadError(429).waitMs).toBe(10_000);
    expect(describeReadError(429, 100_000).waitMs).toBe(600_000);
  });

  it("says unavailable on 503 and on network failure", () => {
    expect(describeReadError(503).message).toBe("no disponible");
    expect(describeReadError(503).kind).toBe("unavailable");
    expect(describeReadError(0).kind).toBe("unavailable");
  });

  it("explains 404 and unexpected statuses", () => {
    expect(describeReadError(404).kind).toBe("empty");
    expect(describeReadError(404).message).toMatch(/todavía no hay datos/);
    expect(describeReadError(500).kind).toBe("error");
  });
});

describe("loadRanking / loadReport", () => {
  it("returns a view on success", async () => {
    const f = fakeFetch(200, {
      tick: "5",
      entries: [{ rank: 1, player_id: "p", username: "Ana", leaves_earned: "3" }],
    });
    const result = await loadRanking(f, "tok", "Ana");
    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(result.view.rows[0]?.isMe).toBe(true);
    }
    expect((f.calls[0]?.init?.headers as Record<string, string>)?.["Authorization"]).toBe("Bearer tok");
  });

  it("returns the mapped error with Retry-After on 429", async () => {
    const result = await loadRanking(fakeFetch(429, {}, { "retry-after": "3" }), "tok", "Ana");
    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.error.waitMs).toBe(3000);
    }
  });

  it("maps a thrown fetch to unavailable and a bad body to an error", async () => {
    const boom: FetchFn = async () => {
      throw new TypeError("network");
    };
    const res1 = await loadReport(boom, "tok", "me");
    expect(res1.ok).toBe(false);
    if (!res1.ok) {
      expect(res1.error.kind).toBe("unavailable");
    }
    const res2 = await loadReport(fakeFetch(200, undefined), "tok", "me");
    expect(res2.ok).toBe(false);
    if (!res2.ok) {
      expect(res2.error.kind).toBe("error");
    }
  });

  it("loads a report view", async () => {
    const f = fakeFetch(200, {
      scope: "global",
      epoch: 1,
      from_tick: "0",
      to_tick: "50",
      totals: {
        water_reduced: "0",
        water_reused: "0",
        water_recycled: "0",
        water_refilled: "0",
        water_spent: "1",
        leaves_earned: "0",
        leaves_spent: "0",
      },
    });
    const result = await loadReport(f, "tok", "global" as ReportScope);
    expect(result.ok).toBe(true);
    expect(f.calls[0]?.url).toBe("/api/report?scope=global");
  });
});

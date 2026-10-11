// @vitest-environment happy-dom
import { describe, it, expect, vi } from "vitest";
import { renderHook, act } from "@testing-library/react";
import { useReports } from "./useReports.ts";
import type { FetchFn, Session } from "../logic/auth.ts";

const session: Session = {
  token: "tok",
  expiresAt: Date.now() + 100_000,
  username: "Ana",
};

const RANKING_BODY = {
  tick: "100",
  entries: [{ rank: 1, player_id: "p1", username: "Ana", leaves_earned: "50" }],
};

const REPORT_BODY = {
  scope: "me",
  epoch: 1,
  from_tick: "0",
  to_tick: "100",
  totals: {
    water_reduced: "10",
    water_reused: "0",
    water_recycled: "5",
    water_refilled: "0",
    water_spent: "2",
    leaves_earned: "50",
    leaves_spent: "0",
  },
};

function fakeFetch(status: number, ranking = RANKING_BODY, report = REPORT_BODY): FetchFn {
  return async (url: string) => {
    return {
      status,
      ok: status >= 200 && status < 300,
      headers: { get: () => null },
      json: async () => {
        if (url.includes("/api/ranking")) return ranking;
        return report;
      },
    };
  };
}

describe("useReports", () => {
  it("does not fetch if session is null", () => {
    const fetchFn = vi.fn();
    const { result } = renderHook(() => useReports({ session: null, fetchFn: fetchFn as unknown as FetchFn }));
    expect(result.current.ranking).toBeNull();
    expect(result.current.report).toBeNull();
    expect(fetchFn).not.toHaveBeenCalled();
  });

  it("fetches ranking and report when session is provided", async () => {
    const fetchFn = fakeFetch(200);
    const { result } = renderHook(() => useReports({ session, fetchFn }));

    await act(async () => {
      await Promise.resolve();
    });

    expect(result.current.ranking?.rows[0]?.username).toBe("Ana");
    expect(result.current.ranking?.rows[0]?.isMe).toBe(true);
    expect(result.current.report?.scope).toBe("me");
  });

  it("changes scope and re-fetches", async () => {
    const fetchFn = fakeFetch(200);
    const { result } = renderHook(() => useReports({ session, fetchFn }));

    await act(async () => {
      await Promise.resolve();
    });

    await act(async () => {
      result.current.setReportScope("global");
    });

    expect(result.current.reportScope).toBe("global");
  });

  it("notifies session expired on 401", async () => {
    const onSessionExpired = vi.fn();
    const fetchFn = fakeFetch(401);
    renderHook(() => useReports({ session, fetchFn, onSessionExpired }));

    await act(async () => {
      await Promise.resolve();
    });

    expect(onSessionExpired).toHaveBeenCalled();
  });
});

import { Writable } from "node:stream";
import type { FastifyInstance } from "fastify";
import { SignJWT } from "jose";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { buildApp } from "../src/app.js";
import { READ_LIMIT_PER_IP } from "../src/limits.js";
import { createAttemptLimiter } from "../src/rate-limit.js";
import {
  SourceInvalidRequestError,
  SourceNotFoundError,
  SourceUnavailableError,
} from "../src/source-errors.js";
import {
  createTokenService,
  createTokenVerifier,
  TOKEN_AUDIENCE,
  TOKEN_ISSUER,
} from "../src/tokens.js";
import {
  FakeAccountDirectory,
  FakeRankingSource,
  FakeReportSource,
  unavailable,
} from "./support/fakes.js";
import { makeKeyPair } from "./support/keys.js";

const keys = makeKeyPair();
const NOW = Date.UTC(2026, 9, 1, 12, 0, 0);
const MAX_U64 = 18_446_744_073_709_551_615n;
const ME = "11111111-1111-4111-8111-111111111111";
const OTHER = "22222222-2222-4222-8222-222222222222";

describe("ranking and report routes", () => {
  let app: FastifyInstance;
  let accounts: FakeAccountDirectory;
  let ranking: FakeRankingSource;
  let reports: FakeReportSource;
  let logs: string[];
  let token: string;

  async function setup(readLimit = 1000) {
    accounts = new FakeAccountDirectory();
    ranking = new FakeRankingSource();
    reports = new FakeReportSource();
    logs = [];
    const stream = new Writable({
      write(chunk: Buffer, _enc, done) {
        logs.push(chunk.toString());
        done();
      },
    });
    app = await buildApp({ logger: { level: "debug", stream } }, undefined, {
      ranking,
      reports,
      accounts,
      tokens: createTokenVerifier({
        publicKey: keys.publicKey,
        now: () => NOW,
      }),
      readLimiter: createAttemptLimiter({
        limit: readLimit,
        windowMs: 60_000,
        now: () => NOW,
      }),
    });
    token = (
      await createTokenService({
        privateKey: keys.privateKey,
        ttlSeconds: 3600,
        now: () => NOW,
      }).issue({
        playerId: ME,
        username: "ana",
        role: "player",
      })
    ).token;
  }

  beforeEach(async () => {
    await setup();
  });
  afterEach(async () => {
    await app.close();
  });

  /** `null` envía la petición sin cabecera de autorización. */
  const get = (url: string, bearer: string | null = token) =>
    app.inject({
      method: "GET",
      url,
      ...(bearer === null ? {} : { headers: { authorization: `Bearer ${bearer}` } }),
    });

  const ROUTES = ["/api/ranking", "/api/report", "/api/report?scope=global"];

  describe("authentication", () => {
    it.each(ROUTES)("%s answers 401 without a token", async (url) => {
      const res = await get(url, null);
      expect(res.statusCode).toBe(401);
      expect(res.json<{ error: string }>().error).toEqual(expect.any(String));
      expect(res.headers["www-authenticate"]).toMatch(/^Bearer/);
    });

    it.each(ROUTES)("%s answers 401 with a malformed header or a bad token", async (url) => {
      const basic = await app.inject({
        method: "GET",
        url,
        headers: { authorization: "Basic abc" },
      });
      expect(basic.statusCode).toBe(401);
      expect((await get(url, "garbage")).statusCode).toBe(401);

      const expired = await createTokenService({
        privateKey: keys.privateKey,
        ttlSeconds: 60,
        now: () => NOW - 3_600_000,
      }).issue({ playerId: ME, username: "ana", role: "player" });
      expect((await get(url, expired.token)).statusCode).toBe(401);

      const wrongAudience = await new SignJWT({
        player_id: ME,
        username: "ana",
        role: "player",
      })
        .setProtectedHeader({ alg: "EdDSA" })
        .setIssuer(TOKEN_ISSUER)
        .setAudience(`${TOKEN_AUDIENCE}-x`)
        .setIssuedAt(NOW / 1000)
        .setExpirationTime(NOW / 1000 + 60)
        .sign(keys.privateKey);
      expect((await get(url, wrongAudience)).statusCode).toBe(401);
    });

    it("never reaches the sources without a valid token", async () => {
      await get("/api/ranking", null);
      await get("/api/report", "garbage");
      expect(ranking.limits).toEqual([]);
      expect(reports.queries).toEqual([]);
    });
  });

  describe("GET /api/ranking", () => {
    beforeEach(() => {
      ranking.ranking = {
        tick: 900n,
        entries: [
          { rank: 1, playerId: ME, leavesEarned: MAX_U64 },
          { rank: 2, playerId: OTHER, leavesEarned: 7n },
          {
            rank: 2,
            playerId: "33333333-3333-4333-8333-333333333333",
            leavesEarned: 7n,
          },
        ],
      };
      accounts.accounts.set("ana", {
        playerId: ME,
        username: "Ana",
        passwordHash: "h",
        role: "player",
      });
      accounts.accounts.set("bo", {
        playerId: OTHER,
        username: "Bo",
        passwordHash: "h",
        role: "player",
      });
    });

    it("returns the entries with usernames and u64 values as strings", async () => {
      const res = await get("/api/ranking");

      expect(res.statusCode).toBe(200);
      expect(res.json()).toEqual({
        tick: "900",
        entries: [
          {
            rank: 1,
            player_id: ME,
            username: "Ana",
            leaves_earned: "18446744073709551615",
          },
          { rank: 2, player_id: OTHER, username: "Bo", leaves_earned: "7" },
          {
            rank: 2,
            player_id: "33333333-3333-4333-8333-333333333333",
            username: "jugador-33333333",
            leaves_earned: "7",
          },
        ],
      });
    });

    it("asks for 20 entries by default and honours ?limit up to 100", async () => {
      await get("/api/ranking");
      await get("/api/ranking?limit=5");
      await get("/api/ranking?limit=100");

      expect(ranking.limits).toEqual([20, 5, 100]);
    });

    it.each(["0", "101", "abc", "-1", "1.5", "", "0010"])(
      "rejects limit=%j with 400",
      async (value) => {
        const res = await get(`/api/ranking?limit=${value}`);
        expect(res.statusCode).toBe(400);
        expect(ranking.limits).toEqual([]);
      },
    );

    it("looks the usernames up once for the distinct players", async () => {
      await get("/api/ranking");
      expect(accounts.calls.getUsernames).toBe(1);
    });

    it("skips the username lookup for an empty ranking", async () => {
      ranking.ranking = { tick: 1n, entries: [] };
      const res = await get("/api/ranking");

      expect(res.json()).toEqual({ tick: "1", entries: [] });
      expect(accounts.calls.getUsernames).toBe(0);
    });

    it("still answers, with fallback names and a warning, when GetUsernames fails", async () => {
      accounts.getUsernamesError = unavailable();
      const res = await get("/api/ranking");

      expect(res.statusCode).toBe(200);
      const { entries } = res.json<{ entries: { username: string }[] }>();
      expect(entries.map((e) => e.username)).toEqual([
        "jugador-11111111",
        "jugador-22222222",
        "jugador-33333333",
      ]);
      expect(
        logs.some((l) => l.includes('"level":40') && l.includes("username lookup failed")),
      ).toBe(true);
    });

    it("answers 503 when the world is unavailable and 400 when it rejects the request", async () => {
      ranking.error = new SourceUnavailableError();
      expect((await get("/api/ranking")).statusCode).toBe(503);
      ranking.error = new SourceInvalidRequestError();
      expect((await get("/api/ranking")).statusCode).toBe(400);
    });

    it("answers 500 with a generic message on an unexpected error", async () => {
      ranking.error = new Error("secret detail");
      const res = await get("/api/ranking");

      expect(res.statusCode).toBe(500);
      expect(res.body).not.toContain("secret detail");
    });
  });

  describe("GET /api/report", () => {
    beforeEach(() => {
      reports.report = {
        playerId: ME,
        epoch: 3,
        fromTick: 50n,
        toTick: 150n,
        totals: {
          waterReduced: 1n,
          waterReused: 2n,
          waterRecycled: 3n,
          waterRefilled: 4n,
          waterSpent: 5n,
          leavesEarned: MAX_U64,
          leavesSpent: 7n,
        },
      };
    });

    it("defaults to the caller's report, taking the player from the token", async () => {
      const res = await get("/api/report");

      expect(res.statusCode).toBe(200);
      expect(reports.queries).toEqual([{ playerId: ME }]);
      expect(res.json()).toEqual({
        scope: "me",
        player_id: ME,
        epoch: 3,
        from_tick: "50",
        to_tick: "150",
        totals: {
          water_reduced: "1",
          water_reused: "2",
          water_recycled: "3",
          water_refilled: "4",
          water_spent: "5",
          leaves_earned: "18446744073709551615",
          leaves_spent: "7",
        },
      });
    });

    it("scope=global sends no player and omits player_id", async () => {
      reports.report = { ...reports.report, playerId: "" };
      const res = await get("/api/report?scope=global");

      expect(reports.queries).toEqual([{}]);
      expect(res.json()).toMatchObject({ scope: "global" });
      expect(res.json()).not.toHaveProperty("player_id");
    });

    it("passes the period as bigint, including values above 2^53", async () => {
      await get("/api/report?from_tick=50&to_tick=18446744073709551615");

      expect(reports.queries).toEqual([{ playerId: ME, fromTick: 50n, toTick: MAX_U64 }]);
    });

    it.each([
      "scope=everyone",
      "from_tick=abc",
      "from_tick=-1",
      "to_tick=1.5",
      "to_tick=18446744073709551616",
      "from_tick=100&to_tick=50",
      "to_tick=",
    ])("rejects ?%s with 400", async (query) => {
      const res = await get(`/api/report?${query}`);

      expect(res.statusCode).toBe(400);
      expect(reports.queries).toEqual([]);
    });

    it("maps NOT_FOUND to 404, INVALID_ARGUMENT to 400 and UNAVAILABLE to 503", async () => {
      reports.error = new SourceNotFoundError();
      expect((await get("/api/report")).statusCode).toBe(404);
      reports.error = new SourceInvalidRequestError();
      expect((await get("/api/report")).statusCode).toBe(400);
      reports.error = new SourceUnavailableError();
      expect((await get("/api/report")).statusCode).toBe(503);
    });

    it("answers 500 with a generic message on an unexpected error", async () => {
      reports.error = new Error("secret detail");
      const res = await get("/api/report");

      expect(res.statusCode).toBe(500);
      expect(res.body).not.toContain("secret detail");
    });
  });

  describe("rate limit", () => {
    it("exposes the agreed constant", () => {
      expect(READ_LIMIT_PER_IP).toEqual({ limit: 60, windowMs: 60_000 });
    });

    it("answers 429 with Retry-After once the IP exceeds the limit, across both routes", async () => {
      await app.close();
      await setup(2);

      expect((await get("/api/ranking")).statusCode).toBe(200);
      expect((await get("/api/report")).statusCode).toBe(200);
      const res = await get("/api/ranking");

      expect(res.statusCode).toBe(429);
      expect(res.headers["retry-after"]).toBe("60");
    });
  });

  it("documents the three schemas in the OpenAPI document", async () => {
    const doc = (await app.inject({ method: "GET", url: "/api/docs/json" })).json<{
      paths: Record<string, unknown>;
    }>();

    expect(Object.keys(doc.paths)).toEqual(expect.arrayContaining(["/api/ranking", "/api/report"]));
  });
});

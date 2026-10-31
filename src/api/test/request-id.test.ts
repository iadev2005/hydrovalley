import { Writable } from "node:stream";
import type { FastifyInstance } from "fastify";
import { afterEach, describe, expect, it } from "vitest";
import { buildApp } from "../src/app.js";
import { WorldService } from "../src/gen/hydrovalley/world/v1/world_pb.js";
import { createGrpcRankingSource } from "../src/grpc-ranking-source.js";
import {
  currentRequestId,
  isValidRequestId,
  REQUEST_ID_HEADER,
  resolveRequestId,
} from "../src/request-id.js";
import { createTokenService, createTokenVerifier } from "../src/tokens.js";
import { createAttemptLimiter } from "../src/rate-limit.js";
import { FakeAccountDirectory, FakeRankingSource, FakeReportSource } from "./support/fakes.js";
import { TestGrpcServer } from "./support/grpc-server.js";
import { makeKeyPair } from "./support/keys.js";

const UUID = "3f2b8c1e-5a47-4d0e-9b6a-1c2d3e4f5a6b";
const HEX32 = "0123456789abcdef0123456789ABCDEF";

describe("request id validation (RNF-07)", () => {
  it.each([UUID, HEX32, UUID.toUpperCase()])("accepts %s", (id) => {
    expect(isValidRequestId(id)).toBe(true);
    expect(resolveRequestId(id)).toBe(id);
  });

  it.each([
    "",
    "abc",
    "not a uuid",
    `${UUID}x`,
    HEX32.slice(1),
    `${HEX32}0`,
    "a".repeat(65),
    "a".repeat(64),
    `${UUID}\r\nX-Evil: 1`,
    "<script>",
  ])("rejects %j and generates a fresh UUID", (id) => {
    expect(isValidRequestId(id)).toBe(false);
    const fresh = resolveRequestId(id);
    expect(fresh).not.toBe(id);
    expect(fresh).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
  });

  it.each([undefined, ["a", "b"], 42])("generates an id for a non-string header %j", (v) => {
    expect(isValidRequestId(v)).toBe(false);
    expect(resolveRequestId(v)).toMatch(/^[0-9a-f-]{36}$/);
  });
});

describe("request id in the HTTP layer and the gRPC clients", () => {
  const server = new TestGrpcServer();
  let created: FastifyInstance | undefined;
  afterEach(async () => {
    await created?.close();
    created = undefined;
    await server.stop();
  });

  const keys = makeKeyPair();
  const NOW = Date.UTC(2026, 9, 1, 12, 0, 0);

  async function setup(ranking = new FakeRankingSource()) {
    const logs: string[] = [];
    const stream = new Writable({
      write(chunk: Buffer, _enc, done) {
        logs.push(chunk.toString());
        done();
      },
    });
    const app = await buildApp({ logger: { level: "info", stream } }, undefined, {
      ranking,
      reports: new FakeReportSource(),
      accounts: new FakeAccountDirectory(),
      tokens: createTokenVerifier({ publicKey: keys.publicKey, now: () => NOW }),
      readLimiter: createAttemptLimiter({ limit: 1000, windowMs: 60_000, now: () => NOW }),
    });
    created = app;
    const token = (
      await createTokenService({ privateKey: keys.privateKey, ttlSeconds: 3600, now: () => NOW }).issue({
        playerId: "11111111-1111-4111-8111-111111111111",
        username: "ana",
        role: "player",
      })
    ).token;
    return { logs, token, app };
  }

  it("echoes a valid incoming id and logs it on the request lines", async () => {
    const { logs, app } = await setup();
    const res = await app.inject({
      method: "GET",
      url: "/api/health",
      headers: { [REQUEST_ID_HEADER]: UUID },
    });
    expect(res.headers[REQUEST_ID_HEADER]).toBe(UUID);
    const lines = logs.map((l) => JSON.parse(l) as { reqId?: string });
    expect(lines.length).toBeGreaterThan(0);
    for (const line of lines) expect(line.reqId).toBe(UUID);
  });

  it("generates an id when the header is missing or invalid, and returns it", async () => {
    const { app } = await setup();
    const missing = await app.inject({ method: "GET", url: "/api/health" });
    const bad = await app.inject({
      method: "GET",
      url: "/api/health",
      headers: { [REQUEST_ID_HEADER]: "x y z" },
    });
    for (const res of [missing, bad]) {
      expect(isValidRequestId(res.headers[REQUEST_ID_HEADER])).toBe(true);
    }
    expect(missing.headers[REQUEST_ID_HEADER]).not.toBe(bad.headers[REQUEST_ID_HEADER]);
  });

  it("returns the id on error responses too", async () => {
    const { app } = await setup();
    const res = await app.inject({
      method: "GET",
      url: "/api/ranking",
      headers: { [REQUEST_ID_HEADER]: UUID },
    });
    expect(res.statusCode).toBe(401);
    expect(res.headers[REQUEST_ID_HEADER]).toBe(UUID);
  });

  it("makes the id available to code called from the route", async () => {
    let seen: string | undefined;
    const ranking = new FakeRankingSource();
    const original = ranking.getRanking.bind(ranking);
    ranking.getRanking = (limit) => {
      seen = currentRequestId();
      return original(limit);
    };
    const { token, app } = await setup(ranking);
    await app.inject({
      method: "GET",
      url: "/api/ranking",
      headers: { authorization: `Bearer ${token}`, [REQUEST_ID_HEADER]: UUID },
    });
    expect(seen).toBe(UUID);
  });

  it("sends the id as x-request-id metadata on gRPC calls made while serving a request", async () => {
    const received: (string | null)[] = [];
    const address = await server.start(WorldService, {
      getRanking: (_req, ctx) => {
        received.push(ctx.requestHeader.get("x-request-id"));
        return { tick: 1n, entries: [] };
      },
    });
    const { token, app } = await setup(
      // Fuente real: la API habla gRPC con el servidor de prueba.
      createGrpcRankingSource({ address }) as unknown as FakeRankingSource,
    );
    await app.inject({
      method: "GET",
      url: "/api/ranking",
      headers: { authorization: `Bearer ${token}`, [REQUEST_ID_HEADER]: UUID },
    });
    expect(received).toEqual([UUID]);
  });

  it("sends no metadata when there is no request in progress", async () => {
    const received: (string | null)[] = [];
    const address = await server.start(WorldService, {
      getRanking: (_req, ctx) => {
        received.push(ctx.requestHeader.get("x-request-id"));
        return { tick: 1n, entries: [] };
      },
    });
    await createGrpcRankingSource({ address }).getRanking(5);
    expect(received).toEqual([null]);
  });
});

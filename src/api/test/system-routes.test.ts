import type { FastifyInstance } from "fastify";
import { afterEach, describe, expect, it } from "vitest";
import { buildApp } from "../src/app.js";
import { SYSTEM_LIMIT_PER_IP } from "../src/limits.js";
import { createAttemptLimiter } from "../src/rate-limit.js";
import type { SystemSnapshot } from "../src/system-status.js";
import { VALID_STATS } from "./support/hub-stats-fixture.js";

const NOW = Date.UTC(2026, 9, 3, 12, 0, 0);

const SNAPSHOT: SystemSnapshot = {
  generated_at: "2026-10-03T12:00:00.000Z",
  services: [
    { name: "nginx", status: "up" },
    { name: "api", status: "up" },
    { name: "hub", status: "up" },
    { name: "world", status: "down", detail: "not_serving" },
    { name: "persistence", status: "up" },
  ],
  hub: VALID_STATS,
};

const opened: FastifyInstance[] = [];
afterEach(async () => {
  await Promise.all(opened.splice(0).map((app) => app.close()));
});

async function setup(
  limit = 1000,
  snapshot: () => Promise<SystemSnapshot> = () => Promise.resolve(SNAPSHOT),
) {
  let calls = 0;
  const app = await buildApp({}, undefined, undefined, undefined, {
    status: {
      get: () => {
        calls++;
        return snapshot();
      },
    },
    limiter: createAttemptLimiter({ limit, windowMs: 60_000, now: () => NOW }),
  });
  opened.push(app);
  return { app, calls: () => calls };
}

describe("GET /api/system", () => {
  it("serializes a hub without restored_tick as null instead of failing", async () => {
    const { app } = await setup(1000, () =>
      Promise.resolve({ ...SNAPSHOT, hub: { ...VALID_STATS, restored_tick: null } }),
    );

    const res = await app.inject({ method: "GET", url: "/api/system" });

    expect(res.statusCode).toBe(200);
    expect(res.json<{ hub: { restored_tick: string | null } }>().hub.restored_tick).toBeNull();
  });

  it("answers 200 with the aggregated snapshot and needs no token", async () => {
    const { app } = await setup();

    const res = await app.inject({ method: "GET", url: "/api/system" });

    expect(res.statusCode).toBe(200);
    expect(res.json()).toEqual(SNAPSHOT);
  });

  it("answers 200 with hub null when the hub is down", async () => {
    const { app } = await setup(1000, () =>
      Promise.resolve({
        ...SNAPSHOT,
        services: SNAPSHOT.services.map((s) =>
          s.name === "hub" ? { name: "hub", status: "down" as const, detail: "timeout" } : s,
        ),
        hub: null,
      }),
    );

    const res = await app.inject({ method: "GET", url: "/api/system" });

    expect(res.statusCode).toBe(200);
    expect(res.json<SystemSnapshot>().hub).toBeNull();
  });

  it("answers 429 with Retry-After once the per-IP limit is exceeded", async () => {
    const { app, calls } = await setup(2);

    for (let i = 0; i < 2; i++) {
      expect((await app.inject({ method: "GET", url: "/api/system" })).statusCode).toBe(200);
    }
    const res = await app.inject({ method: "GET", url: "/api/system" });

    expect(res.statusCode).toBe(429);
    expect(Number(res.headers["retry-after"])).toBeGreaterThan(0);
    expect(res.json()).toHaveProperty("error");
    expect(calls()).toBe(2);
  });

  it("limits each IP separately", async () => {
    const { app } = await setup(1);

    const a = await app.inject({
      method: "GET",
      url: "/api/system",
      remoteAddress: "10.0.0.1",
    });
    const b = await app.inject({
      method: "GET",
      url: "/api/system",
      remoteAddress: "10.0.0.2",
    });

    expect([a.statusCode, b.statusCode]).toEqual([200, 200]);
  });

  it("is documented in OpenAPI", async () => {
    const { app } = await setup();

    const res = await app.inject({ method: "GET", url: "/api/docs/json" });

    const spec = res.json<{
      paths: Record<string, { get?: { tags?: string[] } }>;
    }>();
    expect(spec.paths["/api/system"]?.get?.tags).toEqual(["system"]);
  });

  it("uses 120 requests per minute per IP", () => {
    expect(SYSTEM_LIMIT_PER_IP).toEqual({ limit: 120, windowMs: 60_000 });
  });
});

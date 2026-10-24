import { describe, expect, it } from "vitest";
import { buildApp } from "../src/app.js";
import type { SystemSnapshot } from "../src/system-status.js";
import { createAttemptLimiter } from "../src/rate-limit.js";
import { VALID_STATS } from "./support/hub-stats-fixture.js";

const SNAPSHOT: SystemSnapshot = {
  generated_at: "2026-10-03T12:00:00.000Z",
  services: [
    { name: "nginx", status: "up" },
    { name: "api", status: "up" },
    { name: "hub", status: "up" },
    { name: "world", status: "up" },
    { name: "persistence", status: "up" },
  ],
  hub: VALID_STATS,
};

describe("GET /metrics", () => {
  it("returns prometheus text format metrics with uptime and service statuses", async () => {
    const app = await buildApp({}, undefined, undefined, undefined, {
      status: {
        get: () => Promise.resolve(SNAPSHOT),
      },
      limiter: createAttemptLimiter({ limit: 1000, windowMs: 60_000 }),
    });

    const res = await app.inject({ method: "GET", url: "/metrics" });
    expect(res.statusCode).toBe(200);
    expect(res.headers["content-type"]).toContain("text/plain");
    const body = res.body;
    expect(body).toContain("hydrovalley_api_uptime_seconds");
    expect(body).toContain("hydrovalley_api_memory_rss_bytes");
    expect(body).toContain('hydrovalley_service_status{service="nginx"} 1');
    expect(body).toContain('hydrovalley_service_status{service="api"} 1');
    expect(body).toContain('hydrovalley_service_status{service="hub"} 1');
    expect(body).toContain('hydrovalley_service_status{service="world"} 1');
    expect(body).toContain('hydrovalley_service_status{service="persistence"} 1');
    await app.close();
  });
});

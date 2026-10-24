import { describe, expect, it } from "vitest";
import { buildApp } from "../src/app.js";
import { SNAPSHOT } from "./support/system-snapshot-fixture.js";

describe("GET /metrics", () => {
  it("returns prometheus text format metrics with uptime and service statuses", async () => {
    const app = await buildApp({}, undefined, undefined, undefined, {
      status: {
        get: () => Promise.resolve(SNAPSHOT),
      },
      limiter: {
        hit: () => ({ allowed: true, retryAfterSeconds: 0 }),
      },
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

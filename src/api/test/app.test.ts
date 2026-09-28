import { afterEach, describe, expect, it } from "vitest";
import type { FastifyInstance } from "fastify";
import { buildApp } from "../src/app.js";

describe("api", () => {
  let app: FastifyInstance | undefined;

  afterEach(async () => {
    await app?.close();
    app = undefined;
  });

  it("GET /api/health answers 200 with status ok", async () => {
    app = await buildApp();
    const res = await app.inject({ method: "GET", url: "/api/health" });

    expect(res.statusCode).toBe(200);
    expect(res.headers["content-type"]).toMatch(/^application\/json/);
    expect(res.json()).toEqual({ status: "ok" });
  });

  it("publishes an OpenAPI document that includes /api/health", async () => {
    app = await buildApp();
    const res = await app.inject({ method: "GET", url: "/api/docs/json" });

    expect(res.statusCode).toBe(200);
    const doc = res.json<{ openapi: string; paths: Record<string, unknown> }>();
    expect(doc.openapi).toMatch(/^3\./);
    expect(doc.paths).toHaveProperty("/api/health");
  });

  it("serves the Swagger page at /api/docs", async () => {
    app = await buildApp();
    const res = await app.inject({ method: "GET", url: "/api/docs" });

    // La interfaz puede redirigir a /api/docs/ antes de servir el HTML.
    const page =
      res.statusCode === 302
        ? await app.inject({ method: "GET", url: String(res.headers.location) })
        : res;
    expect(page.statusCode).toBe(200);
    expect(page.headers["content-type"]).toMatch(/^text\/html/);
    expect(page.body).toContain("swagger");
  });

  it("answers 404 in JSON for unknown routes", async () => {
    app = await buildApp();
    const res = await app.inject({ method: "GET", url: "/api/nope" });

    expect(res.statusCode).toBe(404);
    expect(res.json()).toMatchObject({ statusCode: 404 });
  });
});

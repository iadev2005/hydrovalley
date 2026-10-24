import { Code, ConnectError } from "@connectrpc/connect";
import type { FastifyInstance } from "fastify";
import { afterEach, describe, expect, it } from "vitest";
import { buildApp } from "../src/app.js";
import { createCachedCatalog } from "../src/catalog-cache.js";
import type { Catalog, CatalogSource } from "../src/catalog-source.js";
import { CATALOG_CACHE_TTL_SECONDS, CATALOG_LIMIT_PER_IP } from "../src/limits.js";
import { createGrpcCatalogSource } from "../src/grpc-catalog-source.js";
import { CatalogService } from "../src/gen/hydrovalley/persistence/v1/catalog_pb.js";
import { createAttemptLimiter } from "../src/rate-limit.js";
import { SourceUnavailableError } from "../src/source-errors.js";
import { CATALOG, MAX_U64 } from "./support/catalog-fixture.js";
import { TestGrpcServer } from "./support/grpc-server.js";

const NOW = Date.UTC(2026, 9, 14, 12, 0, 0);
const TTL_MS = CATALOG_CACHE_TTL_SECONDS * 1000;
const REQUEST_ID = "6f1c2f6e-0000-4000-8000-000000000001";

const EXPECTED_JSON = {
  catalog_version: "9f2b7c1d",
  drop: { water_cost: "1", humidity: "25", demo_humidity: "1" },
  tank: { capacity: "20", refill_amount: "3", refill_interval_seconds: 2 },
  growth: {
    humidity_per_stage: "100",
    leaves_grass: "1",
    leaves_shrub: "2",
    leaves_flowers: "4",
    leaves_tree: MAX_U64.toString(),
    tree_max_humidity: "101",
  },
  evaporation: { humidity_per_second: "2", wither_after_ticks: 100 },
  fire: {
    drops_to_extinguish: 3,
    extinguish_bonus_leaves: "5",
    burn_out_ticks: 300,
    ignition_chance_ppm: 50,
    propagation_chance_ppm: 2000,
  },
  sprinkler: { price: "10", area_side: 3, humidity: "26", interval_seconds: 4 },
  drip: { price: "25", area_side: 5, evaporation_divisor: 2 },
  treatment_plant: { price: "40", refill_amount: "6", interval_seconds: 7 },
  installation_price_increase_percent: 15,
  bigger_drop: { price: "30", area_side: 3 },
  bigger_tank: { price: "20", extra_capacity: "10" },
  offline_installation_seconds: "7200",
};

/** Fuente controlable: cuenta llamadas y falla o espera a voluntad. */
class FakeCatalogSource implements CatalogSource {
  calls = 0;
  error: Error | undefined;
  gate: Promise<void> | undefined;
  catalog: Catalog = CATALOG;

  async getCatalog(): Promise<Catalog> {
    this.calls++;
    if (this.gate) await this.gate;
    if (this.error) throw this.error;
    return this.catalog;
  }
}

const opened: FastifyInstance[] = [];
const server = new TestGrpcServer();
afterEach(async () => {
  await Promise.all(opened.splice(0).map((app) => app.close()));
  await server.stop();
});

async function setup(source: CatalogSource = new FakeCatalogSource(), limit = 1000) {
  let clock = NOW;
  const app = await buildApp({}, undefined, undefined, undefined, undefined, {
    catalog: createCachedCatalog({ source, ttlMs: TTL_MS, now: () => clock }),
    limiter: createAttemptLimiter({ limit, windowMs: 60_000, now: () => NOW }),
  });
  opened.push(app);
  return {
    app,
    advance: (ms: number) => {
      clock += ms;
    },
    get: (headers: Record<string, string> = {}) =>
      app.inject({ method: "GET", url: "/api/catalog", headers }),
  };
}

describe("GET /api/catalog", () => {
  it("serves every catalog field in snake_case with no token", async () => {
    const { get } = await setup();

    const res = await get();

    expect(res.statusCode).toBe(200);
    expect(res.json()).toEqual(EXPECTED_JSON);
  });

  it("sends u64 values as text and keeps 32-bit values as numbers", async () => {
    const { get } = await setup();

    const body = (await get()).json<typeof EXPECTED_JSON>();

    expect(body.growth.leaves_tree).toBe("18446744073709551615");
    expect(body.offline_installation_seconds).toBe("7200");
    expect(typeof body.tank.refill_interval_seconds).toBe("number");
    expect(typeof body.installation_price_increase_percent).toBe("number");
  });

  it("sets Cache-Control from the TTL and an ETag from the catalog version", async () => {
    const { get } = await setup();

    const res = await get();

    expect(res.headers["cache-control"]).toBe(`public, max-age=${String(CATALOG_CACHE_TTL_SECONDS)}`);
    expect(res.headers.etag).toBe('"9f2b7c1d"');
  });

  it("answers 304 with no body when If-None-Match matches, and 200 when it does not", async () => {
    const { get } = await setup();

    const same = await get({ "if-none-match": '"9f2b7c1d"' });
    const weak = await get({ "if-none-match": 'W/"other", W/"9f2b7c1d"' });
    const any = await get({ "if-none-match": "*" });
    const other = await get({ "if-none-match": '"0000"' });

    for (const res of [same, weak, any]) {
      expect(res.statusCode).toBe(304);
      expect(res.body).toBe("");
      expect(res.headers.etag).toBe('"9f2b7c1d"');
    }
    expect(other.statusCode).toBe(200);
  });

  it("does not call persistence again while the copy is fresh, and renews it after the TTL", async () => {
    const source = new FakeCatalogSource();
    const { get, advance } = await setup(source);

    await get();
    await get();
    advance(TTL_MS - 1);
    await get();
    expect(source.calls).toBe(1);

    advance(1);
    await get();
    expect(source.calls).toBe(2);
  });

  it("shares one persistence call among concurrent misses", async () => {
    const source = new FakeCatalogSource();
    let open: () => void = () => undefined;
    source.gate = new Promise<void>((resolve) => {
      open = resolve;
    });
    const { get } = await setup(source);

    const pending = [get(), get(), get(), get()];
    open();
    const responses = await Promise.all(pending);

    expect(source.calls).toBe(1);
    expect(responses.map((r) => r.statusCode)).toEqual([200, 200, 200, 200]);
  });

  it("serves the expired copy while persistence is down", async () => {
    const source = new FakeCatalogSource();
    const { get, advance } = await setup(source);
    await get();
    source.error = new SourceUnavailableError();
    advance(TTL_MS * 10);

    const res = await get();

    expect(source.calls).toBe(2);
    expect(res.statusCode).toBe(200);
    expect(res.json()).toEqual(EXPECTED_JSON);
    expect(res.headers["cache-control"]).toBe("no-cache");
  });

  it("answers 503 in Spanish when persistence is down and there is no copy", async () => {
    const source = new FakeCatalogSource();
    source.error = new SourceUnavailableError();
    const { get } = await setup(source);

    const res = await get();

    expect(res.statusCode).toBe(503);
    expect(res.json()).toEqual({
      error: "El servicio no está disponible en este momento. Inténtalo de nuevo en unos minutos.",
    });
  });

  it("answers 500 for an unexpected error with no copy", async () => {
    const source = new FakeCatalogSource();
    source.error = new Error("boom");
    const { get } = await setup(source);

    const res = await get();

    expect(res.statusCode).toBe(500);
    expect(res.json()).toEqual({ error: "Error interno" });
  });

  it("answers 429 with Retry-After once the per-IP limit is spent", async () => {
    const { get } = await setup(new FakeCatalogSource(), 2);

    await get();
    await get();
    const res = await get();

    expect(res.statusCode).toBe(429);
    expect(Number(res.headers["retry-after"])).toBeGreaterThan(0);
    expect(res.json()).toEqual({
      error: "Demasiadas consultas. Espera un momento antes de volver a intentarlo.",
    });
  });

  it("limits at the documented rate", () => {
    expect(CATALOG_LIMIT_PER_IP).toEqual({ limit: 60, windowMs: 60_000 });
  });

  it("is documented in the OpenAPI document", async () => {
    const { app } = await setup();

    const spec = (await app.inject({ method: "GET", url: "/api/docs/json" })).json<{
      paths: Record<string, { get?: { tags?: string[] } }>;
    }>();

    expect(spec.paths["/api/catalog"]?.get?.tags).toEqual(["catalog"]);
  });
});

describe("GET /api/catalog through the gRPC source", () => {
  it("maps the persistence response and forwards x-request-id as metadata", async () => {
    const received: (string | null)[] = [];
    const address = await server.start(CatalogService, {
      getCatalog: (_req, ctx) => {
        received.push(ctx.requestHeader.get("x-request-id"));
        return { catalog: CATALOG };
      },
    });
    const { get } = await setup(createGrpcCatalogSource({ address }));

    const res = await get({ "x-request-id": REQUEST_ID });

    expect(res.statusCode).toBe(200);
    expect(res.json()).toEqual(EXPECTED_JSON);
    expect(received).toEqual([REQUEST_ID]);
  });

  it("retries UNAVAILABLE with the documented 100/200/400 ms policy and then fails", async () => {
    let calls = 0;
    const sleeps: number[] = [];
    let offset = 0;
    const address = await server.start(CatalogService, {
      getCatalog: () => {
        calls++;
        throw new ConnectError("down", Code.Unavailable);
      },
    });
    const source = createGrpcCatalogSource({
      address,
      random: () => 0,
      now: () => Date.now() + offset,
      sleep: (ms) => {
        sleeps.push(ms);
        offset += ms;
        return Promise.resolve();
      },
    });

    await expect(source.getCatalog()).rejects.toBeInstanceOf(SourceUnavailableError);
    expect(calls).toBe(4);
    expect(sleeps).toEqual([100, 200, 400]);
  });

  it("rejects a response that lacks the catalog or a rule group", async () => {
    const address = await server.start(CatalogService, {
      getCatalog: () => ({ catalog: { catalogVersion: "x" } }),
    });

    await expect(createGrpcCatalogSource({ address }).getCatalog()).rejects.toBeInstanceOf(
      SourceUnavailableError,
    );
  });
});

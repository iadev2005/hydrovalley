import { createServer, type Server } from "node:http";
import type { AddressInfo } from "node:net";
import { afterEach, describe, expect, it } from "vitest";
import { createHubStatsSource } from "../src/hub-stats.js";
import { VALID_STATS } from "./support/hub-stats-fixture.js";

let server: Server | undefined;
let paths: string[] = [];

async function start(handler: (res: import("node:http").ServerResponse) => void): Promise<string> {
  paths = [];
  server = createServer((req, res) => {
    paths.push(`${req.method ?? ""} ${req.url ?? ""}`);
    handler(res);
  });
  await new Promise<void>((resolve) => server?.listen(0, "127.0.0.1", resolve));
  return `127.0.0.1:${String((server.address() as AddressInfo).port)}`;
}

afterEach(async () => {
  const s = server;
  server = undefined;
  if (!s) return;
  s.closeAllConnections();
  await new Promise<void>((resolve) => {
    s.close(() => {
      resolve();
    });
  });
});

const send = (res: import("node:http").ServerResponse, code: number, body: string) => {
  res.writeHead(code, { "Content-Type": "application/json" });
  return res.end(body);
};

describe("hub stats source", () => {
  it("returns the validated stats, keeping u64 values as text", async () => {
    const address = await start((res) => send(res, 200, JSON.stringify(VALID_STATS)));

    const result = await createHubStatsSource({ address })();

    expect(paths).toEqual(["GET /stats"]);
    expect(result).toEqual({ ok: true, stats: VALID_STATS });
  });

  it("copies only the known fields", async () => {
    const address = await start((res) =>
      send(res, 200, JSON.stringify({ ...VALID_STATS, secret: "x", queue: 3 })),
    );

    const result = await createHubStatsSource({ address })();

    expect(result).toEqual({ ok: true, stats: VALID_STATS });
  });

  it("accepts an older hub without restored_tick and reports it as null", async () => {
    const older: Record<string, unknown> = { ...VALID_STATS };
    delete older.restored_tick;
    const address = await start((res) => send(res, 200, JSON.stringify(older)));

    const result = await createHubStatsSource({ address })();

    expect(result).toEqual({ ok: true, stats: { ...VALID_STATS, restored_tick: null } });
  });

  it("is down with http_503 on a non-200 answer", async () => {
    const address = await start((res) => send(res, 503, "{}"));

    expect(await createHubStatsSource({ address })()).toEqual({
      ok: false,
      detail: "http_503",
    });
  });

  it("releases the response body when the hub answers a non-200 status", async () => {
    let cancelled = false;
    const body = new ReadableStream({
      cancel() {
        cancelled = true;
      },
    });
    const fakeFetch = (() =>
      Promise.resolve(new Response(body, { status: 503 }))) as unknown as typeof fetch;

    const result = await createHubStatsSource({ address: "hub:8081", fetch: fakeFetch })();

    expect(result).toEqual({ ok: false, detail: "http_503" });
    expect(cancelled).toBe(true);
  });

  it("is down with invalid_response on invalid JSON", async () => {
    const address = await start((res) => send(res, 200, "{not json"));

    expect(await createHubStatsSource({ address })()).toEqual({
      ok: false,
      detail: "invalid_response",
    });
  });

  it.each([
    ["a missing field", { ...VALID_STATS, queue: undefined }],
    ["a numeric u64", { ...VALID_STATS, tick: 5 }],
    ["a numeric restored_tick", { ...VALID_STATS, restored_tick: 50 }],
    ["a non-numeric restored_tick text", { ...VALID_STATS, restored_tick: "abc" }],
    ["a non-numeric u64 text", { ...VALID_STATS, accepted_drops: "abc" }],
    ["an unknown breaker state", { ...VALID_STATS, breaker: "weird" }],
    ["a negative count", { ...VALID_STATS, sessions: -1 }],
    ["a nested object of the wrong shape", { ...VALID_STATS, latency_ms: { p50: "x" } }],
    ["an array", [VALID_STATS]],
  ])("is down with invalid_response on %s", async (_name, body) => {
    const address = await start((res) => send(res, 200, JSON.stringify(body)));

    expect(await createHubStatsSource({ address })()).toEqual({
      ok: false,
      detail: "invalid_response",
    });
  });

  it("is down with timeout when the hub does not answer in time", async () => {
    const address = await start(() => {
      // No responde.
    });

    expect(await createHubStatsSource({ address, timeoutMs: 50 })()).toEqual({
      ok: false,
      detail: "timeout",
    });
  });

  it("is down with unavailable when nobody listens", async () => {
    const address = await start((res) => send(res, 200, "{}"));
    await new Promise<void>((resolve) => {
      server?.close(() => {
        resolve();
      });
    });
    server = undefined;

    expect(await createHubStatsSource({ address, timeoutMs: 500 })()).toEqual({
      ok: false,
      detail: "unavailable",
    });
  });

  it("fails over across a comma-separated list of hub addresses and stays sticky", async () => {
    let primaryOk = false;
    const primary = createServer((_req, res) => {
      if (primaryOk) {
        send(res, 200, JSON.stringify(VALID_STATS));
      } else {
        send(res, 503, "{}");
      }
    });
    const standby = createServer((_req, res) => {
      send(res, 200, JSON.stringify({ ...VALID_STATS, tick: "77" }));
    });
    await new Promise<void>((resolve) => primary.listen(0, "127.0.0.1", resolve));
    await new Promise<void>((resolve) => standby.listen(0, "127.0.0.1", resolve));
    try {
      const pAddr = `127.0.0.1:${String((primary.address() as AddressInfo).port)}`;
      const sAddr = `127.0.0.1:${String((standby.address() as AddressInfo).port)}`;
      const source = createHubStatsSource({ address: `${pAddr}, ${sAddr}` });

      const first = await source();
      expect(first).toEqual({ ok: true, stats: { ...VALID_STATS, tick: "77" } });

      // Sticky al standby aunque el primario vuelva.
      primaryOk = true;
      const second = await source();
      expect(second).toEqual({ ok: true, stats: { ...VALID_STATS, tick: "77" } });
    } finally {
      primary.closeAllConnections();
      standby.closeAllConnections();
      await Promise.all([
        new Promise<void>((r) => {
          primary.close(() => {
            r();
          });
        }),
        new Promise<void>((r) => {
          standby.close(() => {
            r();
          });
        }),
      ]);
    }
  });
});

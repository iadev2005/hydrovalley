import { describe, expect, it } from "vitest";
import { createSystemStatus, type HubResult, type ProbeResult } from "../src/system-status.js";
import { VALID_STATS } from "./support/hub-stats-fixture.js";

const UP: ProbeResult = { status: "up" };

class Upstreams {
  calls = { hub: 0, world: 0, persistence: 0 };
  hub: () => Promise<HubResult> = () => Promise.resolve({ ok: true, stats: VALID_STATS });
  world: () => Promise<ProbeResult> = () => Promise.resolve(UP);
  persistence: () => Promise<ProbeResult> = () => Promise.resolve(UP);
  now = Date.UTC(2026, 9, 3, 12, 0, 0);

  build() {
    return createSystemStatus({
      hub: () => {
        this.calls.hub++;
        return this.hub();
      },
      world: () => {
        this.calls.world++;
        return this.world();
      },
      persistence: () => {
        this.calls.persistence++;
        return this.persistence();
      },
      now: () => this.now,
    });
  }
}

describe("system status", () => {
  it("reports every service up with the hub stats", async () => {
    const up = new Upstreams();

    const snapshot = await up.build().get();

    expect(snapshot).toEqual({
      generated_at: "2026-10-03T12:00:00.000Z",
      services: [
        { name: "nginx", status: "up" },
        { name: "api", status: "up" },
        { name: "hub", status: "up" },
        { name: "world", status: "up" },
        { name: "persistence", status: "up" },
      ],
      hub: VALID_STATS,
    });
  });

  it("marks the hub down with its detail and hub null, still reporting the others", async () => {
    const up = new Upstreams();
    up.hub = () => Promise.resolve({ ok: false, detail: "timeout" });

    const snapshot = await up.build().get();

    expect(snapshot.hub).toBeNull();
    expect(snapshot.services).toEqual([
      { name: "nginx", status: "up" },
      { name: "api", status: "up" },
      { name: "hub", status: "down", detail: "timeout" },
      { name: "world", status: "up" },
      { name: "persistence", status: "up" },
    ]);
  });

  it("marks world down on NOT_SERVING and persistence down on an error", async () => {
    const up = new Upstreams();
    up.world = () => Promise.resolve({ status: "down", detail: "not_serving" });
    up.persistence = () => Promise.resolve({ status: "down", detail: "error" });

    const { services } = await up.build().get();

    expect(services.slice(3)).toEqual([
      { name: "world", status: "down", detail: "not_serving" },
      { name: "persistence", status: "down", detail: "error" },
    ]);
  });

  it("never rejects when a probe throws", async () => {
    const up = new Upstreams();
    up.world = () => Promise.reject(new Error("boom 10.0.0.1"));
    up.hub = () => Promise.reject(new Error("boom"));

    const { services, hub } = await up.build().get();

    expect(hub).toBeNull();
    expect(services[2]).toEqual({
      name: "hub",
      status: "down",
      detail: "error",
    });
    expect(services[3]).toEqual({
      name: "world",
      status: "down",
      detail: "error",
    });
  });

  it("queries the three upstreams in parallel", async () => {
    const up = new Upstreams();
    const started: string[] = [];
    const never = new Promise<never>(() => undefined);
    up.hub = () => {
      started.push("hub");
      return never;
    };
    up.world = () => {
      started.push("world");
      return never;
    };
    up.persistence = () => {
      started.push("persistence");
      return never;
    };

    void up.build().get();
    await Promise.resolve();

    expect(started.sort()).toEqual(["hub", "persistence", "world"]);
  });

  it("serves the cache within 1 s with a single upstream call", async () => {
    const up = new Upstreams();
    const status = up.build();

    const first = await status.get();
    up.now += 999;
    const second = await status.get();

    expect(second).toBe(first);
    expect(up.calls).toEqual({ hub: 1, world: 1, persistence: 1 });
  });

  it("queries again once the cache is older than 1 s", async () => {
    const up = new Upstreams();
    const status = up.build();

    await status.get();
    up.now += 1000;
    const second = await status.get();

    expect(second.generated_at).toBe("2026-10-03T12:00:01.000Z");
    expect(up.calls).toEqual({ hub: 2, world: 2, persistence: 2 });
  });

  it("coalesces concurrent requests onto one in-flight aggregation", async () => {
    const up = new Upstreams();
    let release: (() => void) | undefined;
    const gate = new Promise<void>((resolve) => {
      release = resolve;
    });
    up.world = async () => {
      await gate;
      return UP;
    };
    const status = up.build();

    const pending = [status.get(), status.get(), status.get()];
    release?.();
    const results = await Promise.all(pending);

    expect(results[1]).toBe(results[0]);
    expect(results[2]).toBe(results[0]);
    expect(up.calls).toEqual({ hub: 1, world: 1, persistence: 1 });
  });
});

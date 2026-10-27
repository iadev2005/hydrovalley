import { describe, it, expect } from "vitest";
import {
  buildBuyUpgradeFrame,
  buildCleanTrashFrame,
  buildDropFrame,
  buildPlaceInstallationFrame,
  buildResetValleyFrame,
  buildSetDemoModeFrame,
  buildPlayUrl,
  createEndpointRotator,
  createFailoverFetch,
  createReconnectBackoff,
  describeFrame,
  GRID_SIZE,
  AUTH_CLOSE_CODE,
  decideOnClose,
  shouldReconnect,
  newUuid,
  parseBackupHosts,
  parseFrame,
  RECONNECT_BASE_MS,
  RECONNECT_CAP_MS,
  RECONNECT_STABLE_MS,
  reconnectDelay,
  STATE_SILENCE_MS,
  type StateFrame,
} from "./play.ts";

const UUID_V4 = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;

describe("newUuid", () => {
  it("builds a v4 UUID from random bytes", () => {
    const fill = (bytes: Uint8Array<ArrayBuffer>) => bytes.fill(0xff);
    expect(newUuid(fill)).toBe("ffffffff-ffff-4fff-bfff-ffffffffffff");
  });

  it("produces different v4 UUIDs with the real generator", () => {
    const a = newUuid();
    const b = newUuid();
    expect(a).toMatch(UUID_V4);
    expect(b).toMatch(UUID_V4);
    expect(a).not.toBe(b);
  });
});

describe("buildPlayUrl", () => {
  it("uses ws on http, keeps host and port and sends only the token", () => {
    expect(
      buildPlayUrl({ protocol: "http:", host: "localhost:8080" }, "a.b.c"),
    ).toBe("ws://localhost:8080/play?token=a.b.c&map=true");
  });

  it("uses wss on https", () => {
    expect(
      buildPlayUrl({ protocol: "https:", host: "10.68.17.4:8080" }, "a.b.c"),
    ).toBe("wss://10.68.17.4:8080/play?token=a.b.c&map=true");
  });

  it("url-encodes the token and never sends player_id", () => {
    const url = buildPlayUrl({ protocol: "http:", host: "h" }, "a b&c=d");
    expect(url).toBe("ws://h/play?token=a+b%26c%3Dd&map=true");
    expect(url.includes("player_id")).toBe(false);
  });
});

describe("shouldReconnect", () => {
  it("does not reconnect after an invalid or expired token (4401)", () => {
    expect(AUTH_CLOSE_CODE).toBe(4401);
    expect(shouldReconnect(4401)).toBe(false);
  });

  it("reconnects after any other close", () => {
    for (const code of [1000, 1001, 1006, 1011, 4400, 4402]) {
      expect(shouldReconnect(code)).toBe(true);
    }
  });
});

describe("buildDropFrame", () => {
  it("encodes the drop message of play.proto", () => {
    const actionId = "11111111-2222-4333-8444-555555555555";
    expect(JSON.parse(buildDropFrame({ x: 14, y: 9, actionId }))).toEqual({
      type: "drop",
      action_id: actionId,
      x: 14,
      y: 9,
    });
  });

  it("rejects negative or non-integer coordinates", () => {
    const actionId = "11111111-2222-4333-8444-555555555555";
    const cases: [number, number][] = [
      [-1, 0],
      [0, 1.5],
      [Number.NaN, 0],
    ];
    for (const [x, y] of cases) {
      expect(() => buildDropFrame({ x, y, actionId })).toThrow(RangeError);
    }
  });
});

describe("map option of buildPlayUrl", () => {
  it("can ask for a map-less state", () => {
    expect(
      buildPlayUrl({ protocol: "http:", host: "h" }, "t", { map: false }),
    ).toBe("ws://h/play?token=t&map=false");
  });
});

describe("frame builders for the other commands", () => {
  const actionId = "11111111-2222-4333-8444-555555555555";

  it("encodes place_installation, buy_upgrade, set_demo_mode and reset_valley", () => {
    expect(
      JSON.parse(buildPlaceInstallationFrame({ installation: "drip", x: 2, y: 3, actionId })),
    ).toEqual({ type: "place_installation", action_id: actionId, installation: "drip", x: 2, y: 3 });
    expect(
      JSON.parse(buildPlaceInstallationFrame({ installation: "trash_bin", x: 4, y: 5, actionId })),
    ).toEqual({ type: "place_installation", action_id: actionId, installation: "trash_bin", x: 4, y: 5 });
    expect(
      JSON.parse(buildCleanTrashFrame({ x: 4, y: 5, actionId })),
    ).toEqual({ type: "clean_trash", action_id: actionId, x: 4, y: 5 });
    expect(JSON.parse(buildBuyUpgradeFrame({ upgrade: "bigger_tank", actionId }))).toEqual({
      type: "buy_upgrade",
      action_id: actionId,
      upgrade: "bigger_tank",
    });
    expect(JSON.parse(buildSetDemoModeFrame({ enabled: true, actionId }))).toEqual({
      type: "set_demo_mode",
      action_id: actionId,
      enabled: true,
    });
    expect(JSON.parse(buildResetValleyFrame({ actionId }))).toEqual({
      type: "reset_valley",
      action_id: actionId,
    });
  });

  it("rejects unknown kinds and bad coordinates", () => {
    expect(() => buildPlaceInstallationFrame({ installation: "dam", x: 0, y: 0, actionId })).toThrow(RangeError);
    expect(() => buildPlaceInstallationFrame({ installation: "drip", x: -1, y: 0, actionId })).toThrow(RangeError);
    expect(() => buildCleanTrashFrame({ x: -1, y: 0, actionId })).toThrow(RangeError);
    expect(() => buildBuyUpgradeFrame({ upgrade: "bigger_roof", actionId })).toThrow(RangeError);
    expect(() => buildSetDemoModeFrame({ enabled: "yes" as unknown as boolean, actionId })).toThrow(TypeError);
  });
});

describe("parseFrame of the full state", () => {
  it("reads map, fires, installations and the player's prices", () => {
    const state = parseFrame(
      JSON.stringify({
        type: "state",
        tick: "120",
        demo_mode: true,
        width: 2,
        height: 1,
        stages: [1, 2],
        humidity: ["0", "75"],
        fires: [{ x: 1, y: 0, drops_received: 2 }],
        installations: [{ x: 0, y: 0, installation: "drip", owner_id: "p1", remaining_durability: 30 }],
        buildings: [false, true],
        trash: [0, 2],
        player: {
          water: "5",
          tank_capacity: "20",
          leaves: "3",
          upgrades: ["bigger_drop"],
          next_sprinkler_price: "10",
          next_drip_price: "25",
          next_treatment_plant_price: "40",
          next_trash_bin_price: "15",
        },
      }),
    ) as StateFrame;
    expect(state.demo_mode).toBe(true);
    expect(state.width).toBe(2);
    expect(state.height).toBe(1);
    expect(state.stages).toEqual([1, 2]);
    expect(state.humidity).toEqual([0n, 75n]);
    expect(state.fires).toEqual([{ x: 1, y: 0, drops_received: 2 }]);
    expect(state.installations).toEqual([
      { x: 0, y: 0, installation: "drip", owner_id: "p1", remaining_durability: 30 },
    ]);
    expect(state.buildings).toEqual([false, true]);
    expect(state.trash).toEqual([0, 2]);
    expect(state.player?.upgrades).toEqual(["bigger_drop"]);
    expect(state.player?.next_sprinkler_price).toBe(10n);
    expect(state.player?.next_treatment_plant_price).toBe(40n);
    expect(state.player?.next_trash_bin_price).toBe(15n);
  });

  it("defaults the map fields to empty when they are absent (map=false)", () => {
    const state = parseFrame('{"type":"state"}') as StateFrame;
    expect(state.demo_mode).toBe(false);
    expect(state.stages).toEqual([]);
    expect(state.humidity).toEqual([]);
    expect(state.fires).toEqual([]);
    expect(state.installations).toEqual([]);
  });
});

describe("parseFrame of the offline summary", () => {
  it("reads the counts of the summary as BigInt from their text form", () => {
    const state = parseFrame(
      JSON.stringify({
        type: "state",
        player: {
          water: "5",
          offline_summary: {
            leaves_earned: "12",
            water_reused: "1099511627776",
            fires_extinguished: "1",
            tiles_grown: "38",
          },
        },
      }),
    ) as StateFrame;
    expect(state.player?.offline_summary).toEqual({
      leaves_earned: 12n,
      water_reused: 1099511627776n,
      fires_extinguished: 1n,
      tiles_grown: 38n,
    });
  });

  it("fills the counts that are absent with zero and ignores a summary that is not an object", () => {
    const partial = parseFrame(
      '{"type":"state","player":{"offline_summary":{"tiles_grown":"2"}}}',
    ) as StateFrame;
    expect(partial.player?.offline_summary).toEqual({
      leaves_earned: 0n,
      water_reused: 0n,
      fires_extinguished: 0n,
      tiles_grown: 2n,
    });
    const odd = parseFrame('{"type":"state","player":{"offline_summary":"x"}}') as StateFrame;
    expect(odd.player?.offline_summary).toBeNull();
  });
});

describe("parseFrame", () => {
  it("reads result, state and error frames", () => {
    expect(
      parseFrame('{"type":"result","action_id":"a","status":"applied"}'),
    ).toEqual({ type: "result", action_id: "a", status: "applied", reason: "" });
    const state = parseFrame(
      '{"type":"state","tick":"57","width":32,"height":32,"player":{"water":"19","tank_capacity":"20"}}',
    ) as StateFrame;
    expect(state.type).toBe("state");
    expect(state.tick).toBe(57n);
    expect(state.paused).toBe(false);
    expect(state.player).toEqual({
      water: 19n,
      tank_capacity: 20n,
      leaves: 0n,
      upgrades: [],
      next_sprinkler_price: 0n,
      next_drip_price: 0n,
      next_treatment_plant_price: 0n,
      next_trash_bin_price: 0n,
      offline_summary: null,
    });
    expect(parseFrame('{"type":"error","code":"invalid_message"}')).toEqual({
      type: "error",
      action_id: "",
      code: "invalid_message",
    });
  });

  it("reads a state without player and uint64 fields omitted", () => {
    const state = parseFrame('{"type":"state","paused":true}') as StateFrame;
    expect(state.tick).toBe(0n);
    expect(state.paused).toBe(true);
    expect(state.player).toBeNull();
  });

  it("returns an unknown frame for invalid JSON or unknown types", () => {
    expect(parseFrame("{nope").type).toBe("unknown");
    expect(parseFrame('{"type":"chat"}').type).toBe("unknown");
    expect(parseFrame("[1]").type).toBe("unknown");
  });
});

describe("describeFrame", () => {
  it("summarises each frame in one log line", () => {
    expect(
      describeFrame({ type: "result", action_id: "a1", status: "rejected", reason: "out_of_bounds" }),
    ).toBe("result a1: rejected (out_of_bounds)");
    expect(
      describeFrame({ type: "result", action_id: "a1", status: "applied", reason: "" }),
    ).toBe("result a1: applied");
    expect(
      describeFrame({
        type: "state",
        tick: 57n,
        paused: false,
        demo_mode: false,
        width: 32,
        height: 32,
        stages: [],
        humidity: [],
        fires: [],
        installations: [],
        player: {
          water: 19n,
          tank_capacity: 20n,
          leaves: 0n,
          upgrades: [],
          next_sprinkler_price: 0n,
          next_drip_price: 0n,
          next_treatment_plant_price: 0n,
          offline_summary: null,
        },
      }),
    ).toBe("state tick 57, agua 19/20");
    expect(
      describeFrame({
        type: "state",
        tick: 3n,
        paused: true,
        demo_mode: false,
        width: 32,
        height: 32,
        stages: [],
        humidity: [],
        fires: [],
        installations: [],
        player: null,
      }),
    ).toBe("state tick 3 (en pausa)");
    expect(
      describeFrame({ type: "error", action_id: "", code: "unknown_type" }),
    ).toBe("error unknown_type");
    expect(describeFrame({ type: "unknown", raw: "x" })).toBe("mensaje no reconocido: x");
  });
});

describe("GRID_SIZE", () => {
  it("matches the 32x32 valley of D-01", () => {
    expect(GRID_SIZE).toBe(32);
  });
});

describe("reconnectDelay", () => {
  it("uses base 500 ms, cap 10 s and a 5 s stability window", () => {
    expect(RECONNECT_BASE_MS).toBe(500);
    expect(RECONNECT_CAP_MS).toBe(10_000);
    expect(RECONNECT_STABLE_MS).toBe(5_000);
  });

  it("doubles the ceiling on each attempt (random just below 1)", () => {
    const almostOne = () => 0.999999;
    expect([0, 1, 2, 3, 4].map((a) => reconnectDelay(a, almostOne))).toEqual([
      499, 999, 1999, 3999, 7999,
    ]);
  });

  it("caps the ceiling at 10 s, even for huge attempt counts", () => {
    const almostOne = () => 0.999999;
    expect(reconnectDelay(5, almostOne)).toBe(9999);
    expect(reconnectDelay(60, almostOne)).toBe(9999);
    expect(reconnectDelay(10_000, almostOne)).toBe(9999);
  });

  it("applies full jitter: the delay is random * ceiling", () => {
    expect(reconnectDelay(0, () => 0)).toBe(0);
    expect(reconnectDelay(2, () => 0.5)).toBe(1000);
    expect(reconnectDelay(10, () => 0.25)).toBe(2500);
  });

  it("rejects a negative or non-integer attempt", () => {
    expect(() => reconnectDelay(-1, () => 0)).toThrow(RangeError);
    expect(() => reconnectDelay(1.5, () => 0)).toThrow(RangeError);
  });
});

describe("createReconnectBackoff", () => {
  function fakeClock(start = 0) {
    let t = start;
    return { now: () => t, advance: (ms: number) => (t += ms) };
  }
  const almostOne = () => 0.999999;

  it("grows the delay across consecutive failed attempts", () => {
    const clock = fakeClock();
    const backoff = createReconnectBackoff({ random: almostOne, now: clock.now });
    const delays: number[] = [];
    for (let i = 0; i < 6; i++) {
      backoff.closed();
      delays.push(backoff.nextDelay());
    }
    expect(delays).toEqual([499, 999, 1999, 3999, 7999, 9999]);
  });

  it("does not reset when the connection closes before 5 s", () => {
    const clock = fakeClock();
    const backoff = createReconnectBackoff({ random: almostOne, now: clock.now });
    backoff.closed();
    backoff.nextDelay();
    backoff.closed();
    backoff.nextDelay();
    backoff.opened();
    clock.advance(RECONNECT_STABLE_MS - 1);
    backoff.closed();
    expect(backoff.nextDelay()).toBe(1999);
  });

  it("resets after the connection stays open for at least 5 s", () => {
    const clock = fakeClock();
    const backoff = createReconnectBackoff({ random: almostOne, now: clock.now });
    for (let i = 0; i < 4; i++) {
      backoff.closed();
      backoff.nextDelay();
    }
    backoff.opened();
    clock.advance(RECONNECT_STABLE_MS);
    backoff.closed();
    expect(backoff.nextDelay()).toBe(499);
  });

  it("uses the injected random source", () => {
    const backoff = createReconnectBackoff({ random: () => 0, now: () => 0 });
    backoff.closed();
    expect(backoff.nextDelay()).toBe(0);
  });
});

describe("decideOnClose", () => {
  const base = { code: 1006, current: true, expiresAt: 10_000, now: 1_000 };

  it("reconnects after a network drop with a live session", () => {
    expect(decideOnClose(base)).toBe("reconnect");
    expect(decideOnClose({ ...base, code: 1001 })).toBe("reconnect");
  });

  it("shows the access panel on 4401", () => {
    expect(decideOnClose({ ...base, code: 4401 })).toBe("show_access");
  });

  it("ignores the close of a socket that is no longer the current one (logout)", () => {
    expect(decideOnClose({ ...base, current: false })).toBe("ignore");
    expect(decideOnClose({ ...base, current: false, code: 4401 })).toBe("ignore");
  });

  it("stops when the stored session has already expired", () => {
    expect(decideOnClose({ ...base, now: 10_000 })).toBe("expired");
    expect(decideOnClose({ ...base, now: 20_000 })).toBe("expired");
  });

  it("prefers 4401 over expiry (same outcome for the user, one clear reason)", () => {
    expect(decideOnClose({ ...base, code: 4401, now: 20_000 })).toBe("show_access");
  });
});

describe("STATE_SILENCE_MS and endpoint failover (B8 E2)", () => {
  it("defines a 3.5 s silence watchdog for missing state frames", () => {
    expect(STATE_SILENCE_MS).toBe(3_500);
  });

  it("parses backup hosts from comma-separated strings or full URLs and deduplicates", () => {
    expect(
      parseBackupHosts("10.68.17.80:8080", "https://10.68.17.99:8080, 10.68.17.80:8080, 10.68.17.100:8080/"),
    ).toEqual(["10.68.17.80:8080", "10.68.17.99:8080", "10.68.17.100:8080"]);
  });

  it("rotates across hosts on advance and keeps sticky affinity while healthy", () => {
    const rotator = createEndpointRotator("10.68.17.80:8080", ["10.68.17.99:8080"]);
    expect(rotator.currentHost()).toBe("10.68.17.80:8080");
    expect(rotator.hosts()).toEqual(["10.68.17.80:8080", "10.68.17.99:8080"]);
    expect(rotator.advance()).toBe("10.68.17.99:8080");
    expect(rotator.currentHost()).toBe("10.68.17.99:8080");
    expect(rotator.advance()).toBe("10.68.17.80:8080");
  });

  it("allows adding discovered backup hosts dynamically without losing current affinity", () => {
    const rotator = createEndpointRotator("10.68.17.80:8080");
    rotator.addBackups("https://10.68.17.99:8080");
    expect(rotator.hosts()).toEqual(["10.68.17.80:8080", "10.68.17.99:8080"]);
    expect(rotator.advance()).toBe("10.68.17.99:8080");
  });

  it("creates a failover fetch that retries on the backup host when the primary throws or returns 502/504", async () => {
    const rotator = createEndpointRotator("10.68.17.80:8080", ["10.68.17.99:8080"]);
    const calledUrls: string[] = [];
    const baseFetch = async (input: string | URL | Request) => {
      const url = String(input);
      calledUrls.push(url);
      if (url.includes("10.68.17.80:8080")) {
        throw new TypeError("Failed to fetch");
      }
      return new Response('{"ok":true}', {
        status: 200,
        headers: { "X-Proxy-Backups": "10.68.17.100:8080" },
      });
    };
    const failoverFetch = createFailoverFetch({
      baseFetch,
      rotator,
      protocol: "https:",
    });
    const res = await failoverFetch("/api/system");
    expect(res.status).toBe(200);
    expect(calledUrls).toEqual([
      "https://10.68.17.80:8080/api/system",
      "https://10.68.17.99:8080/api/system",
    ]);
    expect(rotator.currentHost()).toBe("10.68.17.99:8080");
    expect(rotator.hosts()).toEqual([
      "10.68.17.80:8080",
      "10.68.17.99:8080",
      "10.68.17.100:8080",
    ]);
  });
});

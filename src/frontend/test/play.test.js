import { describe, it } from "node:test";
import assert from "node:assert/strict";
import {
  buildBuyUpgradeFrame,
  buildDropFrame,
  buildPlaceInstallationFrame,
  buildResetValleyFrame,
  buildSetDemoModeFrame,
  buildPlayUrl,
  createReconnectBackoff,
  describeFrame,
  GRID_SIZE,
  AUTH_CLOSE_CODE,
  decideOnClose,
  shouldReconnect,
  newUuid,
  parseFrame,
  RECONNECT_BASE_MS,
  RECONNECT_CAP_MS,
  RECONNECT_STABLE_MS,
  reconnectDelay,
} from "../js/play.js";

const UUID_V4 = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;

describe("newUuid", () => {
  it("builds a v4 UUID from random bytes", () => {
    const fill = (bytes) => bytes.fill(0xff);
    assert.equal(newUuid(fill), "ffffffff-ffff-4fff-bfff-ffffffffffff");
  });

  it("produces different v4 UUIDs with the real generator", () => {
    const a = newUuid();
    const b = newUuid();
    assert.match(a, UUID_V4);
    assert.match(b, UUID_V4);
    assert.notEqual(a, b);
  });
});

describe("buildPlayUrl", () => {
  it("uses ws on http, keeps host and port and sends only the token", () => {
    assert.equal(
      buildPlayUrl({ protocol: "http:", host: "localhost:8080" }, "a.b.c"),
      "ws://localhost:8080/play?token=a.b.c&map=true",
    );
  });

  it("uses wss on https", () => {
    assert.equal(
      buildPlayUrl({ protocol: "https:", host: "10.68.17.4:8080" }, "a.b.c"),
      "wss://10.68.17.4:8080/play?token=a.b.c&map=true",
    );
  });

  it("url-encodes the token and never sends player_id", () => {
    const url = buildPlayUrl({ protocol: "http:", host: "h" }, "a b&c=d");
    assert.equal(url, "ws://h/play?token=a+b%26c%3Dd&map=true");
    assert.ok(!url.includes("player_id"));
  });
});

describe("shouldReconnect", () => {
  it("does not reconnect after an invalid or expired token (4401)", () => {
    assert.equal(AUTH_CLOSE_CODE, 4401);
    assert.equal(shouldReconnect(4401), false);
  });

  it("reconnects after any other close", () => {
    for (const code of [1000, 1001, 1006, 1011, 4400, 4402]) {
      assert.equal(shouldReconnect(code), true, String(code));
    }
  });
});

describe("buildDropFrame", () => {
  it("encodes the drop message of play.proto", () => {
    const actionId = "11111111-2222-4333-8444-555555555555";
    assert.deepEqual(JSON.parse(buildDropFrame({ x: 14, y: 9, actionId })), {
      type: "drop",
      action_id: actionId,
      x: 14,
      y: 9,
    });
  });

  it("rejects negative or non-integer coordinates", () => {
    const actionId = "11111111-2222-4333-8444-555555555555";
    for (const [x, y] of [
      [-1, 0],
      [0, 1.5],
      [Number.NaN, 0],
    ]) {
      assert.throws(() => buildDropFrame({ x, y, actionId }), RangeError);
    }
  });
});

describe("map option of buildPlayUrl", () => {
  it("can ask for a map-less state", () => {
    assert.equal(
      buildPlayUrl({ protocol: "http:", host: "h" }, "t", { map: false }),
      "ws://h/play?token=t&map=false",
    );
  });
});

describe("frame builders for the other commands", () => {
  const actionId = "11111111-2222-4333-8444-555555555555";

  it("encodes place_installation, buy_upgrade, set_demo_mode and reset_valley", () => {
    assert.deepEqual(
      JSON.parse(buildPlaceInstallationFrame({ installation: "drip", x: 2, y: 3, actionId })),
      { type: "place_installation", action_id: actionId, installation: "drip", x: 2, y: 3 },
    );
    assert.deepEqual(JSON.parse(buildBuyUpgradeFrame({ upgrade: "bigger_tank", actionId })), {
      type: "buy_upgrade",
      action_id: actionId,
      upgrade: "bigger_tank",
    });
    assert.deepEqual(JSON.parse(buildSetDemoModeFrame({ enabled: true, actionId })), {
      type: "set_demo_mode",
      action_id: actionId,
      enabled: true,
    });
    assert.deepEqual(JSON.parse(buildResetValleyFrame({ actionId })), {
      type: "reset_valley",
      action_id: actionId,
    });
  });

  it("rejects unknown kinds and bad coordinates", () => {
    assert.throws(() => buildPlaceInstallationFrame({ installation: "dam", x: 0, y: 0, actionId }), RangeError);
    assert.throws(() => buildPlaceInstallationFrame({ installation: "drip", x: -1, y: 0, actionId }), RangeError);
    assert.throws(() => buildBuyUpgradeFrame({ upgrade: "bigger_roof", actionId }), RangeError);
    assert.throws(() => buildSetDemoModeFrame({ enabled: "yes", actionId }), TypeError);
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
        installations: [{ x: 0, y: 0, installation: "drip", owner_id: "p1" }],
        player: {
          water: "5",
          tank_capacity: "20",
          leaves: "3",
          upgrades: ["bigger_drop"],
          next_sprinkler_price: "10",
          next_drip_price: "25",
          next_treatment_plant_price: "40",
        },
      }),
    );
    assert.equal(state.demo_mode, true);
    assert.equal(state.width, 2);
    assert.equal(state.height, 1);
    assert.deepEqual(state.stages, [1, 2]);
    assert.deepEqual(state.humidity, [0n, 75n]);
    assert.deepEqual(state.fires, [{ x: 1, y: 0, drops_received: 2 }]);
    assert.deepEqual(state.installations, [{ x: 0, y: 0, installation: "drip", owner_id: "p1" }]);
    assert.deepEqual(state.player.upgrades, ["bigger_drop"]);
    assert.equal(state.player.next_sprinkler_price, 10n);
    assert.equal(state.player.next_treatment_plant_price, 40n);
  });

  it("defaults the map fields to empty when they are absent (map=false)", () => {
    const state = parseFrame('{"type":"state"}');
    assert.equal(state.demo_mode, false);
    assert.deepEqual(state.stages, []);
    assert.deepEqual(state.humidity, []);
    assert.deepEqual(state.fires, []);
    assert.deepEqual(state.installations, []);
  });
});

describe("parseFrame", () => {
  it("reads result, state and error frames", () => {
    assert.deepEqual(
      parseFrame('{"type":"result","action_id":"a","status":"applied"}'),
      { type: "result", action_id: "a", status: "applied", reason: "" },
    );
    const state = parseFrame(
      '{"type":"state","tick":"57","width":32,"height":32,"player":{"water":"19","tank_capacity":"20"}}',
    );
    assert.equal(state.type, "state");
    assert.equal(state.tick, 57n);
    assert.equal(state.paused, false);
    assert.deepEqual(state.player, {
      water: 19n,
      tank_capacity: 20n,
      leaves: 0n,
      upgrades: [],
      next_sprinkler_price: 0n,
      next_drip_price: 0n,
      next_treatment_plant_price: 0n,
    });
    assert.deepEqual(parseFrame('{"type":"error","code":"invalid_message"}'), {
      type: "error",
      action_id: "",
      code: "invalid_message",
    });
  });

  it("reads a state without player and uint64 fields omitted", () => {
    const state = parseFrame('{"type":"state","paused":true}');
    assert.equal(state.tick, 0n);
    assert.equal(state.paused, true);
    assert.equal(state.player, null);
  });

  it("returns an unknown frame for invalid JSON or unknown types", () => {
    assert.equal(parseFrame("{nope").type, "unknown");
    assert.equal(parseFrame('{"type":"chat"}').type, "unknown");
    assert.equal(parseFrame("[1]").type, "unknown");
  });
});

describe("describeFrame", () => {
  it("summarises each frame in one log line", () => {
    assert.equal(
      describeFrame({ type: "result", action_id: "a1", status: "rejected", reason: "out_of_bounds" }),
      "result a1: rejected (out_of_bounds)",
    );
    assert.equal(
      describeFrame({ type: "result", action_id: "a1", status: "applied", reason: "" }),
      "result a1: applied",
    );
    assert.equal(
      describeFrame({
        type: "state",
        tick: 57n,
        paused: false,
        player: { water: 19n, tank_capacity: 20n, leaves: 0n },
      }),
      "state tick 57, agua 19/20",
    );
    assert.equal(
      describeFrame({ type: "state", tick: 3n, paused: true, player: null }),
      "state tick 3 (en pausa)",
    );
    assert.equal(
      describeFrame({ type: "error", action_id: "", code: "unknown_type" }),
      "error unknown_type",
    );
    assert.equal(describeFrame({ type: "unknown", raw: "x" }), "mensaje no reconocido: x");
  });
});

describe("GRID_SIZE", () => {
  it("matches the 32x32 valley of D-01", () => {
    assert.equal(GRID_SIZE, 32);
  });
});

describe("reconnectDelay", () => {
  it("uses base 500 ms, cap 10 s and a 5 s stability window", () => {
    assert.equal(RECONNECT_BASE_MS, 500);
    assert.equal(RECONNECT_CAP_MS, 10_000);
    assert.equal(RECONNECT_STABLE_MS, 5_000);
  });

  it("doubles the ceiling on each attempt (random just below 1)", () => {
    const almostOne = () => 0.999999;
    assert.deepEqual(
      [0, 1, 2, 3, 4].map((a) => reconnectDelay(a, almostOne)),
      [499, 999, 1999, 3999, 7999],
    );
  });

  it("caps the ceiling at 10 s, even for huge attempt counts", () => {
    const almostOne = () => 0.999999;
    assert.equal(reconnectDelay(5, almostOne), 9999);
    assert.equal(reconnectDelay(60, almostOne), 9999);
    assert.equal(reconnectDelay(10_000, almostOne), 9999);
  });

  it("applies full jitter: the delay is random * ceiling", () => {
    assert.equal(reconnectDelay(0, () => 0), 0);
    assert.equal(reconnectDelay(2, () => 0.5), 1000);
    assert.equal(reconnectDelay(10, () => 0.25), 2500);
  });

  it("rejects a negative or non-integer attempt", () => {
    assert.throws(() => reconnectDelay(-1, () => 0), RangeError);
    assert.throws(() => reconnectDelay(1.5, () => 0), RangeError);
  });
});

describe("createReconnectBackoff", () => {
  function fakeClock(start = 0) {
    let t = start;
    return { now: () => t, advance: (ms) => (t += ms) };
  }
  const almostOne = () => 0.999999;

  it("grows the delay across consecutive failed attempts", () => {
    const clock = fakeClock();
    const backoff = createReconnectBackoff({ random: almostOne, now: clock.now });
    const delays = [];
    for (let i = 0; i < 6; i++) {
      backoff.closed();
      delays.push(backoff.nextDelay());
    }
    assert.deepEqual(delays, [499, 999, 1999, 3999, 7999, 9999]);
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
    assert.equal(backoff.nextDelay(), 1999);
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
    assert.equal(backoff.nextDelay(), 499);
  });

  it("uses the injected random source", () => {
    const backoff = createReconnectBackoff({ random: () => 0, now: () => 0 });
    backoff.closed();
    assert.equal(backoff.nextDelay(), 0);
  });
});

describe("decideOnClose", () => {
  const base = { code: 1006, current: true, expiresAt: 10_000, now: 1_000 };

  it("reconnects after a network drop with a live session", () => {
    assert.equal(decideOnClose(base), "reconnect");
    assert.equal(decideOnClose({ ...base, code: 1001 }), "reconnect");
  });

  it("shows the access panel on 4401", () => {
    assert.equal(decideOnClose({ ...base, code: 4401 }), "show_access");
  });

  it("ignores the close of a socket that is no longer the current one (logout)", () => {
    assert.equal(decideOnClose({ ...base, current: false }), "ignore");
    assert.equal(decideOnClose({ ...base, current: false, code: 4401 }), "ignore");
  });

  it("stops when the stored session has already expired", () => {
    assert.equal(decideOnClose({ ...base, now: 10_000 }), "expired");
    assert.equal(decideOnClose({ ...base, now: 20_000 }), "expired");
  });

  it("prefers 4401 over expiry (same outcome for the user, one clear reason)", () => {
    assert.equal(decideOnClose({ ...base, code: 4401, now: 20_000 }), "show_access");
  });
});

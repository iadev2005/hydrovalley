import { describe, it } from "node:test";
import assert from "node:assert/strict";
import {
  buildDropFrame,
  buildPlayUrl,
  createReconnectBackoff,
  describeFrame,
  GRID_SIZE,
  isUuid,
  loadOrCreatePlayerId,
  newUuid,
  parseFrame,
  RECONNECT_BASE_MS,
  RECONNECT_CAP_MS,
  RECONNECT_STABLE_MS,
  reconnectDelay,
} from "../js/play.js";

const UUID_V4 = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;

function memoryStorage(initial = {}) {
  const data = new Map(Object.entries(initial));
  return {
    getItem: (k) => (data.has(k) ? data.get(k) : null),
    setItem: (k, v) => data.set(k, String(v)),
    data,
  };
}

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

describe("isUuid", () => {
  it("accepts UUIDs and rejects anything else", () => {
    assert.equal(isUuid("0b9f1c2e-8d3a-4c1b-9e2f-1a2b3c4d5e6f"), true);
    assert.equal(isUuid("not-a-uuid"), false);
    assert.equal(isUuid(null), false);
  });
});

describe("loadOrCreatePlayerId", () => {
  it("returns the stored id when it is a UUID", () => {
    const id = "0b9f1c2e-8d3a-4c1b-9e2f-1a2b3c4d5e6f";
    const storage = memoryStorage({ "hydrovalley.player_id": id });
    assert.equal(loadOrCreatePlayerId(storage, () => "other"), id);
  });

  it("creates and stores a new id when there is none or it is invalid", () => {
    const fresh = "11111111-2222-4333-8444-555555555555";
    for (const initial of [{}, { "hydrovalley.player_id": "garbage" }]) {
      const storage = memoryStorage(initial);
      assert.equal(loadOrCreatePlayerId(storage, () => fresh), fresh);
      assert.equal(storage.data.get("hydrovalley.player_id"), fresh);
    }
  });

  it("still works when storage throws (private mode)", () => {
    const storage = {
      getItem() {
        throw new Error("blocked");
      },
      setItem() {
        throw new Error("blocked");
      },
    };
    const fresh = "11111111-2222-4333-8444-555555555555";
    assert.equal(loadOrCreatePlayerId(storage, () => fresh), fresh);
  });
});

describe("buildPlayUrl", () => {
  const id = "0b9f1c2e-8d3a-4c1b-9e2f-1a2b3c4d5e6f";

  it("uses ws on http and keeps host and port", () => {
    assert.equal(
      buildPlayUrl({ protocol: "http:", host: "localhost:8080" }, id),
      `ws://localhost:8080/play?player_id=${id}&map=false`,
    );
  });

  it("uses wss on https", () => {
    assert.equal(
      buildPlayUrl({ protocol: "https:", host: "10.68.17.4:8080" }, id),
      `wss://10.68.17.4:8080/play?player_id=${id}&map=false`,
    );
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
    assert.deepEqual(state.player, { water: 19n, tank_capacity: 20n, leaves: 0n });
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

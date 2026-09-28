import { describe, it } from "node:test";
import assert from "node:assert/strict";
import {
  buildDropFrame,
  buildPlayUrl,
  describeFrame,
  isUuid,
  loadOrCreatePlayerId,
  newUuid,
  parseFrame,
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

// Pruebas unitarias de PlaySession con un WebSocket falso: no necesitan el
// stack. Se ejecutan con `node --test tests/e2e/play-session.test.mjs`.
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { PlaySession } from "./play-session.mjs";

class FakeWebSocket extends EventTarget {
  constructor(url) {
    super();
    this.url = url;
    this.sent = [];
  }
  deliver(data) {
    const event = new Event("message");
    event.data = data;
    this.dispatchEvent(event);
  }
  send(data) {
    this.sent.push(data);
  }
  close() {}
}

function newSession() {
  let socket;
  const session = new PlaySession("ws://example.test/play", {
    createSocket: (url) => (socket = new FakeWebSocket(url)),
  });
  return { session, socket };
}

const state = (tick, water) => JSON.stringify({ type: "state", tick: String(tick), player: { water } });
const isState = (f) => f.type === "state";

describe("PlaySession.first", () => {
  it("resolves with a frame already buffered", async () => {
    const { session, socket } = newSession();
    socket.deliver(state(1, "20"));
    const frame = await session.first("a state", isState, 50);
    assert.equal(frame.tick, "1");
  });

  it("resolves with a frame that arrives later", async () => {
    const { session, socket } = newSession();
    const pending = session.first("a state", isState, 1000);
    socket.deliver(state(2, "20"));
    assert.equal((await pending).tick, "2");
  });
});

describe("PlaySession.waitFor", () => {
  it("ignores stale buffered frames and waits for a new one", async () => {
    const { session, socket } = newSession();
    socket.deliver(state(1, "20"));
    const pending = session.waitFor("a fresh state", isState, 1000);
    socket.deliver(state(2, "19"));
    const frame = await pending;
    assert.equal(frame.tick, "2");
    assert.equal(frame.player.water, "19");
  });

  it("times out when only stale frames match", async () => {
    const { session, socket } = newSession();
    socket.deliver(state(1, "20"));
    await assert.rejects(session.waitFor("a fresh state", isState, 30), /timed out waiting for a fresh state/);
  });

  it("considers frames from an explicit index onward", async () => {
    const { session, socket } = newSession();
    socket.deliver(state(1, "20"));
    socket.deliver(state(2, "19"));
    const frame = await session.waitFor("a state", isState, 50, { fromIndex: 1 });
    assert.equal(frame.tick, "2");
  });

  it("resolves every waiter that matches the same frame", async () => {
    const { session, socket } = newSession();
    const a = session.waitFor("a", isState, 1000);
    const b = session.waitFor("b", isState, 1000);
    const c = session.waitFor("c", (f) => f.type === "result", 30);
    socket.deliver(state(3, "18"));
    assert.equal((await a).tick, "3");
    assert.equal((await b).tick, "3");
    await assert.rejects(c, /timed out waiting for c/);
  });
});

describe("PlaySession with an unparseable frame", () => {
  it("records the raw frame and fails the pending waiters with a clear error", async () => {
    const { session, socket } = newSession();
    const pending = session.waitFor("a state", isState, 1000);
    const pendingFirst = session.first("a result", (f) => f.type === "result", 1000);
    assert.doesNotThrow(() => socket.deliver("{not json"));
    await assert.rejects(pending, /unparseable frame from \/play: "\{not json"/);
    await assert.rejects(pendingFirst, /unparseable frame from \/play/);
    assert.deepEqual(session.unparseable, ["{not json"]);
  });

  it("keeps working for frames that arrive afterwards", async () => {
    const { session, socket } = newSession();
    socket.deliver("garbage");
    const pending = session.waitFor("a state", isState, 1000);
    socket.deliver(state(4, "17"));
    assert.equal((await pending).tick, "4");
  });
});

describe("PlaySession.closed", () => {
  it("resolves with the close code and reason chosen by the server", async () => {
    const { session, socket } = newSession();
    const event = new Event("close");
    event.code = 4401;
    event.reason = "invalid or expired token";
    socket.dispatchEvent(event);
    assert.deepEqual(await session.closed, { code: 4401, reason: "invalid or expired token" });
  });
});

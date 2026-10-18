// Pruebas unitarias de ChatSession con un WebSocket falso: no necesitan el
// stack. Se ejecutan con `node --test tests/e2e/chat-session.test.mjs`.
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { ChatSession } from "./chat-session.mjs";

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

function newChatSession() {
  let socket;
  const session = new ChatSession("ws://example.test/chat?token=fake", {
    createSocket: (url) => (socket = new FakeWebSocket(url)),
  });
  return { session, socket };
}

const msgFrame = (seq, text, id = "m1") =>
  JSON.stringify({
    type: "message",
    seq: String(seq),
    message_id: id,
    username: "ana",
    text,
    kind: "player",
    sent_at: new Date().toISOString(),
    server_epoch: "epoch-1",
  });
const isMessage = (f) => f.type === "message";

describe("ChatSession.first", () => {
  it("resolves with a message frame already buffered", async () => {
    const { session, socket } = newChatSession();
    socket.deliver(msgFrame(1, "hola"));
    const frame = await session.first("a message", isMessage, 50);
    assert.equal(frame.seq, "1");
    assert.equal(frame.text, "hola");
  });

  it("resolves with a message frame that arrives later", async () => {
    const { session, socket } = newChatSession();
    const pending = session.first("a message", isMessage, 1000);
    socket.deliver(msgFrame(2, "segundo"));
    assert.equal((await pending).seq, "2");
    assert.equal((await pending).text, "segundo");
  });
});

describe("ChatSession.waitFor", () => {
  it("ignores stale buffered frames and waits for a new one", async () => {
    const { session, socket } = newChatSession();
    socket.deliver(msgFrame(1, "primero"));
    const pending = session.waitFor("a fresh message", isMessage, 1000);
    socket.deliver(msgFrame(2, "segundo"));
    const frame = await pending;
    assert.equal(frame.seq, "2");
    assert.equal(frame.text, "segundo");
  });
});

describe("ChatSession.sendMessage and sync", () => {
  it("sends properly formatted send and sync frames", () => {
    const { session, socket } = newChatSession();
    const msgId = session.sendMessage("test text", "fixed-id");
    assert.equal(msgId, "fixed-id");
    assert.equal(socket.sent.length, 1);
    assert.deepEqual(JSON.parse(socket.sent[0]), {
      type: "send",
      text: "test text",
      message_id: "fixed-id",
    });

    session.sync(10);
    assert.equal(socket.sent.length, 2);
    assert.deepEqual(JSON.parse(socket.sent[1]), {
      type: "sync",
      after_seq: 10,
    });
  });
});

import { describe, expect, it } from "vitest";
import {
  buildChatUrl,
  buildSendFrame,
  buildSyncFrame,
  parseChatFrame,
} from "./chat.ts";

describe("chat logic", () => {
  it("buildChatUrl uses wss: for https: and ws: for http:", () => {
    expect(
      buildChatUrl({ protocol: "https:", host: "localhost:8080", token: "jwt-token" }),
    ).toBe("wss://localhost:8080/chat?token=jwt-token");

    expect(
      buildChatUrl({ protocol: "http:", host: "localhost:8080", token: "tok with space" }),
    ).toBe("ws://localhost:8080/chat?token=tok%20with%20space");
  });

  it("buildSendFrame includes type send and message_id", () => {
    const raw = buildSendFrame("hola amigos", "00000000-0000-4000-8000-000000000001");
    const parsed = JSON.parse(raw);
    expect(parsed).toEqual({
      type: "send",
      message_id: "00000000-0000-4000-8000-000000000001",
      text: "hola amigos",
    });
  });

  it("buildSyncFrame formats after_seq as string for protojson", () => {
    const raw = buildSyncFrame(42);
    expect(JSON.parse(raw)).toEqual({
      type: "sync",
      after_seq: "42",
    });
  });

  it("parseChatFrame parses valid and invalid JSON safely", () => {
    expect(parseChatFrame("not-json").type).toBe("unknown");
    expect(parseChatFrame("{}").type).toBe("unknown");
    expect(parseChatFrame('{"type":"message","seq":"1"}')).toEqual({
      type: "message",
      seq: "1",
    });
  });
});

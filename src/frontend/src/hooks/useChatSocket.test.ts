// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { renderHook, act } from "@testing-library/react";
import { useChatSocket } from "./useChatSocket.ts";
import type { Session } from "../logic/auth.ts";

const session: Session = {
  token: "jwt.token.valido",
  expiresAt: Date.now() + 100_000,
  username: "ana",
};

class MockWebSocket {
  static instances: MockWebSocket[] = [];
  static OPEN = 1;
  static CLOSED = 3;

  url: string;
  readyState = MockWebSocket.OPEN;
  sent: string[] = [];
  onopen: (() => void) | null = null;
  onmessage: ((event: { data: unknown }) => void) | null = null;
  onclose: ((event: { code: number }) => void) | null = null;
  onerror: (() => void) | null = null;

  constructor(url: string) {
    this.url = url;
    MockWebSocket.instances.push(this);
  }

  send(data: string) {
    this.sent.push(data);
  }

  close(code = 1000) {
    this.readyState = MockWebSocket.CLOSED;
    this.onclose?.({ code });
  }

  triggerOpen() {
    this.readyState = MockWebSocket.OPEN;
    this.onopen?.();
  }

  triggerMessage(data: string) {
    this.onmessage?.({ data });
  }

  triggerClose(code = 1000) {
    this.readyState = MockWebSocket.CLOSED;
    this.onclose?.({ code });
  }
}

describe("useChatSocket", () => {
  beforeEach(() => {
    MockWebSocket.instances = [];
    vi.stubGlobal("WebSocket", MockWebSocket);
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it("inicia desconectado si no hay sesión", () => {
    const { result } = renderHook(() => useChatSocket({ session: null }));
    expect(result.current.status).toBe("disconnected");
    expect(result.current.isOpen).toBe(false);
    expect(result.current.messages).toEqual([]);
    expect(MockWebSocket.instances.length).toBe(0);
  });

  it("conecta al WebSocket con el token en la query y envía SyncFrame al abrir", () => {
    const { result } = renderHook(() => useChatSocket({ session }));
    expect(MockWebSocket.instances.length).toBe(1);
    const ws = MockWebSocket.instances[0]!;
    expect(ws.url).toContain("/chat?token=jwt.token.valido");
    expect(result.current.status).toBe("connecting");

    act(() => {
      ws.triggerOpen();
    });

    expect(result.current.status).toBe("connected");
    expect(result.current.isOpen).toBe(true);
    expect(ws.sent.length).toBe(1);
    expect(JSON.parse(ws.sent[0]!)).toEqual({
      type: "sync",
      after_seq: "0",
    });
  });

  it("recibe MessageFrames, los almacena ordenados por seq e ignora duplicados", () => {
    const { result } = renderHook(() => useChatSocket({ session }));
    const ws = MockWebSocket.instances[0]!;
    act(() => {
      ws.triggerOpen();
    });

    const msg1 = JSON.stringify({
      type: "message",
      seq: "2",
      message_id: "uuid-2",
      sender_id: "p2",
      username: "bob",
      text: "segundo mensaje",
      kind: "player",
      sent_at: "2026-10-17T12:00:01Z",
      server_epoch: "epoch-1",
    });

    const msg2 = JSON.stringify({
      type: "message",
      seq: "1",
      message_id: "uuid-1",
      sender_id: "p1",
      username: "ana",
      text: "primer mensaje",
      kind: "player",
      sent_at: "2026-10-17T12:00:00Z",
      server_epoch: "epoch-1",
    });

    act(() => {
      ws.triggerMessage(msg1);
      ws.triggerMessage(msg2);
      // Duplicado idéntico
      ws.triggerMessage(msg1);
    });

    expect(result.current.messages.length).toBe(2);
    expect(result.current.messages[0]!.seq).toBe(1);
    expect(result.current.messages[0]!.text).toBe("primer mensaje");
    expect(result.current.messages[1]!.seq).toBe(2);
    expect(result.current.messages[1]!.text).toBe("segundo mensaje");
  });

  it("detecta cambio de server_epoch, descarta mensajes anteriores y solicita after_seq = 0", () => {
    const { result } = renderHook(() => useChatSocket({ session }));
    const ws = MockWebSocket.instances[0]!;
    act(() => {
      ws.triggerOpen();
    });

    const msg1 = JSON.stringify({
      type: "message",
      seq: "1",
      message_id: "uuid-1",
      sender_id: "p1",
      username: "ana",
      text: "mensaje pre-reinicio",
      kind: "player",
      sent_at: "2026-10-17T12:00:00Z",
      server_epoch: "epoch-1",
    });

    act(() => {
      ws.triggerMessage(msg1);
    });
    expect(result.current.messages.length).toBe(1);

    // Mensaje de nueva época
    const msgNuevoReinicio = JSON.stringify({
      type: "message",
      seq: "1",
      message_id: "uuid-reinicio",
      sender_id: "p1",
      username: "ana",
      text: "nuevo arranque",
      kind: "player",
      sent_at: "2026-10-17T12:05:00Z",
      server_epoch: "epoch-2",
    });

    act(() => {
      ws.triggerMessage(msgNuevoReinicio);
    });

    // Se limpió el buffer local y se envió SyncFrame pidiendo desde 0
    expect(result.current.messages.length).toBe(0);
    const lastSent = JSON.parse(ws.sent[ws.sent.length - 1]!);
    expect(lastSent).toEqual({ type: "sync", after_seq: "0" });
  });

  it("sendMessage envía un SendFrame si el socket está abierto", () => {
    const { result } = renderHook(() => useChatSocket({ session }));
    const ws = MockWebSocket.instances[0]!;
    act(() => {
      ws.triggerOpen();
    });

    let ok = false;
    act(() => {
      ok = result.current.sendMessage("hola mundo");
    });
    expect(ok).toBe(true);
    expect(ws.sent.length).toBe(2); // [0]: sync, [1]: send
    const sendPayload = JSON.parse(ws.sent[1]!);
    expect(sendPayload.type).toBe("send");
    expect(sendPayload.text).toBe("hola mundo");
    expect(sendPayload.message_id).toBeDefined();

    // No envía si es solo espacios
    act(() => {
      ok = result.current.sendMessage("   ");
    });
    expect(ok).toBe(false);
  });

  it("cierra con 4401 y llama a onSessionExpired sin reintentar", () => {
    const onSessionExpired = vi.fn();
    const { result } = renderHook(() => useChatSocket({ session, onSessionExpired }));
    const ws = MockWebSocket.instances[0]!;

    act(() => {
      ws.triggerOpen();
      ws.triggerClose(4401);
    });

    expect(result.current.status).toBe("disconnected");
    expect(onSessionExpired).toHaveBeenCalledTimes(1);

    // Avanzar temporizadores: no debe crear otro WebSocket
    act(() => {
      vi.advanceTimersByTime(10_000);
    });
    expect(MockWebSocket.instances.length).toBe(1);
  });
});

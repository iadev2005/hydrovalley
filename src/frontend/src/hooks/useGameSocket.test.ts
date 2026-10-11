// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { renderHook, act } from "@testing-library/react";
import { useGameSocket } from "./useGameSocket.ts";
import type { Session } from "../logic/auth.ts";

const session: Session = {
  token: "tok",
  expiresAt: Date.now() + 100_000,
  username: "Ana",
};

type EventListener = (event: { data?: unknown; code?: number }) => void;

class MockWebSocket {
  static instances: MockWebSocket[] = [];
  static OPEN = 1;
  static CLOSED = 3;

  url: string;
  readyState = MockWebSocket.OPEN;
  listeners: Record<string, EventListener[]> = {};
  sent: string[] = [];

  constructor(url: string) {
    this.url = url;
    MockWebSocket.instances.push(this);
  }

  addEventListener(type: string, listener: EventListener) {
    this.listeners[type] = this.listeners[type] ?? [];
    this.listeners[type].push(listener);
  }

  removeEventListener(type: string, listener: EventListener) {
    this.listeners[type] = (this.listeners[type] ?? []).filter((l) => l !== listener);
  }

  send(data: string) {
    this.sent.push(data);
  }

  close(code = 1000) {
    this.readyState = MockWebSocket.CLOSED;
    this.emit("close", { code });
  }

  emit(type: string, event: { data?: unknown; code?: number } = {}) {
    for (const listener of this.listeners[type] ?? []) {
      listener(event);
    }
  }
}

describe("useGameSocket", () => {
  beforeEach(() => {
    MockWebSocket.instances = [];
    vi.stubGlobal("WebSocket", MockWebSocket);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("remains disconnected when session is null", () => {
    const { result } = renderHook(() => useGameSocket({ session: null }));
    expect(result.current.status).toBe("disconnected");
    expect(MockWebSocket.instances.length).toBe(0);
  });

  it("connects when session is provided and updates status on open", () => {
    const { result } = renderHook(() => useGameSocket({ session }));
    expect(MockWebSocket.instances.length).toBe(1);
    const ws = MockWebSocket.instances[0];
    expect(ws).toBeDefined();
    if (!ws) return;

    expect(result.current.status).toBe("connecting");

    act(() => {
      ws.emit("open");
    });

    expect(result.current.status).toBe("connected");
    expect(result.current.isOpen).toBe(true);
  });

  it("sends drop and marks action as pending", () => {
    const { result } = renderHook(() => useGameSocket({ session }));
    const ws = MockWebSocket.instances[0];
    expect(ws).toBeDefined();
    if (!ws) return;

    act(() => {
      ws.emit("open");
    });

    let sent = false;
    act(() => {
      sent = result.current.sendDrop(2, 3);
    });

    expect(sent).toBe(true);
    expect(result.current.pendingCount).toBe(1);
    expect(ws.sent.length).toBe(1);
    expect(ws.sent[0]).toContain('"type":"drop"');
    expect(result.current.actionMessage?.text).toContain("pendiente");
  });

  it("updates gameState when state frame is received", () => {
    const { result } = renderHook(() => useGameSocket({ session }));
    const ws = MockWebSocket.instances[0];
    expect(ws).toBeDefined();
    if (!ws) return;

    act(() => {
      ws.emit("open");
      ws.emit("message", {
        data: JSON.stringify({
          type: "state",
          tick: "123",
          demo_mode: false,
          width: 32,
          height: 32,
          stages: [1],
          humidity: ["10"],
          fires: [],
          installations: [],
          player: null,
        }),
      });
    });

    expect(result.current.gameState?.tick).toBe(123n);
  });

  it("notifies session expiration on 4401 close code", () => {
    const onSessionExpired = vi.fn();
    renderHook(() => useGameSocket({ session, onSessionExpired }));
    const ws = MockWebSocket.instances[0];
    expect(ws).toBeDefined();
    if (!ws) return;

    act(() => {
      ws.emit("open");
      ws.close(4401);
    });

    expect(onSessionExpired).toHaveBeenCalledWith("la sesión caducó, vuelve a entrar");
  });
});

// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach } from "vitest";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { GameApp } from "./GameApp.tsx";
import { VIEW_MODE_KEY } from "../logic/viewMode.ts";
import { SESSION_KEY, type StorageLike } from "../logic/auth.ts";

function memoryStorage(initial: Record<string, string> = {}): StorageLike {
  const store = new Map<string, string>(Object.entries(initial));
  return {
    getItem: (k: string) => store.get(k) ?? null,
    setItem: (k: string, v: string) => {
      store.set(k, String(v));
    },
    removeItem: (k: string) => {
      store.delete(k);
    },
  };
}

describe("GameApp", () => {
  it("renders main heading, auth panel and valley canvas when unauthenticated", () => {
    const storage = memoryStorage();
    render(
      <GameApp
        sessionOptions={{ storage }}
        socketOptions={{ location: { protocol: "http:", host: "localhost:8080" } }}
      />,
    );

    expect(screen.getByRole("heading", { level: 1, name: "hydrovalley" })).toBeDefined();
    expect(screen.getByRole("heading", { level: 2, name: "Acceso" })).toBeDefined();
    expect(screen.getByText("Sin estado todavía.")).toBeDefined();
    expect(document.getElementById("valley")).toBeDefined();
    expect(screen.getByText("desconectado")).toBeDefined();
  });

  it("renders toggle view button allowing user to switch views and updates storage", () => {
    const storage = memoryStorage({ [VIEW_MODE_KEY]: "zenithal" });
    render(
      <GameApp
        sessionOptions={{ storage }}
        socketOptions={{ location: { protocol: "http:", host: "localhost:8080" } }}
      />,
    );

    const toggleBtn = screen.getByRole("button", { name: "⛰️ Vista isométrica" });
    expect(toggleBtn).toBeDefined();

    // Al pulsar para cambiar a isométrica
    fireEvent.click(toggleBtn);

    expect(storage.getItem(VIEW_MODE_KEY)).toBe("isometric");
  });

  it("renders zenithal view directly if preferred in storage without emergency indicator", () => {
    const storage = memoryStorage({ [VIEW_MODE_KEY]: "zenithal" });
    render(
      <GameApp
        sessionOptions={{ storage }}
        socketOptions={{ location: { protocol: "http:", host: "localhost:8080" } }}
      />,
    );

    expect(screen.getByRole("button", { name: "⛰️ Vista isométrica" })).toBeDefined();
    expect(screen.getByText("Cuadrícula 2D (vista cenital)")).toBeDefined();
    expect(screen.queryByText(/Salida de emergencia/)).toBeNull();
  });

  it("activates emergency zenithal view and shows emergency indicator when Phaser fails", () => {
    const storage = memoryStorage({ [VIEW_MODE_KEY]: "isometric" });
    render(
      <GameApp
        sessionOptions={{ storage }}
        socketOptions={{ location: { protocol: "http:", host: "localhost:8080" } }}
      />,
    );

    // En el entorno de happy-dom, Phaser no puede crear contexto WebGL y falla al montar.
    // ValleyPhaser reporta el error y conmuta automáticamente a ValleyCanvas.
    expect(document.getElementById("valley")).toBeDefined();
    expect(screen.getByText(/Salida de emergencia/)).toBeDefined();
    expect(screen.getByText("Cuadrícula 2D (vista cenital)")).toBeDefined();
  });

  describe("offline summary", () => {
    class FakeSocket {
      static last: FakeSocket | null = null;
      static OPEN = 1;
      readyState = FakeSocket.OPEN;
      listeners: Record<string, ((event: { data?: unknown; code?: number }) => void)[]> = {};
      constructor(url: string) {
        // El chat también abre un WebSocket: solo interesa el de /play.
        if (url.includes("/play")) FakeSocket.last = this;
      }
      addEventListener(type: string, listener: (event: { data?: unknown; code?: number }) => void) {
        (this.listeners[type] ??= []).push(listener);
      }
      send() {}
      close() {}
      emit(type: string, event: { data?: unknown } = {}) {
        for (const listener of this.listeners[type] ?? []) listener(event);
      }
    }

    afterEach(() => {
      vi.unstubAllGlobals();
    });

    it("shows the summary of the first state and hides it when closed", () => {
      vi.stubGlobal("WebSocket", FakeSocket);
      const storage = memoryStorage({
        [SESSION_KEY]: JSON.stringify({
          token: "tok",
          expiresAt: Date.now() + 100_000,
          username: "Ana",
        }),
      });
      render(
        <GameApp
          sessionOptions={{ storage }}
          socketOptions={{ location: { protocol: "http:", host: "localhost:8080" } }}
        />,
      );
      expect(screen.queryByText(/Mientras no estabas/)).toBeNull();

      act(() => {
        FakeSocket.last?.emit("open");
        FakeSocket.last?.emit("message", {
          data: JSON.stringify({
            type: "state",
            tick: "5",
            width: 32,
            height: 32,
            player: { water: "1", offline_summary: { tiles_grown: "38", fires_extinguished: "1" } },
          }),
        });
      });
      expect(
        screen.getByText(
          "Mientras no estabas, tus aspersores hicieron crecer 38 casillas y apagaron un incendio.",
        ),
      ).toBeDefined();

      fireEvent.click(screen.getByRole("button", { name: "Cerrar" }));
      expect(screen.queryByText(/Mientras no estabas/)).toBeNull();
    });
  });
});

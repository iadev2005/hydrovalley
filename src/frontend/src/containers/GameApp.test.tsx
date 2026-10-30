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
  describe("clic contextual y herramientas", () => {
    class SendingSocket {
      static last: SendingSocket | null = null;
      static OPEN = 1;
      readyState = SendingSocket.OPEN;
      sent: Record<string, unknown>[] = [];
      listeners: Record<string, ((event: { data?: unknown; code?: number }) => void)[]> = {};
      constructor(url: string) {
        if (url.includes("/play")) SendingSocket.last = this;
      }
      addEventListener(type: string, listener: (event: { data?: unknown; code?: number }) => void) {
        (this.listeners[type] ??= []).push(listener);
      }
      send(frame: string) {
        this.sent.push(JSON.parse(frame) as Record<string, unknown>);
      }
      close() {}
      emit(type: string, event: { data?: unknown } = {}) {
        for (const listener of this.listeners[type] ?? []) listener(event);
      }
    }

    afterEach(() => {
      vi.unstubAllGlobals();
      vi.restoreAllMocks();
    });

    /** Valle de 32 × 32 en tierra seca con un río en (1,0) y basura en (2,0). */
    function startGame() {
      vi.stubGlobal("WebSocket", SendingSocket);
      // happy-dom no dibuja: un contexto 2D que acepta cualquier llamada.
      const ctx = new Proxy({}, { get: () => () => undefined, set: () => true });
      vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue(
        ctx as unknown as CanvasRenderingContext2D,
      );
      const storage = memoryStorage({
        [VIEW_MODE_KEY]: "zenithal",
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
      const stages = new Array<number>(1024).fill(1);
      stages[1] = 6;
      const trash = new Array<number>(1024).fill(0);
      trash[2] = 1;
      act(() => {
        SendingSocket.last?.emit("open");
        SendingSocket.last?.emit("message", {
          data: JSON.stringify({
            type: "state",
            tick: "5",
            width: 32,
            height: 32,
            stages,
            trash,
            humidity: new Array<string>(1024).fill("0"),
            player: { water: "5", tank_capacity: "10", leaves: "50", next_sprinkler_price: "10" },
          }),
        });
      });
      const canvas = document.getElementById("valley") as HTMLCanvasElement;
      canvas.getBoundingClientRect = () =>
        ({ left: 0, top: 0, width: 512, height: 512, right: 512, bottom: 512, x: 0, y: 0 }) as DOMRect;
      const clickTile = (x: number, y: number) =>
        fireEvent.click(canvas, { clientX: x * 16 + 8, clientY: y * 16 + 8 });
      const sentTypes = () => (SendingSocket.last?.sent ?? []).map((f) => f["type"]);
      return { clickTile, sentTypes, socket: () => SendingSocket.last };
    }

    it("riega la tierra y recoge la basura sin botones intermedios", () => {
      const { clickTile, sentTypes } = startGame();
      clickTile(5, 5);
      clickTile(2, 0);
      expect(sentTypes()).toEqual(["drop", "clean_trash"]);
    });

    it("con el depósito vacío no envía la gota y avisa", () => {
      const { clickTile, sentTypes, socket } = startGame();
      act(() => {
        socket()?.emit("message", {
          data: JSON.stringify({
            type: "state",
            tick: "6",
            width: 32,
            height: 32,
            stages: new Array<number>(1024).fill(1),
            humidity: new Array<string>(1024).fill("0"),
            player: { water: "0", tank_capacity: "10", leaves: "50" },
          }),
        });
      });
      clickTile(5, 5);
      clickTile(6, 5);
      expect(sentTypes()).toEqual([]);
      expect(screen.getByText("Tu depósito está vacío: espera a que se recargue.")).toBeDefined();
    });

    it("no envía nada al hacer clic en el río y explica por qué", () => {
      const { clickTile, sentTypes } = startGame();
      clickTile(1, 0);
      expect(sentTypes()).toEqual([]);
      expect(screen.getByText("El río no se riega: aquí no hay nada que hacer.")).toBeDefined();
    });

    it("con una instalación elegida coloca una en cada clic hasta pulsar Esc", () => {
      const { clickTile, socket } = startGame();
      fireEvent.click(screen.getByRole("button", { name: /Aspersor/ }));
      expect(screen.getByText("Colocando: Aspersor")).toBeDefined();
      clickTile(4, 4);
      clickTile(6, 4);
      const placed = (socket()?.sent ?? []).filter((f) => f["type"] === "place_installation");
      expect(placed.map((f) => [f["installation"], f["x"], f["y"]])).toEqual([
        ["sprinkler", 4, 4],
        ["sprinkler", 6, 4],
      ]);

      fireEvent.keyDown(window, { key: "Escape" });
      expect(screen.queryByText("Colocando: Aspersor")).toBeNull();
      clickTile(8, 8);
      expect(socket()?.sent.at(-1)?.["type"]).toBe("drop");
    });
  });
});

// @vitest-environment happy-dom
import { describe, it, expect } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { GameApp } from "./GameApp.tsx";
import { VIEW_MODE_KEY } from "../logic/viewMode.ts";
import type { StorageLike } from "../logic/auth.ts";

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
});

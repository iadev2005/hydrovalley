// @vitest-environment happy-dom
import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import { GameApp } from "./GameApp.tsx";
import type { StorageLike } from "../logic/auth.ts";

function emptyStorage(): StorageLike {
  return {
    getItem: () => null,
    setItem: () => {},
    removeItem: () => {},
  };
}

describe("GameApp", () => {
  it("renders main heading, auth panel and valley canvas when unauthenticated", () => {
    render(
      <GameApp
        sessionOptions={{ storage: emptyStorage() }}
        socketOptions={{ location: { protocol: "http:", host: "localhost:8080" } }}
      />,
    );

    expect(screen.getByRole("heading", { level: 1, name: "hydrovalley" })).toBeDefined();
    expect(screen.getByRole("heading", { level: 2, name: "Acceso" })).toBeDefined();
    expect(screen.getByText("Sin estado todavía.")).toBeDefined();
    expect(document.getElementById("valley")).toBeDefined();
    expect(screen.getByText("desconectado")).toBeDefined();
  });
});

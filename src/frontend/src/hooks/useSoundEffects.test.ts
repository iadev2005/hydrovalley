// @vitest-environment happy-dom
import { describe, it, expect } from "vitest";
import { renderHook, act } from "@testing-library/react";
import { useSoundEffects } from "./useSoundEffects.ts";

function createMockStorage(initialData: Record<string, string> = {}): Storage {
  const store = new Map<string, string>(Object.entries(initialData));
  return {
    length: store.size,
    clear: () => store.clear(),
    key: (index: number) => Array.from(store.keys())[index] ?? null,
    getItem: (key: string) => store.get(key) ?? null,
    setItem: (key: string, value: string) => {
      store.set(key, value);
    },
    removeItem: (key: string) => {
      store.delete(key);
    },
  };
}

describe("useSoundEffects hook", () => {
  it("inicializa con estado no silenciado por defecto", () => {
    const storage = createMockStorage();
    const { result } = renderHook(() => useSoundEffects({ storage }));

    expect(result.current.isMuted).toBe(false);
  });

  it("inicializa con estado silenciado si el almacenamiento lo tiene como true", () => {
    const storage = createMockStorage({ hydrovalley_sfx_muted: "true" });
    const { result } = renderHook(() => useSoundEffects({ storage }));

    expect(result.current.isMuted).toBe(true);
  });

  it("permite alternar entre silenciado y activo con toggleMute()", () => {
    const storage = createMockStorage();
    const { result } = renderHook(() => useSoundEffects({ storage }));

    act(() => {
      const next = result.current.toggleMute();
      expect(next).toBe(true);
    });

    expect(result.current.isMuted).toBe(true);
    expect(storage.getItem("hydrovalley_sfx_muted")).toBe("true");

    act(() => {
      const next = result.current.toggleMute();
      expect(next).toBe(false);
    });

    expect(result.current.isMuted).toBe(false);
    expect(storage.getItem("hydrovalley_sfx_muted")).toBe("false");
  });

  it("permite establecer explícitamente el valor con setMuted()", () => {
    const storage = createMockStorage();
    const { result } = renderHook(() => useSoundEffects({ storage }));

    act(() => {
      result.current.setMuted(true);
    });

    expect(result.current.isMuted).toBe(true);
  });

  it("expone métodos de disparo de sonido seguros", () => {
    const storage = createMockStorage();
    const { result } = renderHook(() => useSoundEffects({ storage }));

    expect(() => {
      act(() => {
        result.current.playWaterDrop();
        result.current.playInstall();
        result.current.playExtinguish();
        result.current.playFireAlert();
        result.current.playClick();
        result.current.playDockToggle();
      });
    }).not.toThrow();
  });
});

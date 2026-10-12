// @vitest-environment happy-dom
import { describe, it, expect } from "vitest";
import { renderHook, act } from "@testing-library/react";
import { useValleyView } from "./useValleyView.ts";
import { VIEW_MODE_KEY } from "../logic/viewMode.ts";
import type { StorageLike } from "../logic/auth.ts";

function memoryStorage(initial: Record<string, string> = {}): StorageLike {
  const data = new Map(Object.entries(initial));
  return {
    getItem: (k: string) => (data.has(k) ? (data.get(k) ?? null) : null),
    setItem: (k: string, v: string) => data.set(k, String(v)),
    removeItem: (k: string) => {
      data.delete(k);
    },
  };
}

describe("useValleyView", () => {
  it("inicializa con 'isometric' por defecto y sin fallback activo si el almacenamiento está vacío", () => {
    const storage = memoryStorage();
    const { result } = renderHook(() =>
      useValleyView({ storage, isPhaserSupportedFn: () => true }),
    );

    expect(result.current.viewMode).toBe("isometric");
    expect(result.current.preferredMode).toBe("isometric");
    expect(result.current.hasPhaserFailed).toBe(false);
    expect(result.current.isFallbackActive).toBe(false);
  });

  it("recupera 'zenithal' desde el almacenamiento si el usuario lo guardó previamente", () => {
    const storage = memoryStorage({ [VIEW_MODE_KEY]: "zenithal" });
    const { result } = renderHook(() =>
      useValleyView({ storage, isPhaserSupportedFn: () => true }),
    );

    expect(result.current.viewMode).toBe("zenithal");
    expect(result.current.preferredMode).toBe("zenithal");
    expect(result.current.isFallbackActive).toBe(false);
  });

  it("permite alternar entre modos con toggleView y persiste en el almacenamiento", () => {
    const storage = memoryStorage();
    const { result } = renderHook(() =>
      useValleyView({ storage, isPhaserSupportedFn: () => true }),
    );

    expect(result.current.viewMode).toBe("isometric");

    act(() => {
      result.current.toggleView();
    });

    expect(result.current.viewMode).toBe("zenithal");
    expect(result.current.preferredMode).toBe("zenithal");
    expect(storage.getItem(VIEW_MODE_KEY)).toBe("zenithal");

    act(() => {
      result.current.toggleView();
    });

    expect(result.current.viewMode).toBe("isometric");
    expect(result.current.preferredMode).toBe("isometric");
    expect(storage.getItem(VIEW_MODE_KEY)).toBe("isometric");
  });

  it("salida de emergencia: conmuta automáticamente a 'zenithal' ante error de Phaser", () => {
    const storage = memoryStorage({ [VIEW_MODE_KEY]: "isometric" });
    const { result } = renderHook(() =>
      useValleyView({ storage, isPhaserSupportedFn: () => true }),
    );

    expect(result.current.viewMode).toBe("isometric");
    expect(result.current.isFallbackActive).toBe(false);

    act(() => {
      result.current.reportPhaserError(new Error("Cannot create Canvas context, aborting."));
    });

    expect(result.current.viewMode).toBe("zenithal");
    expect(result.current.hasPhaserFailed).toBe(true);
    expect(result.current.isFallbackActive).toBe(true);
  });

  it("salida de emergencia: si el entorno no soporta Phaser, activa 'zenithal' inmediatamente con fallback activo", () => {
    const storage = memoryStorage({ [VIEW_MODE_KEY]: "isometric" });
    const { result } = renderHook(() =>
      useValleyView({ storage, isPhaserSupportedFn: () => false }),
    );

    expect(result.current.viewMode).toBe("zenithal");
    expect(result.current.preferredMode).toBe("isometric");
    expect(result.current.isFallbackActive).toBe(true);
  });
});

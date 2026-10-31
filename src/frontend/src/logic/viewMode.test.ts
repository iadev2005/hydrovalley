import { describe, it, expect } from "vitest";
import type { StorageLike } from "./auth.ts";
import {
  DEFAULT_VIEW_MODE,
  VIEW_MODE_KEY,
  isPhaserSupported,
  loadSavedViewMode,
  resolveEffectiveViewMode,
  saveViewMode,
} from "./viewMode.ts";

function createMockStorage(initialData: Record<string, string> = {}): StorageLike {
  const store = new Map<string, string>(Object.entries(initialData));
  return {
    getItem: (key: string) => store.get(key) ?? null,
    setItem: (key: string, value: string) => {
      store.set(key, value);
    },
    removeItem: (key: string) => {
      store.delete(key);
    },
  };
}

describe("viewMode logic", () => {
  describe("loadSavedViewMode", () => {
    it("devuelve 'isometric' por defecto si el almacenamiento está vacío o es nulo", () => {
      expect(loadSavedViewMode(null)).toBe(DEFAULT_VIEW_MODE);
      expect(loadSavedViewMode(createMockStorage())).toBe("isometric");
    });

    it("recupera 'isometric' o 'zenithal' correctamente desde el almacenamiento", () => {
      const storageIso = createMockStorage({ [VIEW_MODE_KEY]: "isometric" });
      const storageZen = createMockStorage({ [VIEW_MODE_KEY]: "zenithal" });

      expect(loadSavedViewMode(storageIso)).toBe("isometric");
      expect(loadSavedViewMode(storageZen)).toBe("zenithal");
    });

    it("ignora valores inválidos o corruptos y recurre a 'isometric'", () => {
      const storageInvalid = createMockStorage({ [VIEW_MODE_KEY]: "invalid_mode" });
      expect(loadSavedViewMode(storageInvalid)).toBe("isometric");
    });

    it("maneja excepciones al acceder al almacenamiento retornando el valor por defecto", () => {
      const throwingStorage: StorageLike = {
        getItem: () => {
          throw new Error("QuotaExceeded or security restriction");
        },
        setItem: () => {},
        removeItem: () => {},
      };
      expect(loadSavedViewMode(throwingStorage)).toBe(DEFAULT_VIEW_MODE);
    });
  });

  describe("saveViewMode", () => {
    it("guarda el modo seleccionado en el almacenamiento", () => {
      const storage = createMockStorage();
      saveViewMode("zenithal", storage);
      expect(storage.getItem(VIEW_MODE_KEY)).toBe("zenithal");

      saveViewMode("isometric", storage);
      expect(storage.getItem(VIEW_MODE_KEY)).toBe("isometric");
    });

    it("no lanza error si el almacenamiento es nulo o lanza una excepción", () => {
      expect(() => saveViewMode("zenithal", null)).not.toThrow();

      const throwingStorage: StorageLike = {
        getItem: () => null,
        setItem: () => {
          throw new Error("Disk full or private mode block");
        },
        removeItem: () => {},
      };
      expect(() => saveViewMode("zenithal", throwingStorage)).not.toThrow();
    });
  });

  describe("resolveEffectiveViewMode", () => {
    it("devuelve 'isometric' si se prefiere isométrica y Phaser está disponible y sin fallos", () => {
      const mode = resolveEffectiveViewMode({
        preferredMode: "isometric",
        phaserSupported: true,
        phaserFailed: false,
      });
      expect(mode).toBe("isometric");
    });

    it("devuelve 'zenithal' si la preferencia del usuario es 'zenithal'", () => {
      const mode = resolveEffectiveViewMode({
        preferredMode: "zenithal",
        phaserSupported: true,
        phaserFailed: false,
      });
      expect(mode).toBe("zenithal");
    });

    it("salida de emergencia: fuerza 'zenithal' si Phaser falló durante la ejecución o carga", () => {
      const mode = resolveEffectiveViewMode({
        preferredMode: "isometric",
        phaserSupported: true,
        phaserFailed: true,
      });
      expect(mode).toBe("zenithal");
    });

    it("salida de emergencia: fuerza 'zenithal' si el entorno no soporta Phaser", () => {
      const mode = resolveEffectiveViewMode({
        preferredMode: "isometric",
        phaserSupported: false,
        phaserFailed: false,
      });
      expect(mode).toBe("zenithal");
    });
  });

  describe("isPhaserSupported", () => {
    it("devuelve false si el documento es nulo (ej. entorno SSR o Node sin DOM)", () => {
      expect(isPhaserSupported(null)).toBe(false);
    });

    it("devuelve true si el elemento canvas puede crear contextos WebGL o 2D", () => {
      const mockDoc = {
        createElement: (tag: string) => {
          if (tag === "canvas") {
            return {
              getContext: (ctx: string) => (ctx === "2d" ? {} : null),
            };
          }
          return {};
        },
      } as unknown as Document;

      expect(isPhaserSupported(mockDoc)).toBe(true);
    });

    it("devuelve false si createElement lanza un error o getContext no existe", () => {
      const mockDocThrow = {
        createElement: () => {
          throw new Error("DOM not ready");
        },
      } as unknown as Document;

      expect(isPhaserSupported(mockDocThrow)).toBe(false);

      const mockDocNoCtx = {
        createElement: () => ({ getContext: null }),
      } as unknown as Document;

      expect(isPhaserSupported(mockDocNoCtx)).toBe(false);
    });
  });
});

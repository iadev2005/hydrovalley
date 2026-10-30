import { describe, it, expect, vi } from "vitest";
import { createAudioManager, type SoundEffectType } from "./audio.ts";

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

function createMockAudioContext() {
  const destination = {} as AudioNode;
  const mockGainNode = {
    gain: {
      setValueAtTime: vi.fn(),
      linearRampToValueAtTime: vi.fn(),
      exponentialRampToValueAtTime: vi.fn(),
    },
    connect: vi.fn(),
  };

  const mockOscillator = {
    type: "sine",
    frequency: {
      setValueAtTime: vi.fn(),
      exponentialRampToValueAtTime: vi.fn(),
    },
    connect: vi.fn(),
    start: vi.fn(),
    stop: vi.fn(),
  };

  const mockBufferSource = {
    buffer: null as unknown,
    connect: vi.fn(),
    start: vi.fn(),
    stop: vi.fn(),
  };

  const mockFilter = {
    type: "lowpass",
    frequency: {
      setValueAtTime: vi.fn(),
      exponentialRampToValueAtTime: vi.fn(),
    },
    connect: vi.fn(),
  };

  const mockBuffer = {
    getChannelData: vi.fn(() => new Float32Array(100)),
  };

  let audioState: AudioContextState = "suspended";

  const ctx = {
    get state() {
      return audioState;
    },
    currentTime: 10,
    sampleRate: 44100,
    destination,
    createGain: vi.fn(() => mockGainNode),
    createOscillator: vi.fn(() => mockOscillator),
    createBufferSource: vi.fn(() => mockBufferSource),
    createBiquadFilter: vi.fn(() => mockFilter),
    createBuffer: vi.fn(() => mockBuffer),
    resume: vi.fn(async () => {
      audioState = "running";
    }),
    close: vi.fn(async () => {
      audioState = "closed";
    }),
  } as unknown as AudioContext;

  return { ctx, mockGainNode, mockOscillator, mockBufferSource };
}

describe("audio logic", () => {
  it("inicia sin silenciar por defecto si el almacenamiento está vacío", () => {
    const storage = createMockStorage();
    const manager = createAudioManager({ storage });

    expect(manager.isMuted()).toBe(false);
  });

  it("recupera el estado silenciado desde el almacenamiento si está configurado en 'true'", () => {
    const storage = createMockStorage({ hydrovalley_sfx_muted: "true" });
    const manager = createAudioManager({ storage });

    expect(manager.isMuted()).toBe(true);
  });

  it("permite silenciar y reactivar el sonido persistiendo en el almacenamiento", () => {
    const storage = createMockStorage();
    const manager = createAudioManager({ storage });

    expect(manager.toggleMute()).toBe(true);
    expect(manager.isMuted()).toBe(true);
    expect(storage.getItem("hydrovalley_sfx_muted")).toBe("true");

    expect(manager.toggleMute()).toBe(false);
    expect(manager.isMuted()).toBe(false);
    expect(storage.getItem("hydrovalley_sfx_muted")).toBe("false");

    manager.setMuted(true);
    expect(manager.isMuted()).toBe(true);
  });

  it("no ejecuta síntesis de audio si el sonido está silenciado", () => {
    const { ctx } = createMockAudioContext();
    const createCtx = vi.fn(() => ctx);
    const storage = createMockStorage({ hydrovalley_sfx_muted: "true" });

    const manager = createAudioManager({ storage, createAudioContext: createCtx });
    manager.play("waterDrop");

    expect(createCtx).not.toHaveBeenCalled();
  });

  it("sintetiza correctamente cada tipo de sonido cuando no está silenciado", () => {
    const soundTypes: SoundEffectType[] = [
      "waterDrop",
      "install",
      "extinguish",
      "fireAlert",
      "click",
      "dockToggle",
    ];

    soundTypes.forEach((type) => {
      const { ctx } = createMockAudioContext();
      const manager = createAudioManager({
        storage: createMockStorage(),
        createAudioContext: () => ctx,
      });

      manager.play(type);
      expect(ctx.resume).toHaveBeenCalled();
    });
  });

  it("cierra el AudioContext al invocar close()", () => {
    const { ctx } = createMockAudioContext();
    const manager = createAudioManager({
      storage: createMockStorage(),
      createAudioContext: () => ctx,
    });

    manager.play("click");
    manager.close();

    expect(ctx.close).toHaveBeenCalled();
  });

  it("maneja con gracia la ausencia de AudioContext sin arrojar excepciones", () => {
    const manager = createAudioManager({
      storage: createMockStorage(),
      createAudioContext: () => null,
    });

    expect(() => {
      manager.play("waterDrop");
      manager.close();
    }).not.toThrow();
  });
});

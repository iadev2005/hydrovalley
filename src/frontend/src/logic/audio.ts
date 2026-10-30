import type { StorageLike } from "./auth.ts";

export type SoundEffectType =
  | "waterDrop"
  | "install"
  | "extinguish"
  | "fireAlert"
  | "click"
  | "dockToggle";

export interface AudioManagerOptions {
  storage?: StorageLike | Storage;
  storageKey?: string;
  createAudioContext?: () => AudioContext | null;
}

export interface AudioManager {
  isMuted: () => boolean;
  setMuted: (muted: boolean) => void;
  toggleMute: () => boolean;
  play: (type: SoundEffectType) => void;
  close: () => void;
}

const DEFAULT_STORAGE_KEY = "hydrovalley_sfx_muted";

export function createAudioManager(options: AudioManagerOptions = {}): AudioManager {
  const {
    storage = typeof window !== "undefined" ? window.localStorage : undefined,
    storageKey = DEFAULT_STORAGE_KEY,
    createAudioContext,
  } = options;

  let muted = false;
  if (storage) {
    try {
      const stored = storage.getItem(storageKey);
      if (stored !== null) {
        muted = stored === "true";
      }
    } catch {
      // Ignorar fallos de acceso a storage
    }
  }

  let audioCtx: AudioContext | null = null;
  let masterGain: GainNode | null = null;

  function getAudioContext(): AudioContext | null {
    if (audioCtx) return audioCtx;

    try {
      if (createAudioContext) {
        audioCtx = createAudioContext();
      } else if (typeof window !== "undefined") {
        const AudioContextClass =
          window.AudioContext ||
          (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext;
        if (AudioContextClass) {
          audioCtx = new AudioContextClass();
        }
      }
    } catch {
      audioCtx = null;
    }

    if (audioCtx) {
      try {
        masterGain = audioCtx.createGain();
        masterGain.gain.setValueAtTime(0.2, audioCtx.currentTime);
        masterGain.connect(audioCtx.destination);
      } catch {
        masterGain = null;
      }
    }

    return audioCtx;
  }

  function ensureResumed(ctx: AudioContext): void {
    if (ctx.state === "suspended") {
      ctx.resume().catch(() => {});
    }
  }

  function playWaterDrop(ctx: AudioContext, dest: GainNode): void {
    const now = ctx.currentTime;
    const osc = ctx.createOscillator();
    const gain = ctx.createGain();

    osc.type = "sine";
    osc.frequency.setValueAtTime(620, now);
    osc.frequency.exponentialRampToValueAtTime(220, now + 0.12);

    gain.gain.setValueAtTime(0.001, now);
    gain.gain.linearRampToValueAtTime(0.4, now + 0.015);
    gain.gain.exponentialRampToValueAtTime(0.001, now + 0.12);

    osc.connect(gain);
    gain.connect(dest);

    osc.start(now);
    osc.stop(now + 0.13);
  }

  function playInstall(ctx: AudioContext, dest: GainNode): void {
    const now = ctx.currentTime;
    // Dos tonos armónicos para simular construcción / instalación
    const freqs = [440, 659.25]; // La4 y Mi5 (quinta justa)
    freqs.forEach((freq, idx) => {
      const osc = ctx.createOscillator();
      const gain = ctx.createGain();

      osc.type = idx === 0 ? "triangle" : "sine";
      osc.frequency.setValueAtTime(freq, now);
      osc.frequency.exponentialRampToValueAtTime(freq * 1.25, now + 0.16);

      gain.gain.setValueAtTime(0.001, now);
      gain.gain.linearRampToValueAtTime(0.25, now + 0.02);
      gain.gain.exponentialRampToValueAtTime(0.001, now + 0.16);

      osc.connect(gain);
      gain.connect(dest);

      osc.start(now);
      osc.stop(now + 0.17);
    });
  }

  function playExtinguish(ctx: AudioContext, dest: GainNode): void {
    const now = ctx.currentTime;
    const duration = 0.2;
    const sampleRate = ctx.sampleRate;
    const bufferSize = Math.floor(sampleRate * duration);
    const buffer = ctx.createBuffer(1, bufferSize, sampleRate);
    const data = buffer.getChannelData(0);

    for (let i = 0; i < bufferSize; i++) {
      data[i] = Math.random() * 2 - 1;
    }

    const noise = ctx.createBufferSource();
    noise.buffer = buffer;

    const filter = ctx.createBiquadFilter();
    filter.type = "lowpass";
    filter.frequency.setValueAtTime(2200, now);
    filter.frequency.exponentialRampToValueAtTime(350, now + duration);

    const gain = ctx.createGain();
    gain.gain.setValueAtTime(0.001, now);
    gain.gain.linearRampToValueAtTime(0.3, now + 0.02);
    gain.gain.exponentialRampToValueAtTime(0.001, now + duration);

    noise.connect(filter);
    filter.connect(gain);
    gain.connect(dest);

    noise.start(now);
    noise.stop(now + duration);
  }

  function playFireAlert(ctx: AudioContext, dest: GainNode): void {
    const now = ctx.currentTime;
    // Doble beeping de aviso
    const beeps = [
      { start: 0, freq: 880, dur: 0.07 },
      { start: 0.09, freq: 1100, dur: 0.09 },
    ];

    beeps.forEach((beep) => {
      const osc = ctx.createOscillator();
      const gain = ctx.createGain();

      osc.type = "sine";
      osc.frequency.setValueAtTime(beep.freq, now + beep.start);

      gain.gain.setValueAtTime(0.001, now + beep.start);
      gain.gain.linearRampToValueAtTime(0.25, now + beep.start + 0.01);
      gain.gain.exponentialRampToValueAtTime(0.001, now + beep.start + beep.dur);

      osc.connect(gain);
      gain.connect(dest);

      osc.start(now + beep.start);
      osc.stop(now + beep.start + beep.dur);
    });
  }

  function playClick(ctx: AudioContext, dest: GainNode): void {
    const now = ctx.currentTime;
    const osc = ctx.createOscillator();
    const gain = ctx.createGain();

    osc.type = "sine";
    osc.frequency.setValueAtTime(1150, now);
    osc.frequency.exponentialRampToValueAtTime(450, now + 0.025);

    gain.gain.setValueAtTime(0.001, now);
    gain.gain.linearRampToValueAtTime(0.2, now + 0.004);
    gain.gain.exponentialRampToValueAtTime(0.001, now + 0.025);

    osc.connect(gain);
    gain.connect(dest);

    osc.start(now);
    osc.stop(now + 0.03);
  }

  function playDockToggle(ctx: AudioContext, dest: GainNode): void {
    const now = ctx.currentTime;
    const osc = ctx.createOscillator();
    const gain = ctx.createGain();

    osc.type = "triangle";
    osc.frequency.setValueAtTime(280, now);
    osc.frequency.exponentialRampToValueAtTime(520, now + 0.07);

    gain.gain.setValueAtTime(0.001, now);
    gain.gain.linearRampToValueAtTime(0.25, now + 0.015);
    gain.gain.exponentialRampToValueAtTime(0.001, now + 0.07);

    osc.connect(gain);
    gain.connect(dest);

    osc.start(now);
    osc.stop(now + 0.08);
  }

  return {
    isMuted: () => muted,
    setMuted: (nextMuted: boolean) => {
      muted = nextMuted;
      if (storage) {
        try {
          storage.setItem(storageKey, String(muted));
        } catch {
          // Ignorar fallos de almacenamiento
        }
      }
    },
    toggleMute: () => {
      muted = !muted;
      if (storage) {
        try {
          storage.setItem(storageKey, String(muted));
        } catch {
          // Ignorar fallos de almacenamiento
        }
      }
      return muted;
    },
    play: (type: SoundEffectType) => {
      if (muted) return;

      const ctx = getAudioContext();
      if (!ctx || !masterGain) return;

      ensureResumed(ctx);

      try {
        switch (type) {
          case "waterDrop":
            playWaterDrop(ctx, masterGain);
            break;
          case "install":
            playInstall(ctx, masterGain);
            break;
          case "extinguish":
            playExtinguish(ctx, masterGain);
            break;
          case "fireAlert":
            playFireAlert(ctx, masterGain);
            break;
          case "click":
            playClick(ctx, masterGain);
            break;
          case "dockToggle":
            playDockToggle(ctx, masterGain);
            break;
        }
      } catch {
        // Ignorar fallos de reproducción de audio para no interrumpir el juego
      }
    },
    close: () => {
      if (audioCtx && audioCtx.state !== "closed") {
        audioCtx.close().catch(() => {});
      }
      audioCtx = null;
      masterGain = null;
    },
  };
}

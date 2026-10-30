import { useCallback, useEffect, useState } from "react";
import {
  createAudioManager,
  type AudioManagerOptions,
  type SoundEffectType,
} from "../logic/audio.ts";

export type UseSoundEffectsOptions = AudioManagerOptions;

export function useSoundEffects(options: UseSoundEffectsOptions = {}) {
  const [manager] = useState(() => createAudioManager(options));
  const [isMuted, setIsMuted] = useState<boolean>(() => manager.isMuted());

  useEffect(() => {
    return () => {
      manager.close();
    };
  }, [manager]);

  const toggleMute = useCallback(() => {
    const next = manager.toggleMute();
    setIsMuted(next);
    return next;
  }, [manager]);

  const setMuted = useCallback((muted: boolean) => {
    manager.setMuted(muted);
    setIsMuted(muted);
  }, [manager]);

  const play = useCallback((type: SoundEffectType) => {
    manager.play(type);
  }, [manager]);

  const playWaterDrop = useCallback(() => play("waterDrop"), [play]);
  const playInstall = useCallback(() => play("install"), [play]);
  const playExtinguish = useCallback(() => play("extinguish"), [play]);
  const playFireAlert = useCallback(() => play("fireAlert"), [play]);
  const playClick = useCallback(() => play("click"), [play]);
  const playDockToggle = useCallback(() => play("dockToggle"), [play]);

  return {
    isMuted,
    toggleMute,
    setMuted,
    play,
    playWaterDrop,
    playInstall,
    playExtinguish,
    playFireAlert,
    playClick,
    playDockToggle,
  };
}

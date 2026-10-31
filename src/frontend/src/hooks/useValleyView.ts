// Hook de React para gestionar el modo de visualización del valle (isométrica vs. cenital),
// la persistencia de preferencias en almacenamiento local y la salida de emergencia
// automática hacia la vista cenital si Phaser falla o no está soportado.

import { useCallback, useState } from "react";
import type { StorageLike } from "../logic/auth.ts";
import {
  isPhaserSupported,
  loadSavedViewMode,
  resolveEffectiveViewMode,
  saveViewMode,
  type ValleyViewMode,
} from "../logic/viewMode.ts";

export interface UseValleyViewOptions {
  storage?: StorageLike;
  isPhaserSupportedFn?: () => boolean;
}

export interface UseValleyViewReturn {
  /** Modo de vista efectivo que se está renderizando actualmente en pantalla. */
  viewMode: ValleyViewMode;
  /** Preferencia guardada del jugador en el almacenamiento local. */
  preferredMode: ValleyViewMode;
  /** Indica si Phaser falló durante la instanciación o ejecución. */
  hasPhaserFailed: boolean;
  /** Indica si la vista cenital está activa debido a un fallo o falta de soporte gráfico. */
  isFallbackActive: boolean;
  /** Alterna entre vista isométrica y cenital, actualizando la preferencia. */
  toggleView: () => void;
  /** Establece una preferencia de vista explícita. */
  setViewMode: (mode: ValleyViewMode) => void;
  /** Callback para notificar que Phaser falló, forzando la salida de emergencia a cenital. */
  reportPhaserError: (err?: unknown) => void;
}

export function useValleyView({
  storage = typeof window !== "undefined" ? window.localStorage : undefined,
  isPhaserSupportedFn = isPhaserSupported,
}: UseValleyViewOptions = {}): UseValleyViewReturn {
  const [preferredMode, setPreferredModeState] = useState<ValleyViewMode>(() =>
    loadSavedViewMode(storage),
  );
  const [hasPhaserFailed, setHasPhaserFailed] = useState<boolean>(false);

  const phaserSupported = isPhaserSupportedFn();

  const viewMode = resolveEffectiveViewMode({
    preferredMode,
    phaserSupported,
    phaserFailed: hasPhaserFailed,
  });

  const isFallbackActive = preferredMode === "isometric" && viewMode === "zenithal";

  const setViewMode = useCallback(
    (mode: ValleyViewMode) => {
      setPreferredModeState(mode);
      saveViewMode(mode, storage);
    },
    [storage],
  );

  const toggleView = useCallback(() => {
    const next: ValleyViewMode = viewMode === "isometric" ? "zenithal" : "isometric";
    setViewMode(next);
  }, [viewMode, setViewMode]);

  const reportPhaserError = useCallback((err?: unknown) => {
    if (err) {
      console.warn("Phaser falló al inicializar, activando vista cenital de emergencia:", err);
    }
    setHasPhaserFailed(true);
  }, []);

  return {
    viewMode,
    preferredMode,
    hasPhaserFailed,
    isFallbackActive,
    toggleView,
    setViewMode,
    reportPhaserError,
  };
}

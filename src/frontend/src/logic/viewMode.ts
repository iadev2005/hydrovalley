// Lógica pura de selección de vista del valle (isométrica vs. cenital) y
// persistencia de preferencias de usuario. Desacoplada del DOM para poder
// probarla exhaustivamente con vitest sin depender de Phaser.

import type { StorageLike } from "./auth.ts";

/** Modos de visualización disponibles para el valle. */
export type ValleyViewMode = "isometric" | "zenithal";

/** Clave de `localStorage` donde se almacena la preferencia del jugador. */
export const VIEW_MODE_KEY = "hydrovalley.view_mode";

/** Vista predeterminada del juego (nivel 1 de CLAUDE.md: valle isométrico). */
export const DEFAULT_VIEW_MODE: ValleyViewMode = "isometric";

/**
 * Validador para asegurar que el modo leído es un valor válido.
 */
function isValidViewMode(val: unknown): val is ValleyViewMode {
  return val === "isometric" || val === "zenithal";
}

/**
 * Recupera la preferencia guardada de la vista del valle. Si no existe,
 * está corrupta o el almacenamiento no está disponible, recurre al valor por defecto.
 */
export function loadSavedViewMode(storage?: StorageLike | null): ValleyViewMode {
  if (!storage) return DEFAULT_VIEW_MODE;
  try {
    const saved = storage.getItem(VIEW_MODE_KEY);
    if (isValidViewMode(saved)) {
      return saved;
    }
  } catch {
    // Protección ante entornos privados o restricciones de acceso a almacenamiento
  }
  return DEFAULT_VIEW_MODE;
}

/**
 * Guarda la preferencia de vista del valle en el almacenamiento especificado.
 */
export function saveViewMode(mode: ValleyViewMode, storage?: StorageLike | null): void {
  if (!storage) return;
  try {
    storage.setItem(VIEW_MODE_KEY, mode);
  } catch {
    // Si la cuota está llena o el almacenamiento está bloqueado, se ignora silenciosamente
  }
}

export interface ResolveViewModeOptions {
  preferredMode: ValleyViewMode;
  phaserSupported: boolean;
  phaserFailed: boolean;
}

/**
 * Decide la vista efectiva a renderizar.
 * Salida de emergencia: si Phaser no está soportado en el dispositivo o falló
 * al instanciarse/ejecutarse, se conmuta automáticamente a vista cenital (2D)
 * para garantizar que el juego continúe siempre jugable.
 */
export function resolveEffectiveViewMode({
  preferredMode,
  phaserSupported,
  phaserFailed,
}: ResolveViewModeOptions): ValleyViewMode {
  if (!phaserSupported || phaserFailed) {
    return "zenithal";
  }
  return preferredMode;
}

/**
 * Comprueba de forma segura si el entorno actual soporta capacidades mínimas
 * de renderizado gráfico (Canvas2D / WebGL) requeridas por Phaser.
 */
export function isPhaserSupported(
  doc: Document | null = typeof document !== "undefined" ? document : null,
): boolean {
  if (!doc) return false;
  try {
    const canvas = doc.createElement("canvas");
    return Boolean(
      canvas.getContext &&
        (canvas.getContext("webgl") ||
          canvas.getContext("experimental-webgl") ||
          canvas.getContext("2d")),
    );
  } catch {
    return false;
  }
}

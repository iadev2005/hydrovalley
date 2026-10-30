import { useEffect } from "react";
import { GRID_SIZE } from "../logic/play.ts";

export interface UseKeyboardShortcutsOptions {
  enabled?: boolean;
  selected: { x: number; y: number } | null;
  onSelectTile: (coord: { x: number; y: number } | null) => void;
  onDrop: () => void;
  onPlaceInstallation: (installation: string) => void;
  onToggleView?: () => void;
  onFocusFire?: () => void;
  onCenterSelected?: () => void;
  onResetCamera?: () => void;
  canActOnTile?: boolean;
  gridSize?: number;
}

/**
 * Comprueba si el elemento activo es un campo de formulario o contenido editable
 * para no interferir cuando el usuario escribe en el chat o en login.
 */
export function isEditableElement(element: Element | null): boolean {
  if (!element) return false;
  const tagName = element.tagName.toLowerCase();
  return (
    tagName === "input" ||
    tagName === "textarea" ||
    tagName === "select" ||
    (element as HTMLElement).isContentEditable
  );
}

/**
 * Hook para gestionar atajos de teclado globales en Hydrovalley.
 * WASD / Flechas -> Navegación de casillas
 * Espacio / Enter -> Soltar gota
 * 1, 2, 3 -> Comprar instalaciones 3R (aspersor, goteo, planta)
 * F -> Centrar/ciclar en incendios activos
 * C -> Centrar cámara en casilla seleccionada
 * R / 0 -> Restablecer cámara
 * V -> Alternar vista isométrica / cenital
 * Escape -> Deseleccionar casilla
 */
export function useKeyboardShortcuts({
  enabled = true,
  selected,
  onSelectTile,
  onDrop,
  onPlaceInstallation,
  onToggleView,
  onFocusFire,
  onCenterSelected,
  onResetCamera,
  canActOnTile = false,
  gridSize = GRID_SIZE,
}: UseKeyboardShortcutsOptions): void {
  useEffect(() => {
    if (!enabled) return;

    const handleKeyDown = (event: KeyboardEvent) => {
      // Ignorar si el foco está en un input o si el evento ya fue consumido
      if (event.defaultPrevented) return;
      if (isEditableElement(document.activeElement)) return;

      // No interferir con atajos del navegador del sistema (Ctrl, Cmd, Alt)
      if (event.ctrlKey || event.metaKey || event.altKey) return;

      const key = event.key;

      // 1. Deseleccionar casilla
      if (key === "Escape") {
        if (selected !== null) {
          event.preventDefault();
          onSelectTile(null);
        }
        return;
      }

      // 2. Navegación por casillas (WASD o Flechas)
      const isUp = key === "ArrowUp" || key === "w" || key === "W";
      const isDown = key === "ArrowDown" || key === "s" || key === "S";
      const isLeft = key === "ArrowLeft" || key === "a" || key === "A";
      const isRight = key === "ArrowRight" || key === "d" || key === "D";

      if (isUp || isDown || isLeft || isRight) {
        event.preventDefault();
        const base = selected ?? {
          x: Math.floor(gridSize / 2),
          y: Math.floor(gridSize / 2),
        };

        const next = {
          x: Math.min(
            gridSize - 1,
            Math.max(0, base.x + (isLeft ? -1 : isRight ? 1 : 0)),
          ),
          y: Math.min(
            gridSize - 1,
            Math.max(0, base.y + (isUp ? -1 : isDown ? 1 : 0)),
          ),
        };

        onSelectTile(next);
        return;
      }

      // 3. Acción principal: Soltar gota (Espacio o Enter)
      if (key === " " || key === "Space" || key === "Enter") {
        event.preventDefault();
        if (canActOnTile) {
          onDrop();
        }
        return;
      }

      // 4. Instalaciones rápidas 3R (1, 2, 3)
      if (key === "1") {
        if (canActOnTile) {
          event.preventDefault();
          onPlaceInstallation("sprinkler");
        }
        return;
      }
      if (key === "2") {
        if (canActOnTile) {
          event.preventDefault();
          onPlaceInstallation("drip");
        }
        return;
      }
      if (key === "3") {
        if (canActOnTile) {
          event.preventDefault();
          onPlaceInstallation("treatment_plant");
        }
        return;
      }

      // 5. Enfocar incendio activo (F)
      if (key === "f" || key === "F") {
        event.preventDefault();
        onFocusFire?.();
        return;
      }

      // 6. Centrar cámara en selección (C)
      if (key === "c" || key === "C") {
        event.preventDefault();
        onCenterSelected?.();
        return;
      }

      // 7. Restablecer encuadre de la cámara (R o 0)
      if (key === "r" || key === "R" || key === "0") {
        event.preventDefault();
        onResetCamera?.();
        return;
      }

      // 8. Alternar vista isométrica / cenital (V)
      if (key === "v" || key === "V") {
        event.preventDefault();
        onToggleView?.();
        return;
      }
    };

    window.addEventListener("keydown", handleKeyDown);
    return () => {
      window.removeEventListener("keydown", handleKeyDown);
    };
  }, [
    enabled,
    selected,
    onSelectTile,
    onDrop,
    onPlaceInstallation,
    onToggleView,
    onFocusFire,
    onCenterSelected,
    onResetCamera,
    canActOnTile,
    gridSize,
  ]);
}

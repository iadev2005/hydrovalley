import {
  forwardRef,
  useCallback,
  useEffect,
  useImperativeHandle,
  useRef,
  type ForwardedRef,
  type ReactNode,
} from "react";
import Phaser from "phaser";
import { ValleyScene } from "../game/ValleyScene.ts";
import type { StateFrame } from "../logic/play.ts";

export interface ValleyPhaserHandle {
  spawnPendingDrop: (actionId: string, x: number, y: number) => void;
  resolvePendingDrop: (actionId: string, status: string) => void;
  clearPendingDrops: () => void;
  resetCamera: () => void;
}

export interface ValleyPhaserProps {
  gameState: StateFrame | null;
  selected: { x: number; y: number } | null;
  onSelectTile: (coord: { x: number; y: number }) => void;
  onError?: (error: unknown) => void;
  toolbarExtra?: ReactNode;
}

function isCanvasSupported(): boolean {
  if (typeof document === "undefined") return false;
  try {
    const canvas = document.createElement("canvas");
    return Boolean(canvas.getContext && canvas.getContext("2d"));
  } catch {
    return false;
  }
}

export const ValleyPhaser = forwardRef(function ValleyPhaser(
  { gameState, selected, onSelectTile, onError, toolbarExtra }: ValleyPhaserProps,
  ref: ForwardedRef<ValleyPhaserHandle>,
) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const gameRef = useRef<Phaser.Game | null>(null);
  const sceneRef = useRef<ValleyScene | null>(null);

  const getScene = useCallback((): ValleyScene | null => {
    if (sceneRef.current) return sceneRef.current;
    if (gameRef.current) {
      const scene = gameRef.current.scene.getScene(ValleyScene.KEY) as ValleyScene | null;
      if (scene) {
        sceneRef.current = scene;
        return scene;
      }
    }
    return null;
  }, []);

  useImperativeHandle(
    ref,
    () => ({
      spawnPendingDrop: (actionId: string, x: number, y: number) => {
        getScene()?.spawnPendingDrop(actionId, x, y);
      },
      resolvePendingDrop: (actionId: string, status: string) => {
        getScene()?.resolvePendingDrop(actionId, status);
      },
      clearPendingDrops: () => {
        getScene()?.clearPendingDrops();
      },
      resetCamera: () => {
        getScene()?.resetCamera();
      },
    }),
    [getScene],
  );

  // 1. Inicialización y limpieza del juego Phaser
  useEffect(() => {
    const parent = containerRef.current;
    if (!parent) return;

    if (!isCanvasSupported()) {
      onError?.(new Error("Canvas2D no soportado en este entorno"));
      return;
    }

    let game: Phaser.Game | null = null;
    try {
      const config: Phaser.Types.Core.GameConfig = {
        type: Phaser.AUTO,
        parent,
        width: 512,
        height: 512,
        backgroundColor: "#2e1c0c",
        pixelArt: true,
        roundPixels: true,
        scene: [ValleyScene],
        audio: { noAudio: true },
      };

      game = new Phaser.Game(config);
      gameRef.current = game;

      // Esperar a que la escena esté lista para registrar callbacks
      game.events.once("ready", () => {
        const scene = game?.scene.getScene(ValleyScene.KEY) as ValleyScene | null;
        if (scene) {
          sceneRef.current = scene;
          scene.onSelectTile(onSelectTile);
          if (gameState) scene.updateState(gameState);
          if (selected) scene.setSelectedTile(selected);
        }
      });
    } catch (err) {
      // Entornos de prueba como happy-dom/node pueden carecer de WebGL/Canvas2D
      console.warn("No se pudo instanciar Phaser en el entorno actual:", err);
      onError?.(err);
    }

    return () => {
      sceneRef.current = null;
      if (game) {
        game.destroy(true);
        gameRef.current = null;
      }
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // 2. Sincronización de onSelectTile
  useEffect(() => {
    getScene()?.onSelectTile(onSelectTile);
  }, [getScene, onSelectTile]);

  // 3. Sincronización de gameState
  useEffect(() => {
    getScene()?.updateState(gameState);
  }, [getScene, gameState]);

  // 4. Sincronización de selección
  useEffect(() => {
    getScene()?.setSelectedTile(selected);
  }, [getScene, selected]);

  const handleResetCamera = () => {
    getScene()?.resetCamera();
  };

  return (
    <div className="valley-wrapper">
      <div
        id="valley"
        ref={containerRef}
        aria-label="Valle isométrico de 32 por 32 casillas"
      />
      <div className="valley-toolbar">
        <div className="valley-toolbar-left">
          <button
            type="button"
            onClick={handleResetCamera}
            className="btn-camera"
            title="Restablecer la cámara al centro del valle"
          >
            ⊙ Centrar vista
          </button>
          <span className="camera-hint">Arrastra para mover · Rueda para zoom</span>
        </div>
        {toolbarExtra ? <div className="valley-toolbar-right">{toolbarExtra}</div> : null}
      </div>
    </div>
  );
});

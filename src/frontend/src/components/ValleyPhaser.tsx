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
  const gameStateRef = useRef<StateFrame | null>(gameState);
  const selectedRef = useRef<{ x: number; y: number } | null>(selected);
  const onSelectTileRef = useRef(onSelectTile);
  const onErrorRef = useRef(onError);

  gameStateRef.current = gameState;
  selectedRef.current = selected;
  onSelectTileRef.current = onSelectTile;
  onErrorRef.current = onError;

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

  // 1. Inicialización y limpieza del juego Phaser (con recuperación ante pérdida de contexto WebGL)
  useEffect(() => {
    const parent = containerRef.current;
    if (!parent) return;

    if (!isCanvasSupported()) {
      onErrorRef.current?.(new Error("Canvas2D no soportado en este entorno"));
      return;
    }

    let isDisposed = false;
    let activeGame: Phaser.Game | null = null;
    let cleanupCanvasListeners: (() => void) | null = null;

    const destroyCurrentGame = () => {
      cleanupCanvasListeners?.();
      cleanupCanvasListeners = null;
      sceneRef.current = null;
      if (activeGame) {
        try {
          activeGame.destroy(true);
        } catch {
          // Ignorar errores al destruir un contexto ya perdido
        }
        activeGame = null;
        gameRef.current = null;
      }
    };

    const mountPhaser = (rendererType: number) => {
      if (isDisposed) return;
      destroyCurrentGame();

      try {
        const config: Phaser.Types.Core.GameConfig = {
          type: rendererType,
          parent,
          width: 512,
          height: 512,
          backgroundColor: "#2e1c0c",
          pixelArt: true,
          roundPixels: true,
          scene: [ValleyScene],
          audio: { noAudio: true },
        };

        const instance = new Phaser.Game(config);
        activeGame = instance;
        gameRef.current = instance;

        const canvas = instance.canvas;
        if (canvas && typeof canvas.addEventListener === "function") {
          const handleContextLost = (event: Event) => {
            event.preventDefault();
            if (isDisposed) return;
            console.warn(
              "Contexto WebGL perdido en ValleyPhaser; recreando en modo Canvas2D o activando vista de emergencia.",
            );
            if (rendererType !== Phaser.CANVAS) {
              mountPhaser(Phaser.CANVAS);
            } else {
              destroyCurrentGame();
              onErrorRef.current?.(new Error("Contexto gráfico perdido"));
            }
          };
          canvas.addEventListener("webglcontextlost", handleContextLost);
          canvas.addEventListener("contextlost", handleContextLost);
          cleanupCanvasListeners = () => {
            canvas.removeEventListener("webglcontextlost", handleContextLost);
            canvas.removeEventListener("contextlost", handleContextLost);
          };
        }

        // Esperar a que la escena esté lista para registrar callbacks y sincronizar estado
        instance.events.once("ready", () => {
          if (isDisposed || activeGame !== instance) return;
          const scene = instance.scene.getScene(ValleyScene.KEY) as ValleyScene | null;
          if (scene) {
            sceneRef.current = scene;
            scene.onSelectTile(onSelectTileRef.current);
            if (gameStateRef.current) scene.updateState(gameStateRef.current);
            if (selectedRef.current) scene.setSelectedTile(selectedRef.current);
          }
        });
      } catch (err) {
        // Entornos de prueba como happy-dom/node pueden carecer de WebGL/Canvas2D
        console.warn("No se pudo instanciar Phaser en el entorno actual:", err);
        onErrorRef.current?.(err);
      }
    };

    mountPhaser(Phaser.AUTO);

    return () => {
      isDisposed = true;
      destroyCurrentGame();
    };
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

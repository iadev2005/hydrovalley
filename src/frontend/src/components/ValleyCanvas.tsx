import { useEffect, useRef, type MouseEvent, type ReactNode } from "react";
import { GRID_SIZE, type StateFrame } from "../logic/play.ts";
import { buildTiles, tileAt } from "../logic/valley.ts";

export const CELL = 16;
export const STAGE_COLOURS = [
  "#8d6e4a",
  "#9ccc65",
  "#558b2f",
  "#f06292",
  "#1b5e20",
];
export const FIRE_COLOUR = "#e53935";
export const INSTALLATION_LETTER: Record<string, string> = {
  sprinkler: "A",
  drip: "G",
  treatment_plant: "P",
};

export interface ValleyCanvasProps {
  gameState: StateFrame | null;
  selected: { x: number; y: number } | null;
  onSelectTile: (coord: { x: number; y: number }) => void;
  toolbarExtra?: ReactNode;
}

export function ValleyCanvas({
  gameState,
  selected,
  onSelectTile,
  toolbarExtra,
}: ValleyCanvasProps) {
  const canvasRef = useRef<HTMLCanvasElement | null>(null);

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const ctx = canvas.getContext("2d");
    if (!ctx) return;

    ctx.clearRect(0, 0, canvas.width, canvas.height);
    ctx.font = `${CELL - 4}px sans-serif`;
    ctx.textAlign = "center";
    ctx.textBaseline = "middle";

    const tiles = buildTiles(gameState);
    for (const tile of tiles) {
      ctx.fillStyle = STAGE_COLOURS[tile.stage - 1] ?? STAGE_COLOURS[0] ?? "#8d6e4a";
      ctx.fillRect(tile.x * CELL, tile.y * CELL, CELL, CELL);

      if (tile.fire) {
        ctx.fillStyle = FIRE_COLOUR;
        ctx.fillRect(tile.x * CELL + 3, tile.y * CELL + 3, CELL - 6, CELL - 6);
      }

      if (tile.installation) {
        ctx.fillStyle = "#0d47a1";
        ctx.fillText(
          INSTALLATION_LETTER[tile.installation.kind] ?? "?",
          tile.x * CELL + CELL / 2,
          tile.y * CELL + CELL / 2 + 1,
        );
      }
    }

    ctx.strokeStyle = "rgba(0,0,0,0.12)";
    ctx.lineWidth = 1;
    for (let i = 0; i <= GRID_SIZE; i += 1) {
      ctx.beginPath();
      ctx.moveTo(i * CELL + 0.5, 0);
      ctx.lineTo(i * CELL + 0.5, GRID_SIZE * CELL);
      ctx.moveTo(0, i * CELL + 0.5);
      ctx.lineTo(GRID_SIZE * CELL, i * CELL + 0.5);
      ctx.stroke();
    }

    if (selected) {
      ctx.strokeStyle = "#ffd600";
      ctx.lineWidth = 2;
      ctx.strokeRect(
        selected.x * CELL + 1,
        selected.y * CELL + 1,
        CELL - 2,
        CELL - 2,
      );
    }
  }, [gameState, selected]);

  const handleClick = (event: MouseEvent<HTMLCanvasElement>) => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const rect = canvas.getBoundingClientRect();
    const scale = canvas.width / rect.width;
    const tile = tileAt(
      (event.clientX - rect.left) * scale,
      (event.clientY - rect.top) * scale,
      CELL,
      GRID_SIZE,
    );
    if (tile) {
      onSelectTile(tile);
    }
  };

  return (
    <div className="valley-wrapper">
      <canvas
        id="valley"
        ref={canvasRef}
        width={512}
        height={512}
        aria-label="Valle de 32 por 32 casillas"
        onClick={handleClick}
      />
      <div className="valley-toolbar">
        <div className="valley-toolbar-left">
          <span className="camera-hint">Cuadrícula 2D (vista cenital)</span>
        </div>
        {toolbarExtra ? <div className="valley-toolbar-right">{toolbarExtra}</div> : null}
      </div>
    </div>
  );
}

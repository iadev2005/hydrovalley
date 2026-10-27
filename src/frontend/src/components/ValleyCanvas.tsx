import {
  useEffect,
  useRef,
  useState,
  type MouseEvent,
  type KeyboardEvent,
  type PointerEvent,
  type ReactNode,
} from "react";
import { GRID_SIZE, type StateFrame } from "../logic/play.ts";
import { buildTiles, tileAt } from "../logic/valley.ts";

export const CELL = 16;
const MIN_ZOOM = 0.75;
const MAX_ZOOM = 2;
const ZOOM_STEP = 0.1;
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
  const wrapperRef = useRef<HTMLDivElement | null>(null);
  const [zoom, setZoom] = useState(1);
  const [offset, setOffset] = useState({ x: 0, y: 0 });
  const dragRef = useRef({ active: false, moved: false, x: 0, y: 0 });
  const suppressClickRef = useRef(false);

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
    if (suppressClickRef.current) {
      suppressClickRef.current = false;
      return;
    }
    const canvas = canvasRef.current;
    if (!canvas) return;
    const rect = canvas.getBoundingClientRect();
    const scaleX = canvas.width / rect.width;
    const scaleY = canvas.height / rect.height;
    const tile = tileAt(
      (event.clientX - rect.left) * scaleX,
      (event.clientY - rect.top) * scaleY,
      CELL,
      GRID_SIZE,
    );
    if (tile) {
      onSelectTile(tile);
    }
  };

  useEffect(() => {
    const wrapper = wrapperRef.current;
    if (!wrapper) return;

    const handleWheel = (event: globalThis.WheelEvent) => {
      if ((event.target as HTMLElement).closest(".valley-toolbar")) return;
      event.preventDefault();
      event.stopPropagation();
      setZoom((current) =>
        Math.min(
          MAX_ZOOM,
          Math.max(MIN_ZOOM, current + (event.deltaY > 0 ? -ZOOM_STEP : ZOOM_STEP)),
        ),
      );
    };

    wrapper.addEventListener("wheel", handleWheel, { passive: false });
    return () => wrapper.removeEventListener("wheel", handleWheel);
  }, []);

  const handlePointerDown = (event: PointerEvent<HTMLCanvasElement>) => {
    if (event.button !== 0) return;
    event.currentTarget.setPointerCapture(event.pointerId);
    dragRef.current = {
      active: true,
      moved: false,
      x: event.clientX,
      y: event.clientY,
    };
  };

  const handlePointerMove = (event: PointerEvent<HTMLCanvasElement>) => {
    const drag = dragRef.current;
    if (!drag.active) return;
    const dx = event.clientX - drag.x;
    const dy = event.clientY - drag.y;
    if (Math.abs(dx) < 1 && Math.abs(dy) < 1) return;
    drag.moved = true;
    drag.x = event.clientX;
    drag.y = event.clientY;
    setOffset((current) => ({ x: current.x + dx, y: current.y + dy }));
  };

  const handlePointerUp = (event: PointerEvent<HTMLCanvasElement>) => {
    if (!dragRef.current.active) return;
    suppressClickRef.current = dragRef.current.moved;
    dragRef.current.active = false;
    event.currentTarget.releasePointerCapture(event.pointerId);
  };

  const handleKeyDown = (event: KeyboardEvent<HTMLCanvasElement>) => {
    if (event.key !== "ArrowUp" && event.key !== "ArrowDown" && event.key !== "ArrowLeft" && event.key !== "ArrowRight") {
      return;
    }
    event.preventDefault();
    const current = selected ?? { x: 0, y: 0 };
    const next = {
      x: Math.min(GRID_SIZE - 1, Math.max(0, current.x + (event.key === "ArrowLeft" ? -1 : event.key === "ArrowRight" ? 1 : 0))),
      y: Math.min(GRID_SIZE - 1, Math.max(0, current.y + (event.key === "ArrowUp" ? -1 : event.key === "ArrowDown" ? 1 : 0))),
    };
    onSelectTile(next);
  };

  return (
    <div ref={wrapperRef} className="valley-wrapper">
      <p id="zenithal-help" className="visually-hidden">
        Usa las flechas del teclado para seleccionar una casilla. Usa la rueda o arrastra el mapa para cambiar la vista.
      </p>
      <canvas
        id="valley"
        ref={canvasRef}
        className="zenithal-canvas"
        style={{ transform: `translate(${offset.x}px, ${offset.y}px) scale(${zoom})` }}
        width={512}
        height={512}
        aria-label="Valle de 32 por 32 casillas"
        aria-describedby="zenithal-help"
        tabIndex={0}
        onClick={handleClick}
        onKeyDown={handleKeyDown}
        onPointerDown={handlePointerDown}
        onPointerMove={handlePointerMove}
        onPointerUp={handlePointerUp}
        onPointerCancel={handlePointerUp}
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

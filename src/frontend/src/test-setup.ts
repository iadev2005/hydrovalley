import { afterEach } from "vitest";
import { cleanup } from "@testing-library/react";

// Mock básico de CanvasRenderingContext2D para happy-dom en pruebas unitarias
if (typeof HTMLCanvasElement !== "undefined") {
  const dummyContext = {
    fillRect: () => {},
    clearRect: () => {},
    getImageData: () => ({ data: new Uint8ClampedArray(4) }),
    putImageData: () => {},
    createImageData: () => [],
    setTransform: () => {},
    drawImage: () => {},
    save: () => {},
    fillText: () => {},
    restore: () => {},
    beginPath: () => {},
    moveTo: () => {},
    lineTo: () => {},
    closePath: () => {},
    stroke: () => {},
    translate: () => {},
    scale: () => {},
    rotate: () => {},
    arc: () => {},
    fill: () => {},
    measureText: () => ({ width: 0 }),
    transform: () => {},
    rect: () => {},
    clip: () => {},
    globalCompositeOperation: "source-over",
    canvas: {} as HTMLCanvasElement,
  };

  HTMLCanvasElement.prototype.getContext = (() =>
    dummyContext) as unknown as typeof HTMLCanvasElement.prototype.getContext;
}

afterEach(() => {
  cleanup();
});

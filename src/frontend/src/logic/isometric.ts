// Funciones puras de proyección isométrica 2:1 para el valle de Hydrovalley (RF-03, RF-04).
// Sin dependencias de DOM ni de Phaser para permitir pruebas unitarias en Vitest.

export const ISO_TILE_WIDTH = 32;
export const ISO_TILE_HEIGHT = 16;
export const VALLEY_GRID_SIZE = 32;

export interface IsometricPoint {
  x: number;
  y: number;
}

export interface GridCoord {
  x: number;
  y: number;
}

export interface IsometricDiamondBounds {
  top: IsometricPoint;
  right: IsometricPoint;
  bottom: IsometricPoint;
  left: IsometricPoint;
  center: IsometricPoint;
}

/**
 * Convierte una coordenada de la cuadrícula (x, y) a la posición en pantalla
 * del centro del rombo isométrico.
 *
 * @param gridX Coordenada X en la cuadrícula [0, gridSize - 1].
 * @param gridY Coordenada Y en la cuadrícula [0, gridSize - 1].
 * @param originX Posición X en pantalla del centro de la casilla (0, 0).
 * @param originY Posición Y en pantalla del centro de la casilla (0, 0).
 * @param tileWidth Ancho del rombo en píxeles (por defecto 32).
 * @param tileHeight Alto del rombo en píxeles (por defecto 16).
 */
export function gridToIsometric(
  gridX: number,
  gridY: number,
  originX = 0,
  originY = 0,
  tileWidth = ISO_TILE_WIDTH,
  tileHeight = ISO_TILE_HEIGHT,
): IsometricPoint {
  const halfW = tileWidth / 2;
  const halfH = tileHeight / 2;
  return {
    x: (gridX - gridY) * halfW + originX,
    y: (gridX + gridY) * halfH + originY,
  };
}

/**
 * Convierte un punto en pantalla (screenX, screenY) a la coordenada entera de la
 * casilla (x, y) de la cuadrícula bajo ese punto.
 *
 * Devuelve `null` si el punto cae fuera del valle o si las entradas son inválidas.
 *
 * @param screenX Coordenada X en pantalla (mundo o puntero del ratón).
 * @param screenY Coordenada Y en pantalla (mundo o puntero del ratón).
 * @param originX Posición X en pantalla del centro de la casilla (0, 0).
 * @param originY Posición Y en pantalla del centro de la casilla (0, 0).
 * @param gridSize Número de casillas por lado (por defecto 32).
 * @param tileWidth Ancho del rombo en píxeles (por defecto 32).
 * @param tileHeight Alto del rombo en píxeles (por defecto 16).
 */
export function isometricToGrid(
  screenX: number,
  screenY: number,
  originX = 0,
  originY = 0,
  gridSize = VALLEY_GRID_SIZE,
  tileWidth = ISO_TILE_WIDTH,
  tileHeight = ISO_TILE_HEIGHT,
): GridCoord | null {
  if (
    !Number.isFinite(screenX) ||
    !Number.isFinite(screenY) ||
    !Number.isFinite(originX) ||
    !Number.isFinite(originY)
  ) {
    return null;
  }

  const dx = screenX - originX;
  const dy = screenY - originY;

  // En la proyección dimétrica 2:1:
  // dx / (W / 2) = x - y
  // dy / (H / 2) = x + y
  // Sumando ambas ecuaciones: x = dx / W + dy / H
  // Restando ambas ecuaciones: y = dy / H - dx / W
  const rawX = dx / tileWidth + dy / tileHeight;
  const rawY = dy / tileHeight - dx / tileWidth;

  // Centro de cada rombo es entero; los bordes son +/- 0.5
  const tileX = Math.floor(rawX + 0.5);
  const tileY = Math.floor(rawY + 0.5);

  if (tileX < 0 || tileX >= gridSize || tileY < 0 || tileY >= gridSize) {
    return null;
  }

  return { x: tileX, y: tileY };
}

/**
 * Calcula la profundidad visual (depth / z-order) de una casilla para asegurar
 * que las casillas y elementos frontales (mayor x + y) se dibujen sobre los traseros.
 */
export function getIsometricDepth(
  gridX: number,
  gridY: number,
  gridSize = VALLEY_GRID_SIZE,
): number {
  return gridY * gridSize + gridX;
}

/**
 * Devuelve los 4 vértices y el centro del rombo isométrico de una casilla en pantalla.
 * Útil para dibujar el cursor de selección, el contorno o depuración geométrica.
 */
export function getIsometricTileBounds(
  gridX: number,
  gridY: number,
  originX = 0,
  originY = 0,
  tileWidth = ISO_TILE_WIDTH,
  tileHeight = ISO_TILE_HEIGHT,
): IsometricDiamondBounds {
  const center = gridToIsometric(
    gridX,
    gridY,
    originX,
    originY,
    tileWidth,
    tileHeight,
  );
  const halfW = tileWidth / 2;
  const halfH = tileHeight / 2;

  return {
    center,
    top: { x: center.x, y: center.y - halfH },
    right: { x: center.x + halfW, y: center.y },
    bottom: { x: center.x, y: center.y + halfH },
    left: { x: center.x - halfW, y: center.y },
  };
}

/**
 * Dimensiones y límites de la cuadrícula completa de 32x32 en pantalla.
 */
export function getValleyIsometricDimensions(
  gridSize = VALLEY_GRID_SIZE,
  tileWidth = ISO_TILE_WIDTH,
  tileHeight = ISO_TILE_HEIGHT,
): { totalWidth: number; totalHeight: number } {
  return {
    totalWidth: gridSize * tileWidth,
    totalHeight: gridSize * tileHeight,
  };
}

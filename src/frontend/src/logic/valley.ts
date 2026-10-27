// Lógica de la vista del valle sin DOM, para probarla con vitest: del
// estado del hub al modelo que se dibuja, mensajes de resultado, rol del
// token y acciones pendientes. main.js o componentes solo conectan esto con la UI.

import type { StateFrame } from "./play.ts";

const STAGE_NAMES = [
  "tierra seca",
  "pasto",
  "arbusto",
  "flores",
  "árbol",
  "agua",
  "roca",
  "suelo dañado",
] as const;

/** Nombre de la etapa (1 tierra seca … 5 árbol, 6 agua, 7 roca, 8 suelo dañado); lo desconocido cuenta como tierra seca. */
export function stageName(stage: number): string {
  const name = STAGE_NAMES[stage - 1];
  return name ?? STAGE_NAMES[0];
}

/**
 * Rol del token firmado (`{player_id, username, role, exp}`). Solo sirve para
 * mostrar u ocultar los controles de administración: el hub es quien decide
 * (responde `forbidden` sin rol de administrador). Vacío si no se puede leer.
 */
export function tokenRole(token: unknown): string {
  if (typeof token !== "string") return "";
  try {
    const parts = token.split(".");
    if (parts.length < 2 || !parts[1]) return "";
    const payload = parts[1].replace(/-/g, "+").replace(/_/g, "/");
    const bytes = Uint8Array.from(atob(payload), (c) => c.charCodeAt(0));
    const claims = JSON.parse(new TextDecoder().decode(bytes)) as Record<string, unknown>;
    const role = claims["role"];
    return typeof role === "string" ? role : "";
  } catch {
    return "";
  }
}

export function isAdminRole(role: string): boolean {
  return role === "admin";
}

export interface TileFire {
  drops_received: number;
}

export interface TileInstallation {
  kind: string;
  owner_id: string;
  remaining_durability?: number;
}

export interface Tile {
  x: number;
  y: number;
  stage: number;
  humidity: bigint;
  fire: TileFire | null;
  installation: TileInstallation | null;
  has_building?: boolean;
  trash_level?: number;
}

export type StateForTiles = Pick<
  StateFrame,
  "width" | "height" | "stages" | "humidity" | "fires" | "installations"
> & {
  buildings?: boolean[];
  trash?: number[];
};

/**
 * Una entrada por casilla, por filas (índice = y * width + x), con su etapa,
 * humedad, fuego e instalación. Vacío si el estado no trae el mapa.
 */
export function buildTiles(state: StateForTiles | null): Tile[] {
  if (!state || state.stages.length === 0) return [];
  const { width, height } = state;
  const tiles: Tile[] = state.stages.map((stage, i) => ({
    x: i % width,
    y: Math.floor(i / width),
    stage,
    humidity: state.humidity[i] ?? 0n,
    fire: null,
    installation: null,
    has_building: state.buildings ? state.buildings[i] === true : false,
    trash_level: state.trash ? state.trash[i] ?? 0 : 0,
  }));
  const at = (x: number, y: number): Tile | undefined =>
    x < width && y < height ? tiles[y * width + x] : undefined;
  for (const f of state.fires) {
    const tile = at(f.x, f.y);
    if (tile) tile.fire = { drops_received: f.drops_received };
  }
  for (const i of state.installations) {
    const tile = at(i.x, i.y);
    if (tile) {
      tile.installation = {
        kind: i.installation,
        owner_id: i.owner_id,
        remaining_durability: i.remaining_durability,
      };
    }
  }
  return tiles;
}

/**
 * Casilla bajo un punto del lienzo, o `null` si cae fuera del valle.
 */
export function tileAt(
  px: number,
  py: number,
  cellSize: number,
  size: number,
): { x: number; y: number } | null {
  if (!Number.isFinite(px) || !Number.isFinite(py) || px < 0 || py < 0) return null;
  const x = Math.floor(px / cellSize);
  const y = Math.floor(py / cellSize);
  return x < size && y < size ? { x, y } : null;
}

const DASH = "—";

export interface PlayerPanelPrices {
  sprinkler: string;
  drip: string;
  treatment_plant: string;
  trash_bin: string;
}

export interface PlayerPanelOwned {
  bigger_drop: boolean;
  bigger_tank: boolean;
}

export interface PlayerPanelView {
  water: string;
  waterRatio: number;
  leaves: string;
  prices: PlayerPanelPrices;
  owned: PlayerPanelOwned;
}

export interface PlayerDataForPanel {
  water: bigint;
  tank_capacity: bigint;
  leaves: bigint;
  upgrades: string[];
  next_sprinkler_price?: bigint;
  next_drip_price?: bigint;
  next_treatment_plant_price?: bigint;
  next_trash_bin_price?: bigint;
}

/**
 * Textos del panel del jugador. Antes de que el jugador entre en el mundo
 * (su primer comando) el estado no lo trae y se muestran guiones.
 */
export function playerPanel(player: PlayerDataForPanel | null): PlayerPanelView {
  const owned = (name: string): boolean => player?.upgrades.includes(name) ?? false;
  const price = (value?: bigint): string => (player ? String(value ?? 0n) : DASH);
  return {
    water: player ? `${player.water} / ${player.tank_capacity}` : DASH,
    waterRatio:
      player && player.tank_capacity > 0n
        ? Number((player.water * 1000n) / player.tank_capacity) / 1000
        : 0,
    leaves: player ? String(player.leaves) : DASH,
    prices: {
      sprinkler: price(player?.next_sprinkler_price),
      drip: price(player?.next_drip_price),
      treatment_plant: price(player?.next_treatment_plant_price),
      trash_bin: price(player?.next_trash_bin_price),
    },
    owned: { bigger_drop: owned("bigger_drop"), bigger_tank: owned("bigger_tank") },
  };
}

const REJECT_TEXT: Record<string, string> = {
  insufficient_water: "no tienes agua suficiente",
  insufficient_leaves: "no tienes hojas suficientes",
  out_of_bounds: "la casilla está fuera del valle",
  tile_occupied: "la casilla está ocupada (instalación, agua, roca, edificio o basura)",
  upgrade_already_owned: "ya tienes esa mejora",
  invalid_command: "el comando no es válido (¿la casilla no tiene basura?)",
};

/**
 * Única tabla de resultados finales de play.proto: cada clave es un estado
 * que cierra la acción. `duplicate` cuenta como aplicada (CLAUDE.md, regla 2).
 * `rejected` se resuelve aparte porque su texto depende del motivo.
 */
const STATUS_TABLE: Record<string, { ok: boolean; text: string }> = {
  applied: { ok: true, text: "aplicada" },
  duplicate: { ok: true, text: "aplicada" },
  rejected: { ok: false, text: "rechazada" },
  expired: { ok: false, text: "caducó en la cola (más de 30 s sin aplicarse)" },
  rate_limited: { ok: false, text: "demasiadas acciones por segundo; espera un momento" },
  paused: { ok: false, text: "el valle está en pausa; inténtalo más tarde" },
  forbidden: { ok: false, text: "solo un administrador puede hacerlo" },
};

/**
 * Mensaje para el jugador a partir del resultado de una acción.
 */
export function describeActionResult({
  status,
  reason = "",
}: {
  status: string;
  reason?: string;
}): { ok: boolean; text: string } {
  if (status === "rejected") {
    return { ok: false, text: `rechazada: ${REJECT_TEXT[reason] ?? (reason || "sin motivo")}` };
  }
  const known = STATUS_TABLE[status];
  return known ? { ...known } : { ok: false, text: `resultado desconocido (${status})` };
}

/**
 * Descripción de la casilla elegida para el panel; sin selección devuelve el
 * texto de reposo (así «Salir» limpia la etiqueta). El mapa se indexa con el
 * ancho del propio estado. El hub no difunde cuántas gotas apagan un fuego
 * (`FireView` solo trae `drops_received`), así que solo se muestran las
 * recibidas, sin un total fijado en la página.
 */
export function selectionView(
  state: StateForTiles | null,
  selected: { x: number; y: number } | null,
): { label: string; info: string } {
  if (!selected) return { label: "ninguna", info: "Haz clic en una casilla." };
  const label = `(${selected.x}, ${selected.y})`;
  if (!state || state.stages.length === 0) {
    return { label, info: "Sin datos del mapa todavía." };
  }
  const { width, height } = state;
  if (selected.x < 0 || selected.y < 0 || selected.x >= width || selected.y >= height) {
    return { label, info: "Sin datos del mapa todavía." };
  }
  const idx = selected.y * width + selected.x;
  const stage = state.stages[idx];
  if (stage === undefined) {
    return { label, info: "Sin datos del mapa todavía." };
  }
  const humidity = state.humidity[idx] ?? 0n;
  let info = `${stageName(stage)}, humedad ${humidity}`;
  if (state.buildings?.[idx]) info += ", estructura urbana";
  const trash = state.trash?.[idx] ?? 0;
  if (trash > 0) info += `, basura nivel ${trash}`;
  const fire = state.fires.find((f) => f.x === selected.x && f.y === selected.y);
  if (fire) info += `, en llamas (${fire.drops_received} gotas recibidas)`;
  const inst = state.installations.find((i) => i.x === selected.x && i.y === selected.y);
  if (inst) info += `, instalación: ${inst.installation}`;
  return { label, info };
}

/**
 * Plazo de una acción pendiente: el mismo que la cola del hub, que responde
 * `expired` a lo que lleva más de 30 s admitido (CLAUDE.md, regla 2). Pasado
 * ese tiempo reenviarla ya no tiene sentido.
 */
export const PENDING_MAX_AGE_MS = 30_000;

/** Comandos de administración (RF-31): nunca se reenvían solos al reconectar. */
const ADMIN_COMMAND_TYPES = new Set(["reset_valley", "set_demo_mode"]);

export interface PendingActionEntry {
  actionId: string;
  label: string;
  frame: string;
  type: string;
  addedAt: number;
}

export interface ResendPlan {
  resend: PendingActionEntry[];
  expired: PendingActionEntry[];
  adminDropped: PendingActionEntry[];
}

/**
 * Qué hacer con las acciones pendientes al reabrir el WebSocket (regla 2 de
 * CLAUDE.md): reenviar las recientes de jugador, en orden de envío; descartar
 * las que superan el plazo (`expired`) y los comandos de administración
 * (`adminDropped`), que el administrador debe repetir a propósito.
 */
export function planResend(entries: PendingActionEntry[], now: number): ResendPlan {
  const plan: ResendPlan = { resend: [], expired: [], adminDropped: [] };
  for (const entry of entries) {
    if (now - entry.addedAt > PENDING_MAX_AGE_MS) plan.expired.push(entry);
    else if (ADMIN_COMMAND_TYPES.has(entry.type)) plan.adminDropped.push(entry);
    else plan.resend.push(entry);
  }
  return plan;
}

const FINAL_STATUSES = new Set(Object.keys(STATUS_TABLE));

export interface PendingActionsManager {
  readonly size: number;
  has(actionId: string): boolean;
  add(actionId: string, frame: string, label: string): void;
  resolve(result: { action_id: string; status: string }): PendingActionEntry | null;
  settle(actionId: string): PendingActionEntry | null;
  planReconnect(): ResendPlan;
}

/**
 * Acciones enviadas que aún no tienen resultado final. Se guardan con su
 * mensaje para reenviarlas al reconectarse (regla 2 de CLAUDE.md): el hub
 * ignora un `action_id` que sigue en cola y responde `duplicate` a uno ya
 * aplicado, así que reenviar es seguro. Un resultado final, de cualquier
 * clase (incluido `duplicate`), la quita.
 */
export function createPendingActions({
  now = Date.now,
}: {
  now?: () => number;
} = {}): PendingActionsManager {
  const entries = new Map<string, PendingActionEntry>();
  return {
    get size(): number {
      return entries.size;
    },
    has: (actionId: string): boolean => entries.has(actionId),
    add(actionId: string, frame: string, label: string): void {
      let type = "";
      try {
        const parsed = JSON.parse(frame) as Record<string, unknown>;
        type = typeof parsed["type"] === "string" ? parsed["type"] : "";
      } catch {
        // un mensaje ilegible se trata como comando de jugador
      }
      entries.set(actionId, { actionId, label, frame, type, addedAt: now() });
    },
    resolve({ action_id: actionId, status }: { action_id: string; status: string }): PendingActionEntry | null {
      return FINAL_STATUSES.has(status) ? this.settle(actionId) : null;
    },
    settle(actionId: string): PendingActionEntry | null {
      const entry = entries.get(actionId);
      if (!entry) return null;
      entries.delete(actionId);
      return entry;
    },
    planReconnect(): ResendPlan {
      const plan = planResend(Array.from(entries.values()), now());
      for (const entry of [...plan.expired, ...plan.adminDropped]) {
        entries.delete(entry.actionId);
      }
      return plan;
    },
  };
}

/**
 * Determina si una casilla cae dentro del área 5x5 de al menos un riego por goteo.
 */
export function isTileInDripArea(
  tileX: number,
  tileY: number,
  dripPositions: Array<{ x: number; y: number }>,
): boolean {
  return dripPositions.some(
    (d) => Math.abs(d.x - tileX) <= 2 && Math.abs(d.y - tileY) <= 2,
  );
}

// Lógica de la vista del valle sin DOM, para probarla con node:test: del
// estado del hub al modelo que se dibuja, mensajes de resultado, rol del
// token y acciones pendientes. main.js solo conecta esto con el DOM.

const STAGE_NAMES = ["tierra seca", "pasto", "arbusto", "flores", "árbol"];

/** Nombre de la etapa (1 tierra seca … 5 árbol); lo desconocido cuenta como tierra seca. */
export function stageName(stage) {
  return STAGE_NAMES[stage - 1] ?? STAGE_NAMES[0];
}

/**
 * Rol del token firmado (`{player_id, username, role, exp}`). Solo sirve para
 * mostrar u ocultar los controles de administración: el hub es quien decide
 * (responde `forbidden` sin rol de administrador). Vacío si no se puede leer.
 * @param {unknown} token
 */
export function tokenRole(token) {
  if (typeof token !== "string") return "";
  try {
    const payload = token.split(".")[1]?.replace(/-/g, "+").replace(/_/g, "/");
    if (!payload) return "";
    const bytes = Uint8Array.from(atob(payload), (c) => c.charCodeAt(0));
    const role = JSON.parse(new TextDecoder().decode(bytes))?.role;
    return typeof role === "string" ? role : "";
  } catch {
    return "";
  }
}

/** @param {string} role */
export function isAdminRole(role) {
  return role === "admin";
}

/**
 * Una entrada por casilla, por filas (índice = y * width + x), con su etapa,
 * humedad, fuego e instalación. Vacío si el estado no trae el mapa.
 * @param {{width: number, height: number, stages: number[], humidity: bigint[], fires: any[], installations: any[]} | null} state
 */
export function buildTiles(state) {
  if (!state || state.stages.length === 0) return [];
  const { width, height } = state;
  const tiles = state.stages.map((stage, i) => ({
    x: i % width,
    y: Math.floor(i / width),
    stage,
    humidity: state.humidity[i] ?? 0n,
    fire: null,
    installation: null,
  }));
  const at = (x, y) => (x < width && y < height ? tiles[y * width + x] : undefined);
  for (const f of state.fires) {
    const tile = at(f.x, f.y);
    if (tile) tile.fire = { drops_received: f.drops_received };
  }
  for (const i of state.installations) {
    const tile = at(i.x, i.y);
    if (tile) tile.installation = { kind: i.installation, owner_id: i.owner_id };
  }
  return tiles;
}

/**
 * Casilla bajo un punto del lienzo, o `null` si cae fuera del valle.
 * @param {number} px
 * @param {number} py
 * @param {number} cellSize
 * @param {number} size casillas por lado
 */
export function tileAt(px, py, cellSize, size) {
  if (!Number.isFinite(px) || !Number.isFinite(py) || px < 0 || py < 0) return null;
  const x = Math.floor(px / cellSize);
  const y = Math.floor(py / cellSize);
  return x < size && y < size ? { x, y } : null;
}

const DASH = "—";

/**
 * Textos del panel del jugador. Antes de que el jugador entre en el mundo
 * (su primer comando) el estado no lo trae y se muestran guiones.
 * @param {{water: bigint, tank_capacity: bigint, leaves: bigint, upgrades: string[], next_sprinkler_price?: bigint, next_drip_price?: bigint, next_treatment_plant_price?: bigint} | null} player
 */
export function playerPanel(player) {
  const owned = (name) => player?.upgrades?.includes(name) ?? false;
  const price = (value) => (player ? String(value ?? 0n) : DASH);
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
    },
    owned: { bigger_drop: owned("bigger_drop"), bigger_tank: owned("bigger_tank") },
  };
}

const REJECT_TEXT = {
  insufficient_water: "no tienes agua suficiente",
  insufficient_leaves: "no tienes hojas suficientes",
  out_of_bounds: "la casilla está fuera del valle",
  tile_occupied: "la casilla ya está ocupada por una instalación",
  upgrade_already_owned: "ya tienes esa mejora",
  invalid_command: "el comando no es válido",
};

/**
 * Única tabla de resultados finales de play.proto: cada clave es un estado
 * que cierra la acción. `duplicate` cuenta como aplicada (CLAUDE.md, regla 2).
 * `rejected` se resuelve aparte porque su texto depende del motivo.
 */
const STATUS_TABLE = {
  applied: { ok: true, text: "aplicada" },
  duplicate: { ok: true, text: "aplicada" },
  rejected: { ok: false, text: "rechazada" },
  expired: { ok: false, text: "caducó en la cola (más de 30 s sin aplicarse)" },
  rate_limited: { ok: false, text: "demasiadas gotas por segundo; espera un momento" },
  paused: { ok: false, text: "el valle está en pausa; inténtalo más tarde" },
  forbidden: { ok: false, text: "solo un administrador puede hacerlo" },
};

/**
 * Mensaje para el jugador a partir del resultado de una acción.
 * @param {{status: string, reason?: string}} result
 * @returns {{ok: boolean, text: string}}
 */
export function describeActionResult({ status, reason = "" }) {
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
 * @param {Parameters<typeof buildTiles>[0]} state
 * @param {{x: number, y: number} | null} selected
 */
export function selectionView(state, selected) {
  if (!selected) return { label: "ninguna", info: "Haz clic en una casilla." };
  const label = `(${selected.x}, ${selected.y})`;
  const tile = buildTiles(state)[selected.y * (state?.width ?? 0) + selected.x];
  if (!tile) return { label, info: "Sin datos del mapa todavía." };
  let info = `${stageName(tile.stage)}, humedad ${tile.humidity}`;
  if (tile.fire) info += `, en llamas (${tile.fire.drops_received} gotas recibidas)`;
  if (tile.installation) info += `, instalación: ${tile.installation.kind}`;
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

/**
 * Qué hacer con las acciones pendientes al reabrir el WebSocket (regla 2 de
 * CLAUDE.md): reenviar las recientes de jugador, en orden de envío; descartar
 * las que superan el plazo (`expired`) y los comandos de administración
 * (`adminDropped`), que el administrador debe repetir a propósito.
 * @param {{actionId: string, frame: string, type: string, addedAt: number}[]} entries
 * @param {number} now
 */
export function planResend(entries, now) {
  const plan = { resend: [], expired: [], adminDropped: [] };
  for (const entry of entries) {
    if (now - entry.addedAt > PENDING_MAX_AGE_MS) plan.expired.push(entry);
    else if (ADMIN_COMMAND_TYPES.has(entry.type)) plan.adminDropped.push(entry);
    else plan.resend.push(entry);
  }
  return plan;
}

const FINAL_STATUSES = new Set(Object.keys(STATUS_TABLE));

/**
 * Acciones enviadas que aún no tienen resultado final. Se guardan con su
 * mensaje para reenviarlas al reconectarse (regla 2 de CLAUDE.md): el hub
 * ignora un `action_id` que sigue en cola y responde `duplicate` a uno ya
 * aplicado, así que reenviar es seguro. Un resultado final, de cualquier
 * clase (incluido `duplicate`), la quita.
 * @param {{now?: () => number}} [deps]
 */
export function createPendingActions({ now = Date.now } = {}) {
  /** @type {Map<string, {actionId: string, label: string, frame: string, type: string, addedAt: number}>} */
  const entries = new Map();
  return {
    get size() {
      return entries.size;
    },
    has: (actionId) => entries.has(actionId),
    add(actionId, frame, label) {
      let type = "";
      try {
        type = JSON.parse(frame)?.type ?? "";
      } catch {
        // un mensaje ilegible se trata como comando de jugador
      }
      entries.set(actionId, { actionId, label, frame, type, addedAt: now() });
    },
    /** Quita la acción si el resultado es final; devuelve su entrada o `null`. */
    resolve({ action_id: actionId, status }) {
      return FINAL_STATUSES.has(status) ? this.settle(actionId) : null;
    },
    /** Quita la acción (resultado final, `error` del hub o descarte); devuelve su entrada o `null`. */
    settle(actionId) {
      const entry = entries.get(actionId);
      if (!entry) return null;
      entries.delete(actionId);
      return entry;
    },
    /**
     * Plan para reabrir el WebSocket: quita las caducadas y las de
     * administración; las que se reenvían siguen pendientes.
     */
    planReconnect() {
      const plan = planResend(Array.from(entries.values()), now());
      for (const entry of [...plan.expired, ...plan.adminDropped]) entries.delete(entry.actionId);
      return plan;
    },
  };
}

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

const STATUS_TEXT = {
  expired: "caducó en la cola (más de 30 s sin aplicarse)",
  rate_limited: "demasiadas gotas por segundo; espera un momento",
  paused: "el valle está en pausa; inténtalo más tarde",
  forbidden: "solo un administrador puede hacerlo",
};

/**
 * Mensaje para el jugador a partir del resultado de una acción. `duplicate`
 * cuenta como aplicada (CLAUDE.md, regla 2).
 * @param {{status: string, reason?: string}} result
 * @returns {{ok: boolean, text: string}}
 */
export function describeActionResult({ status, reason = "" }) {
  if (status === "applied" || status === "duplicate") return { ok: true, text: "aplicada" };
  if (status === "rejected") {
    return { ok: false, text: `rechazada: ${REJECT_TEXT[reason] ?? (reason || "sin motivo")}` };
  }
  if (status in STATUS_TEXT) return { ok: false, text: STATUS_TEXT[status] };
  return { ok: false, text: `resultado desconocido (${status})` };
}

const FINAL_STATUSES = new Set([
  "applied",
  "duplicate",
  "rejected",
  "expired",
  "rate_limited",
  "paused",
  "forbidden",
]);

/**
 * Acciones enviadas que aún no tienen resultado final. Se guardan con su
 * mensaje para reenviarlas al reconectarse (regla 2 de CLAUDE.md): el hub
 * ignora un `action_id` que sigue en cola y responde `duplicate` a uno ya
 * aplicado, así que reenviar es seguro. Un resultado final, de cualquier
 * clase (incluido `duplicate`), la quita.
 */
export function createPendingActions() {
  /** @type {Map<string, {label: string, frame: string}>} */
  const entries = new Map();
  return {
    get size() {
      return entries.size;
    },
    has: (actionId) => entries.has(actionId),
    add(actionId, frame, label) {
      entries.set(actionId, { label, frame });
    },
    /** Quita la acción si el resultado es final; devuelve su entrada o `null`. */
    resolve({ action_id: actionId, status }) {
      if (!FINAL_STATUSES.has(status)) return null;
      return this.fail(actionId);
    },
    /** Quita la acción que el hub contestó con un mensaje `error`. */
    fail(actionId) {
      const entry = entries.get(actionId);
      if (!entry) return null;
      entries.delete(actionId);
      return entry;
    },
    framesToResend: () => Array.from(entries.values(), (e) => e.frame),
  };
}

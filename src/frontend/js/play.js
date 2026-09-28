// Mensajes del WebSocket /play (proto/hydrovalley/play/v1/play.proto) y
// utilidades sin DOM, para poder probarlas con node:test.

const PLAYER_ID_KEY = "hydrovalley.player_id";
const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

/**
 * UUID v4 a partir de bytes aleatorios. Usa `crypto.getRandomValues`, que
 * también existe fuera de un contexto seguro (http://10.68.17.x:8080), a
 * diferencia de `crypto.randomUUID`.
 * @param {(bytes: Uint8Array) => Uint8Array} [fill]
 * @returns {string}
 */
export function newUuid(fill = (bytes) => globalThis.crypto.getRandomValues(bytes)) {
  const b = fill(new Uint8Array(16));
  b[6] = (b[6] & 0x0f) | 0x40; // versión 4
  b[8] = (b[8] & 0x3f) | 0x80; // variante RFC 9562
  const hex = Array.from(b, (v) => v.toString(16).padStart(2, "0")).join("");
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}

/** @param {unknown} value */
export function isUuid(value) {
  return typeof value === "string" && UUID_RE.test(value);
}

/**
 * Identidad provisional del jugador: un UUID guardado en `localStorage`.
 * TODO(PR de acceso): se sustituye por el token firmado de la API.
 * @param {{getItem(k: string): string | null, setItem(k: string, v: string): void}} storage
 * @param {() => string} [create]
 */
export function loadOrCreatePlayerId(storage, create = newUuid) {
  try {
    const stored = storage.getItem(PLAYER_ID_KEY);
    if (isUuid(stored)) return stored;
  } catch {
    // Almacenamiento bloqueado: se usa una identidad nueva en cada visita.
  }
  const id = create();
  try {
    storage.setItem(PLAYER_ID_KEY, id);
  } catch {
    // Igual que arriba: no se puede recordar.
  }
  return id;
}

/**
 * Dirección del WebSocket en el mismo origen que la página (nginx la
 * reenvía al hub). Sin mapa: esta página no dibuja el valle.
 * @param {{protocol: string, host: string}} location
 * @param {string} playerId
 */
export function buildPlayUrl(location, playerId) {
  const scheme = location.protocol === "https:" ? "wss:" : "ws:";
  const query = new URLSearchParams({ player_id: playerId, map: "false" });
  return `${scheme}//${location.host}/play?${query.toString()}`;
}

/**
 * `DropMessage` en JSON. Las coordenadas son enteros no negativos; el
 * servidor decide si caen dentro del valle.
 * @param {{x: number, y: number, actionId: string}} drop
 */
export function buildDropFrame({ x, y, actionId }) {
  for (const [name, v] of [
    ["x", x],
    ["y", y],
  ]) {
    if (!Number.isInteger(v) || v < 0) {
      throw new RangeError(`${name} must be a non-negative integer, got ${v}`);
    }
  }
  return JSON.stringify({ type: "drop", action_id: actionId, x, y });
}

/** uint64 de protojson: texto (o número si es pequeño); ausente → 0. */
function toBigInt(value) {
  if (typeof value === "string" || typeof value === "number") {
    try {
      return BigInt(value);
    } catch {
      return 0n;
    }
  }
  return 0n;
}

const str = (value) => (typeof value === "string" ? value : "");

/**
 * Decodifica un mensaje del hub. Los campos ausentes toman su valor cero
 * (convención de protojson) y los uint64 se leen como BigInt (regla 7).
 * @param {string} text
 */
export function parseFrame(text) {
  let msg;
  try {
    msg = JSON.parse(text);
  } catch {
    return { type: "unknown", raw: text };
  }
  if (msg === null || typeof msg !== "object" || Array.isArray(msg)) {
    return { type: "unknown", raw: text };
  }
  switch (msg.type) {
    case "result":
      return {
        type: "result",
        action_id: str(msg.action_id),
        status: str(msg.status),
        reason: str(msg.reason),
      };
    case "error":
      return { type: "error", action_id: str(msg.action_id), code: str(msg.code) };
    case "state": {
      const p = msg.player;
      return {
        type: "state",
        tick: toBigInt(msg.tick),
        paused: msg.paused === true,
        player:
          p && typeof p === "object"
            ? {
                water: toBigInt(p.water),
                tank_capacity: toBigInt(p.tank_capacity),
                leaves: toBigInt(p.leaves),
              }
            : null,
      };
    }
    default:
      return { type: "unknown", raw: text };
  }
}

/** Una línea del registro de la página para cada mensaje decodificado. */
export function describeFrame(frame) {
  switch (frame.type) {
    case "result":
      return `result ${frame.action_id}: ${frame.status}${frame.reason ? ` (${frame.reason})` : ""}`;
    case "error":
      return `error ${frame.code}${frame.action_id ? ` (${frame.action_id})` : ""}`;
    case "state": {
      const water = frame.player
        ? `, agua ${frame.player.water}/${frame.player.tank_capacity}`
        : "";
      return `state tick ${frame.tick}${frame.paused ? " (en pausa)" : ""}${water}`;
    }
    default:
      return `mensaje no reconocido: ${frame.raw}`;
  }
}

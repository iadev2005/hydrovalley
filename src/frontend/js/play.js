// Mensajes del WebSocket /play (proto/hydrovalley/play/v1/play.proto) y
// utilidades sin DOM, para poder probarlas con node:test.

/**
 * Lado del valle (32 × 32 casillas, D-01). Solo sirve para proponer
 * coordenadas en la página: los límites los decide el servidor, que
 * rechaza las gotas fuera del valle.
 */
export const GRID_SIZE = 32;

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

/**
 * Código con el que el hub cierra `/play` cuando el token es inválido o
 * caducó (diagrama 09). Está en el rango privado del WebSocket.
 */
export const AUTH_CLOSE_CODE = 4401;

/**
 * Si conviene reintentar tras un cierre. Con 4401 no: hace falta un token
 * nuevo y reintentar con el mismo solo repetiría el rechazo.
 * @param {number} code código de cierre del WebSocket
 */
export function shouldReconnect(code) {
  return code !== AUTH_CLOSE_CODE;
}

/**
 * Qué hacer cuando se cierra el WebSocket de la sesión:
 * - `ignore`: el cierre es de un socket que ya no es el actual (salida o
 *   sustitución por otra sesión): no hay nada que hacer;
 * - `show_access`: el hub rechazó el token (4401): volver a entrar;
 * - `expired`: la sesión guardada ya venció, así que reconectar solo
 *   repetiría el rechazo: volver a entrar;
 * - `reconnect`: caída de red con sesión vigente: reintentar con espera.
 * @param {{code: number, current: boolean, expiresAt: number, now: number}} close
 * @returns {"ignore" | "show_access" | "expired" | "reconnect"}
 */
export function decideOnClose({ code, current, expiresAt, now }) {
  if (!current) return "ignore";
  if (!shouldReconnect(code)) return "show_access";
  if (expiresAt <= now) return "expired";
  return "reconnect";
}

/**
 * Dirección del WebSocket en el mismo origen que la página (nginx la
 * reenvía al hub). La identidad sale solo del token firmado. Sin mapa:
 * esta página no dibuja el valle.
 * @param {{protocol: string, host: string}} location
 * @param {string} token
 */
export function buildPlayUrl(location, token) {
  const scheme = location.protocol === "https:" ? "wss:" : "ws:";
  const query = new URLSearchParams({ token, map: "false" });
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

/** Espera base de la reconexión: la del primer reintento, como máximo. */
export const RECONNECT_BASE_MS = 500;
/** Techo de la espera de reconexión. */
export const RECONNECT_CAP_MS = 10_000;
/** Una conexión que dura al menos esto reinicia la secuencia de esperas. */
export const RECONNECT_STABLE_MS = 5_000;

/**
 * Espera antes del reintento número `attempt` (desde 0): espera exponencial
 * con *full jitter*, un valor al azar en [0, min(techo, base · 2^attempt)).
 * El azar evita que todos los clientes se reconecten a la vez tras una caída.
 * @param {number} attempt
 * @param {() => number} [random] fuente en [0, 1)
 * @returns {number} milisegundos
 */
export function reconnectDelay(attempt, random = Math.random) {
  if (!Number.isInteger(attempt) || attempt < 0) {
    throw new RangeError(`attempt must be a non-negative integer, got ${attempt}`);
  }
  // A partir de 2^5 · 500 ms ya se supera el techo; se acota el exponente
  // para no calcular potencias enormes.
  const ceiling = Math.min(RECONNECT_CAP_MS, RECONNECT_BASE_MS * 2 ** Math.min(attempt, 16));
  return Math.floor(random() * ceiling);
}

/**
 * Estado de la reconexión de un WebSocket. `opened()` al abrirse,
 * `closed()` al cerrarse y `nextDelay()` para saber cuánto esperar antes
 * del siguiente intento. Si la conexión duró al menos `RECONNECT_STABLE_MS`,
 * la secuencia vuelve a empezar desde la espera base.
 * @param {{random?: () => number, now?: () => number}} [deps]
 */
export function createReconnectBackoff({ random = Math.random, now = Date.now } = {}) {
  let attempt = 0;
  /** @type {number | null} */
  let openedAt = null;
  return {
    opened() {
      openedAt = now();
    },
    closed() {
      if (openedAt !== null && now() - openedAt >= RECONNECT_STABLE_MS) attempt = 0;
      openedAt = null;
    },
    nextDelay() {
      const delay = reconnectDelay(attempt, random);
      attempt += 1;
      return delay;
    },
  };
}

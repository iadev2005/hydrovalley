// Mensajes del WebSocket /play (proto/hydrovalley/play/v1/play.proto) y
// utilidades sin DOM, para poder probarlas con vitest.

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
 */
export function newUuid(
  fill: (bytes: Uint8Array<ArrayBuffer>) => Uint8Array<ArrayBuffer> = (bytes) => {
    globalThis.crypto.getRandomValues(bytes);
    return bytes;
  },
): string {
  const b = fill(new Uint8Array(16));
  const byte6 = b[6] ?? 0;
  const byte8 = b[8] ?? 0;
  b[6] = (byte6 & 0x0f) | 0x40; // versión 4
  b[8] = (byte8 & 0x3f) | 0x80; // variante RFC 9562
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
 */
export function shouldReconnect(code: number): boolean {
  return code !== AUTH_CLOSE_CODE;
}

export type CloseAction = "ignore" | "show_access" | "expired" | "reconnect";

export type ConnectionStatus = "disconnected" | "connecting" | "connected";

/**
 * Qué hacer cuando se cierra el WebSocket de la sesión:
 * - `ignore`: el cierre es de un socket que ya no es el actual (salida o
 *   sustitución por otra sesión): no hay nada que hacer;
 * - `show_access`: el hub rechazó el token (4401): volver a entrar;
 * - `expired`: la sesión guardada ya venció, así que reconectar solo
 *   repetiría el rechazo: volver a entrar;
 * - `reconnect`: caída de red con sesión vigente: reintentar con espera.
 */
export function decideOnClose({
  code,
  current,
  expiresAt,
  now,
}: {
  code: number;
  current: boolean;
  expiresAt: number;
  now: number;
}): CloseAction {
  if (!current) return "ignore";
  if (!shouldReconnect(code)) return "show_access";
  if (expiresAt <= now) return "expired";
  return "reconnect";
}

/**
 * Dirección del WebSocket en el mismo origen que la página (nginx la
 * reenvía al hub). La identidad sale solo del token firmado. Con
 * `map: true` (por defecto) el estado trae el mapa del valle.
 */
export function buildPlayUrl(
  location: { protocol: string; host: string },
  token: string,
  { map = true }: { map?: boolean } = {},
): string {
  const scheme = location.protocol === "https:" ? "wss:" : "ws:";
  const query = new URLSearchParams({ token, map: String(map) });
  return `${scheme}//${location.host}/play?${query.toString()}`;
}

function assertCoordinates(x: number, y: number): void {
  for (const [name, v] of [
    ["x", x],
    ["y", y],
  ] as const) {
    if (!Number.isInteger(v) || v < 0) {
      throw new RangeError(`${name} must be a non-negative integer, got ${v}`);
    }
  }
}

/**
 * `DropMessage` en JSON. Las coordenadas son enteros no negativos; el
 * servidor decide si caen dentro del valle.
 */
export function buildDropFrame({ x, y, actionId }: { x: number; y: number; actionId: string }): string {
  assertCoordinates(x, y);
  return JSON.stringify({ type: "drop", action_id: actionId, x, y });
}

/** Instalaciones y mejoras que acepta el hub (play.proto). */
export const INSTALLATIONS = ["sprinkler", "drip", "treatment_plant"] as const;
export type InstallationType = (typeof INSTALLATIONS)[number];

export const UPGRADES = ["bigger_drop", "bigger_tank"] as const;
export type UpgradeType = (typeof UPGRADES)[number];

/**
 * `PlaceInstallationMessage` en JSON.
 */
export function buildPlaceInstallationFrame({
  installation,
  x,
  y,
  actionId,
}: {
  installation: string;
  x: number;
  y: number;
  actionId: string;
}): string {
  if (!INSTALLATIONS.includes(installation as InstallationType)) {
    throw new RangeError(`unknown installation ${installation}`);
  }
  assertCoordinates(x, y);
  return JSON.stringify({ type: "place_installation", action_id: actionId, installation, x, y });
}

/**
 * `BuyUpgradeMessage` en JSON.
 */
export function buildBuyUpgradeFrame({
  upgrade,
  actionId,
}: {
  upgrade: string;
  actionId: string;
}): string {
  if (!UPGRADES.includes(upgrade as UpgradeType)) {
    throw new RangeError(`unknown upgrade ${upgrade}`);
  }
  return JSON.stringify({ type: "buy_upgrade", action_id: actionId, upgrade });
}

/**
 * `SetDemoModeMessage` en JSON (solo administrador, RF-31).
 */
export function buildSetDemoModeFrame({
  enabled,
  actionId,
}: {
  enabled: boolean;
  actionId: string;
}): string {
  if (typeof enabled !== "boolean") throw new TypeError("enabled must be a boolean");
  return JSON.stringify({ type: "set_demo_mode", action_id: actionId, enabled });
}

/**
 * `ResetValleyMessage` en JSON (solo administrador, RF-31).
 */
export function buildResetValleyFrame({ actionId }: { actionId: string }): string {
  return JSON.stringify({ type: "reset_valley", action_id: actionId });
}

/** uint64 de protojson: texto (o número si es pequeño); ausente → 0. */
function toBigInt(value: unknown): bigint {
  if (typeof value === "string" || typeof value === "number") {
    try {
      return BigInt(value);
    } catch {
      return 0n;
    }
  }
  return 0n;
}

const str = (value: unknown): string => (typeof value === "string" ? value : "");

function parseOfflineSummary(value: unknown): OfflineSummary | null {
  if (value === null || typeof value !== "object" || Array.isArray(value)) return null;
  const rec = value as Record<string, unknown>;
  return {
    leaves_earned: toBigInt(rec["leaves_earned"]),
    water_reused: toBigInt(rec["water_reused"]),
    fires_extinguished: toBigInt(rec["fires_extinguished"]),
    tiles_grown: toBigInt(rec["tiles_grown"]),
  };
}

export interface FireState {
  x: number;
  y: number;
  drops_received: number;
}

export interface PlacedInstallation {
  x: number;
  y: number;
  installation: string;
  owner_id: string;
}

/**
 * Lo que hicieron los aspersores del jugador mientras no estaba (RF-25;
 * `OfflineSummaryView` de play.proto). Solo llega en el primer `state` de la
 * conexión y solo si no está vacío.
 */
export interface OfflineSummary {
  leaves_earned: bigint;
  water_reused: bigint;
  fires_extinguished: bigint;
  tiles_grown: bigint;
}

export interface PlayerState {
  water: bigint;
  tank_capacity: bigint;
  leaves: bigint;
  upgrades: string[];
  next_sprinkler_price: bigint;
  next_drip_price: bigint;
  next_treatment_plant_price: bigint;
  offline_summary: OfflineSummary | null;
}

export interface ResultFrame {
  type: "result";
  action_id: string;
  status: string;
  reason: string;
}

export interface ErrorFrame {
  type: "error";
  action_id: string;
  code: string;
}

export interface StateFrame {
  type: "state";
  tick: bigint;
  paused: boolean;
  demo_mode: boolean;
  width: number;
  height: number;
  stages: number[];
  humidity: bigint[];
  fires: FireState[];
  installations: PlacedInstallation[];
  player: PlayerState | null;
}

export interface UnknownFrame {
  type: "unknown";
  raw: string;
}

export type PlayFrame = ResultFrame | ErrorFrame | StateFrame | UnknownFrame;

/**
 * Decodifica un mensaje del hub. Los campos ausentes toman su valor cero
 * (convención de protojson) y los uint64 se leen como BigInt (regla 7).
 */
export function parseFrame(text: string): PlayFrame {
  let msg: unknown;
  try {
    msg = JSON.parse(text);
  } catch {
    return { type: "unknown", raw: text };
  }
  if (msg === null || typeof msg !== "object" || Array.isArray(msg)) {
    return { type: "unknown", raw: text };
  }
  const rec = msg as Record<string, unknown>;
  switch (rec["type"]) {
    case "result":
      return {
        type: "result",
        action_id: str(rec["action_id"]),
        status: str(rec["status"]),
        reason: str(rec["reason"]),
      };
    case "error":
      return { type: "error", action_id: str(rec["action_id"]), code: str(rec["code"]) };
    case "state": {
      const p = rec["player"] as Record<string, unknown> | null | undefined;
      const list = (value: unknown): unknown[] => (Array.isArray(value) ? value : []);
      return {
        type: "state",
        tick: toBigInt(rec["tick"]),
        paused: rec["paused"] === true,
        demo_mode: rec["demo_mode"] === true,
        width: Number(rec["width"]) || 0,
        height: Number(rec["height"]) || 0,
        stages: list(rec["stages"]).map((v) => Number(v) || 0),
        humidity: list(rec["humidity"]).map(toBigInt),
        fires: list(rec["fires"]).map((f) => {
          const rf = (f && typeof f === "object" ? f : {}) as Record<string, unknown>;
          return {
            x: Number(rf["x"]) || 0,
            y: Number(rf["y"]) || 0,
            drops_received: Number(rf["drops_received"]) || 0,
          };
        }),
        installations: list(rec["installations"]).map((i) => {
          const ri = (i && typeof i === "object" ? i : {}) as Record<string, unknown>;
          return {
            x: Number(ri["x"]) || 0,
            y: Number(ri["y"]) || 0,
            installation: str(ri["installation"]),
            owner_id: str(ri["owner_id"]),
          };
        }),
        player:
          p && typeof p === "object"
            ? {
                water: toBigInt(p["water"]),
                tank_capacity: toBigInt(p["tank_capacity"]),
                leaves: toBigInt(p["leaves"]),
                upgrades: list(p["upgrades"]).filter((u): u is string => typeof u === "string"),
                next_sprinkler_price: toBigInt(p["next_sprinkler_price"]),
                next_drip_price: toBigInt(p["next_drip_price"]),
                next_treatment_plant_price: toBigInt(p["next_treatment_plant_price"]),
                offline_summary: parseOfflineSummary(p["offline_summary"]),
              }
            : null,
      };
    }
    default:
      return { type: "unknown", raw: text };
  }
}

/** Una línea del registro de la página para cada mensaje decodificado. */
export function describeFrame(frame: PlayFrame): string {
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
 * Silencio máximo sin recibir un mensaje en `/play` antes de dar la conexión
 * por muerta y forzar la reconexión (el hub difunde `state` cada 1 s; cuando
 * se apaga una máquina o se pausa un contenedor, TCP no envía `FIN` ni `RST`).
 */
export const STATE_SILENCE_MS = 3_500;
/**
 * Plazo máximo de cada intento HTTP individual hacia un proxy antes de
 * abortar y conmutar al siguiente host del rotador (evita quedarse bloqueado
 * 21–75 s en el timeout de SYN del sistema operativo cuando un nodo entra en
 * `docker pause` o pierde alimentación).
 */
export const FETCH_ATTEMPT_TIMEOUT_MS = 3_000;
/**
 * Plazo máximo para completar el handshake `open` de un WebSocket antes de
 * cerrar el intento y rotar al siguiente proxy de respaldo.
 */
export const WS_HANDSHAKE_TIMEOUT_MS = 4_000;

/**
 * Normaliza una entrada de dirección de respaldo (`https://10.68.17.99:8080/`,
 * `10.68.17.99:8080`, etc.) a `host[:puerto]`.
 */
function normalizeHostEntry(raw: string): string {
  const trimmed = raw.trim();
  if (!trimmed) return "";
  try {
    if (trimmed.includes("://")) {
      return new URL(trimmed).host;
    }
    return new URL(`http://${trimmed}`).host;
  } catch {
    return trimmed.replace(/^\/+|\/+$/g, "");
  }
}

/**
 * Construye una lista ordenada y sin duplicados de `host[:puerto]` a partir
 * del host primario y de una o varias listas de respaldos (separadas por comas
 * o en arreglo).
 */
export function parseBackupHosts(
  primaryHost: string,
  ...sources: (string | readonly string[] | null | undefined)[]
): string[] {
  const result: string[] = [];
  const seen = new Set<string>();
  const push = (entry: string) => {
    const host = normalizeHostEntry(entry);
    if (host && !seen.has(host)) {
      seen.add(host);
      result.push(host);
    }
  };
  push(primaryHost);
  for (const src of sources) {
    if (!src) continue;
    const items = typeof src === "string" ? src.split(",") : src;
    for (const item of items) {
      push(item);
    }
  }
  return result;
}

export interface EndpointRotator {
  currentHost(): string;
  hosts(): readonly string[];
  advance(): string;
  addBackups(...sources: (string | readonly string[] | null | undefined)[]): void;
}

/**
 * Rotador con afinidad al nodo activo (*sticky*) para conmutar entre el proxy
 * primario y los de respaldo sin cambiar la URL de la barra del navegador.
 */
export function createEndpointRotator(
  primaryHost: string,
  ...initialBackups: (string | readonly string[] | null | undefined)[]
): EndpointRotator {
  let hostList = parseBackupHosts(primaryHost, ...initialBackups);
  if (hostList.length === 0 && primaryHost) {
    hostList = [primaryHost];
  }
  let index = 0;

  return {
    currentHost(): string {
      return hostList[index] ?? primaryHost;
    },
    hosts(): readonly string[] {
      return [...hostList];
    },
    advance(): string {
      if (hostList.length > 1) {
        index = (index + 1) % hostList.length;
      }
      return hostList[index] ?? primaryHost;
    },
    addBackups(...sources: (string | readonly string[] | null | undefined)[]): void {
      const current = hostList[index] ?? primaryHost;
      hostList = parseBackupHosts(hostList[0] ?? primaryHost, hostList, ...sources);
      const found = hostList.indexOf(current);
      index = found >= 0 ? found : 0;
    },
  };
}

/**
 * Envuelve `fetch` para reintentar automáticamente contra el siguiente proxy
 * del rotador cuando la petición falla por red o recibe `502`/`503`/`504`
 * de una pasarela caída, y aprende respaldos anunciados en `X-Proxy-Backups`.
 */
export function createFailoverFetch({
  baseFetch,
  rotator,
  protocol,
  onBackupsDiscovered,
  attemptTimeoutMs = FETCH_ATTEMPT_TIMEOUT_MS,
}: {
  baseFetch: (input: string | URL | Request, init?: RequestInit) => Promise<Response>;
  rotator: EndpointRotator;
  protocol: string;
  onBackupsDiscovered?: (headerValue: string) => void;
  attemptTimeoutMs?: number;
}): (input: string | URL | Request, init?: RequestInit) => Promise<Response> {
  return async (input: string | URL | Request, init?: RequestInit): Promise<Response> => {
    const rawUrl = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
    const isRelative = rawUrl.startsWith("/");
    const totalHosts = Math.max(1, rotator.hosts().length);
    let lastError: unknown;

    for (let attempt = 0; attempt < totalHosts; attempt++) {
      const host = rotator.currentHost();
      const targetUrl = isRelative && host ? `${protocol}//${host}${rawUrl}` : rawUrl;
      const controller =
        !init?.signal && attemptTimeoutMs > 0 && typeof AbortController !== "undefined"
          ? new AbortController()
          : null;
      const timer =
        controller !== null ? setTimeout(() => controller.abort(), attemptTimeoutMs) : null;
      const effectiveInit: RequestInit | undefined =
        controller !== null ? { ...init, signal: controller.signal } : init;
      try {
        const res = await baseFetch(targetUrl, effectiveInit);
        if (timer !== null) clearTimeout(timer);
        const backupsHeader = res.headers?.get?.("X-Proxy-Backups");
        if (backupsHeader) {
          rotator.addBackups(backupsHeader);
          onBackupsDiscovered?.(backupsHeader);
        }
        if ((res.status === 502 || res.status === 503 || res.status === 504) && attempt + 1 < Math.max(totalHosts, rotator.hosts().length)) {
          rotator.advance();
          continue;
        }
        return res;
      } catch (err) {
        if (timer !== null) clearTimeout(timer);
        lastError = err;
        if (attempt + 1 < Math.max(totalHosts, rotator.hosts().length)) {
          rotator.advance();
          continue;
        }
        throw err;
      }
    }
    throw lastError instanceof Error ? lastError : new TypeError("Failed to fetch");
  };
}

/**
 * Espera antes del reintento número `attempt` (desde 0): espera exponencial
 * con *full jitter*, un valor al azar en [0, min(techo, base · 2^attempt)).
 * El azar evita que todos los clientes se reconecten a la vez tras una caída.
 */
export function reconnectDelay(attempt: number, random: () => number = Math.random): number {
  if (!Number.isInteger(attempt) || attempt < 0) {
    throw new RangeError(`attempt must be a non-negative integer, got ${attempt}`);
  }
  // A partir de 2^5 · 500 ms ya se supera el techo; se acota el exponente
  // para no calcular potencias enormes.
  const ceiling = Math.min(RECONNECT_CAP_MS, RECONNECT_BASE_MS * 2 ** Math.min(attempt, 16));
  return Math.floor(random() * ceiling);
}

export interface ReconnectBackoff {
  opened(): void;
  closed(): void;
  nextDelay(): number;
}

/**
 * Estado de la reconexión de un WebSocket. `opened()` al abrirse,
 * `closed()` al cerrarse y `nextDelay()` para saber cuánto esperar antes
 * del siguiente intento. Si la conexión duró al menos `RECONNECT_STABLE_MS`,
 * la secuencia vuelve a empezar desde la espera base.
 */
export function createReconnectBackoff({
  random = Math.random,
  now = Date.now,
}: {
  random?: () => number;
  now?: () => number;
} = {}): ReconnectBackoff {
  let attempt = 0;
  let openedAt: number | null = null;
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

// Configuración de la API a partir de variables de entorno (regla 1 de
// portabilidad: ninguna dirección en el código).

export interface Config {
  /** Interfaz en la que escucha el HTTP. */
  host: string;
  /** Puerto del HTTP. */
  port: number;
  /** Nivel de registro de pino. */
  logLevel: LogLevel;
}

/** Niveles que acepta pino, del más grave al más detallado. */
export const LOG_LEVELS = ["fatal", "error", "warn", "info", "debug", "trace", "silent"] as const;
export type LogLevel = (typeof LOG_LEVELS)[number];

const DEFAULT_LOG_LEVEL: LogLevel = "info";

function isLogLevel(value: string): value is LogLevel {
  return (LOG_LEVELS as readonly string[]).includes(value);
}

const DEFAULT_LISTEN = "0.0.0.0:3000";

type Env = Readonly<Record<string, string | undefined>>;

/**
 * Lee `API_LISTEN` (`host:puerto`, `:puerto` o `[ipv6]:puerto`; por defecto
 * `0.0.0.0:3000`) y `LOG_LEVEL` (un nivel de pino; por defecto `info`).
 * Lanza un error que nombra la variable si un valor no es válido.
 */
export function loadConfig(env: Env): Config {
  const listen = env.API_LISTEN ?? DEFAULT_LISTEN;
  const match = /^(?:\[([^\]]+)\]|([^:[\]]*)):(\d+)$/.exec(listen);
  if (!match) {
    throw new Error(`API_LISTEN must be host:port, got "${listen}"`);
  }
  const host = match[1] ?? match[2] ?? "";
  const port = Number(match[3]);
  if (!Number.isInteger(port) || port < 1 || port > 65535) {
    throw new Error(`API_LISTEN port must be 1-65535, got "${listen}"`);
  }
  const logLevel = env.LOG_LEVEL ?? DEFAULT_LOG_LEVEL;
  if (!isLogLevel(logLevel)) {
    throw new Error(`LOG_LEVEL must be one of ${LOG_LEVELS.join(", ")}; got "${logLevel}"`);
  }
  return { host: host === "" ? "0.0.0.0" : host, port, logLevel };
}

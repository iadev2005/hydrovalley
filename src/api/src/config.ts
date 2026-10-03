// Configuración de la API a partir de variables de entorno (regla 1 de
// portabilidad: ninguna dirección en el código).
import { createPrivateKey, type KeyObject } from "node:crypto";
import { MAX_TOKEN_TTL_SECONDS } from "./limits.js";

/** Lo que necesita el proceso para escuchar (también lo usa el HEALTHCHECK). */
export interface ListenConfig {
  /** Interfaz en la que escucha el HTTP. */
  host: string;
  /** Puerto del HTTP. */
  port: number;
  /** Nivel de registro de pino. */
  logLevel: LogLevel;
}

export interface Config extends ListenConfig {
  /** `host:puerto` de la persistencia (gRPC), de `PERSISTENCE_ADDR`. */
  persistenceAddr: string;
  /** `host:puerto` del simulador del valle (gRPC), de `WORLD_ADDR`. */
  worldAddr: string;
  /** Clave privada Ed25519 con la que se firman los tokens. */
  tokenPrivateKey: KeyObject;
  /** Identificador de la clave (`kid`), opcional. */
  tokenKeyId?: string | undefined;
  /** Vigencia del token en segundos (12 h por defecto). */
  tokenTtlSeconds: number;
  /**
   * Saltos de proxy de confianza para la IP del cliente (`X-Forwarded-For`):
   * `false` ninguno; `1` solo nginx.
   */
  trustProxy: false | number;
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

const DEFAULT_TOKEN_TTL_SECONDS = 43200;
const MAX_TRUST_PROXY_HOPS = 10;

/**
 * Lee `API_LISTEN` (`host:puerto`, `:puerto` o `[ipv6]:puerto`; por defecto
 * `0.0.0.0:3000`) y `LOG_LEVEL` (un nivel de pino; por defecto `info`).
 * Lanza un error que nombra la variable si un valor no es válido.
 */
export function loadListenConfig(env: Env): ListenConfig {
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

/** `host:puerto` o `[ipv6]:puerto`, con puerto de 1 a 65535. */
function parseAddress(name: string, value: string): string {
  const match = /^(?:\[[0-9A-Fa-f:.]+\]|[A-Za-z0-9._-]+):(\d+)$/.exec(value);
  const port = Number(match?.[1]);
  if (!match || port < 1 || port > 65535) {
    throw new Error(`${name} must be host:port, got "${value}"`);
  }
  return value;
}

/**
 * Acepta el PEM PKCS#8 tal cual, con los saltos de línea escritos como `\n`
 * (como los dejan algunos cargadores de .env) o en base64 del PEM completo.
 * Los mensajes de error nunca incluyen el valor.
 */
function parsePrivateKey(name: string, raw: string): KeyObject {
  const value = raw.trim();
  let pem = value;
  if (!value.includes("-----BEGIN")) {
    const decoded = Buffer.from(value, "base64").toString("utf8");
    pem = decoded.includes("-----BEGIN") ? decoded : value;
  }
  let key: KeyObject;
  try {
    key = createPrivateKey({ key: pem.replaceAll("\\n", "\n"), format: "pem" });
  } catch {
    throw new Error(`${name} must be a PKCS#8 PEM private key (or the base64 of that PEM)`);
  }
  if (key.asymmetricKeyType !== "ed25519") {
    throw new Error(`${name} must be an Ed25519 private key`);
  }
  return key;
}

function required(env: Env, name: string): string {
  const value = env[name];
  if (value === undefined || value.trim() === "") {
    throw new Error(`${name} is required`);
  }
  return value;
}

/**
 * Configuración completa: lo de `loadListenConfig` más `PERSISTENCE_ADDR` y
 * `WORLD_ADDR` (obligatorias, sin valor por defecto), `TOKEN_PRIVATE_KEY` (obligatoria),
 * `TOKEN_TTL_SECONDS` (por defecto 43200, máximo 604800), `TOKEN_KEY_ID` y `TRUST_PROXY`.
 */
export function loadConfig(env: Env): Config {
  const listen = loadListenConfig(env);
  const persistenceAddr = parseAddress("PERSISTENCE_ADDR", required(env, "PERSISTENCE_ADDR"));
  const worldAddr = parseAddress("WORLD_ADDR", required(env, "WORLD_ADDR"));
  const tokenPrivateKey = parsePrivateKey("TOKEN_PRIVATE_KEY", required(env, "TOKEN_PRIVATE_KEY"));

  const ttl = env.TOKEN_TTL_SECONDS;
  if (ttl !== undefined && !/^[1-9]\d*$/.test(ttl)) {
    throw new Error(`TOKEN_TTL_SECONDS must be a positive integer, got "${ttl}"`);
  }
  if (ttl !== undefined && Number(ttl) > MAX_TOKEN_TTL_SECONDS) {
    throw new Error(
      `TOKEN_TTL_SECONDS must be at most ${String(MAX_TOKEN_TTL_SECONDS)} (7 days), got "${ttl}"`,
    );
  }
  const trust = env.TRUST_PROXY;
  let trustProxy: false | number = false;
  if (trust === "true") {
    // Un solo salto: nginx sobrescribe X-Forwarded-For con $remote_addr, así
    // que una entrada previa del cliente no se toma en cuenta.
    trustProxy = 1;
  } else if (trust !== undefined && trust !== "false") {
    const hops = /^[1-9]\d*$/.test(trust) ? Number(trust) : 0;
    if (hops < 1 || hops > MAX_TRUST_PROXY_HOPS) {
      throw new Error(
        `TRUST_PROXY must be "true", "false" or a hop count from 1 to ${String(MAX_TRUST_PROXY_HOPS)}, got "${trust}"`,
      );
    }
    trustProxy = hops;
  }
  return {
    ...listen,
    persistenceAddr,
    worldAddr,
    tokenPrivateKey,
    tokenKeyId:
      env.TOKEN_KEY_ID === undefined || env.TOKEN_KEY_ID === "" ? undefined : env.TOKEN_KEY_ID,
    tokenTtlSeconds: ttl === undefined ? DEFAULT_TOKEN_TTL_SECONDS : Number(ttl),
    trustProxy,
  };
}

// Límites de la API en un solo lugar: los usan main.ts, las descripciones de
// OpenAPI y el README. Todos son en memoria y por instancia.

export interface RateLimit {
  /** Intentos admitidos por ventana. */
  readonly limit: number;
  /** Duración de la ventana, en milisegundos. */
  readonly windowMs: number;
}

/** Acceso: intentos por usuario (sin distinguir mayúsculas) e IP. */
export const LOGIN_LIMIT_PER_USER_IP: RateLimit = {
  limit: 10,
  windowMs: 60_000,
};
/** Acceso: intentos por IP entre todos los usuarios (contra la adivinación masiva). */
export const LOGIN_LIMIT_PER_IP: RateLimit = { limit: 30, windowMs: 60_000 };
/** Registro: cuentas nuevas por IP. */
export const REGISTER_LIMIT_PER_IP: RateLimit = { limit: 5, windowMs: 600_000 };

/** Consultas de ranking y reportes (GET): peticiones por IP. */
export const READ_LIMIT_PER_IP: RateLimit = { limit: 60, windowMs: 60_000 };

/**
 * Panel del sistema (`GET /api/system`): peticiones por IP. El panel consulta
 * cada 2 s (30 por minuto por pestaña); 120 por minuto deja margen para varias
 * pestañas detrás de la misma IP.
 */
export const SYSTEM_LIMIT_PER_IP: RateLimit = { limit: 120, windowMs: 60_000 };

/** Aprovisionamiento de cuentas de bots: peticiones por administrador (por `player_id`). */
export const ADMIN_LIMIT_PER_ADMIN: RateLimit = { limit: 10, windowMs: 60_000 };
/** Máximo de cuentas por petición de aprovisionamiento. */
export const BOT_ACCOUNTS_MAX_COUNT = 100;
/** Hashes Argon2id que se calculan a la vez al aprovisionar (cada uno usa ~19 MiB). */
export const BOT_HASH_CONCURRENCY = 4;
/** Nombres distintos que se prueban por cuenta si el elegido ya existe. */
export const BOT_NAME_ATTEMPTS = 5;

/** Claves que guarda cada limitador antes de rechazar las nuevas. */
export const LIMITER_MAX_KEYS = 10_000;

/** Vigencia máxima admitida para `TOKEN_TTL_SECONDS` (7 días). */
export const MAX_TOKEN_TTL_SECONDS = 604_800;

/** Texto de ventana para las descripciones («por minuto», «cada 10 minutos»). */
export function describeWindow(windowMs: number): string {
  const minutes = windowMs / 60_000;
  return minutes === 1 ? "por minuto" : `cada ${String(minutes)} minutos`;
}

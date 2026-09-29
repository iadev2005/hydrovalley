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

/** Claves que guarda cada limitador antes de rechazar las nuevas. */
export const LIMITER_MAX_KEYS = 10_000;

/** Vigencia máxima admitida para `TOKEN_TTL_SECONDS` (7 días). */
export const MAX_TOKEN_TTL_SECONDS = 604_800;

/** Texto de ventana para las descripciones («por minuto», «cada 10 minutos»). */
export function describeWindow(windowMs: number): string {
  const minutes = windowMs / 60_000;
  return minutes === 1 ? "por minuto" : `cada ${String(minutes)} minutos`;
}

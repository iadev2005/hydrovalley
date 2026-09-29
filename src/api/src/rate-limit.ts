// Límite de intentos en memoria, por clave y ventana fija.
// Es por instancia: con varias réplicas de la API cada una cuenta aparte.

export interface AttemptResult {
  allowed: boolean;
  /** Segundos hasta que se libera la clave (solo si `allowed` es falso). */
  retryAfterSeconds: number;
}

export interface AttemptLimiter {
  /** Registra un intento y dice si se admite. */
  hit(key: string): AttemptResult;
  /** Número de claves en memoria. */
  size(): number;
}

export interface AttemptLimiterOptions {
  limit: number;
  windowMs: number;
  now?: () => number;
  /** Tope de claves; lleno de ventanas vivas, las claves nuevas se rechazan. */
  maxKeys?: number;
}

interface Window {
  count: number;
  resetAt: number;
}

const DEFAULT_MAX_KEYS = 10_000;

export function createAttemptLimiter(options: AttemptLimiterOptions): AttemptLimiter {
  const now = options.now ?? Date.now;
  const maxKeys = options.maxKeys ?? DEFAULT_MAX_KEYS;
  // Todas las ventanas duran lo mismo y `resetAt` crece con el momento de
  // creación, así que el orden de inserción de Map es también el orden de
  // caducidad: las caducadas forman un prefijo y podar cuesta lo que se
  // poda, nunca un recorrido completo.
  const windows = new Map<string, Window>();

  function pruneExpired(at: number): void {
    for (const [key, w] of windows) {
      if (w.resetAt > at) break;
      windows.delete(key);
    }
  }

  return {
    hit(key) {
      const at = now();
      let w = windows.get(key);
      if (w && w.resetAt <= at) {
        windows.delete(key);
        w = undefined;
      }
      if (!w) {
        if (windows.size >= maxKeys) {
          pruneExpired(at);
          if (windows.size >= maxKeys) {
            // Lleno de ventanas vivas: nunca se expulsa una clave bloqueada o
            // dentro de su ventana; la nueva se rechaza (falla cerrado) hasta
            // que caduque la más antigua.
            const oldest = windows.values().next();
            const resetAt = oldest.done ? at : oldest.value.resetAt;
            return {
              allowed: false,
              retryAfterSeconds: Math.ceil((resetAt - at) / 1000),
            };
          }
        }
        w = { count: 0, resetAt: at + options.windowMs };
        windows.set(key, w);
      }
      w.count++;
      if (w.count > options.limit) {
        return {
          allowed: false,
          retryAfterSeconds: Math.ceil((w.resetAt - at) / 1000),
        };
      }
      return { allowed: true, retryAfterSeconds: 0 };
    },
    size: () => windows.size,
  };
}

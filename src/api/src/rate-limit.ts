// Límite de intentos en memoria, por clave (usuario + IP) y ventana fija.
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
  /** Tope de claves; al llegar se expulsa la más antigua. */
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
  // Map conserva el orden de inserción: la primera clave es la más antigua.
  const windows = new Map<string, Window>();

  function prune(at: number): void {
    for (const [key, w] of windows) {
      if (w.resetAt <= at) windows.delete(key);
    }
    while (windows.size >= maxKeys) {
      const oldest = windows.keys().next();
      if (oldest.done) break;
      windows.delete(oldest.value);
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
        if (windows.size >= maxKeys) prune(at);
        w = { count: 0, resetAt: at + options.windowMs };
        windows.set(key, w);
      }
      w.count++;
      if (w.count > options.limit) {
        return { allowed: false, retryAfterSeconds: Math.ceil((w.resetAt - at) / 1000) };
      }
      return { allowed: true, retryAfterSeconds: 0 };
    },
    size: () => windows.size,
  };
}

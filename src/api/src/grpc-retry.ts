// Política de llamadas gRPC de CLAUDE.md compartida por los adaptadores:
// plazo de 2 s por llamada; las idempotentes se reintentan hasta 3 veces con
// esperas de 100, 200 y 400 ms (más un poco de azar) dentro de un
// presupuesto total de 3 s, y solo ante UNAVAILABLE o plazo agotado.
import { Code, ConnectError } from "@connectrpc/connect";

export interface GrpcCallOptions {
  /** `host:puerto` del servicio. */
  address: string;
  /** Plazo de cada llamada en milisegundos. */
  timeoutMs?: number;
  /** Esperas entre reintentos de las llamadas idempotentes. */
  retryDelaysMs?: readonly number[];
  /** Presupuesto total de una llamada idempotente, con todos sus intentos. */
  budgetMs?: number;
  /** Espera, reloj (ms) y azar inyectables para las pruebas. */
  sleep?: (ms: number) => Promise<void>;
  now?: () => number;
  random?: () => number;
}

const DEFAULT_TIMEOUT_MS = 2000;
const DEFAULT_RETRY_DELAYS_MS = [100, 200, 400] as const;
const DEFAULT_BUDGET_MS = 3000;
/** Fracción máxima de azar que se suma a cada espera. */
const JITTER = 0.25;

/** Códigos que indican que el servicio no respondió a tiempo. */
export const TRANSIENT_CODES: ReadonlySet<Code> = new Set([
  Code.Unavailable,
  Code.DeadlineExceeded,
]);

export interface GrpcCaller {
  /** Plazo de cada intento, para las llamadas que no se reintentan. */
  readonly timeoutMs: number;
  /**
   * Ejecuta la llamada; los fallos transitorios se reintentan con espera
   * creciente y algo de azar. Todo cabe en el presupuesto: el plazo de cada
   * intento se recorta a lo que queda y no se empieza una espera que lo
   * agotaría.
   */
  idempotent: <T>(call: (timeoutMs: number) => Promise<T>) => Promise<T>;
}

export function createGrpcCaller(options: GrpcCallOptions): GrpcCaller {
  const timeoutMs = options.timeoutMs ?? DEFAULT_TIMEOUT_MS;
  const retryDelays = options.retryDelaysMs ?? DEFAULT_RETRY_DELAYS_MS;
  const budgetMs = options.budgetMs ?? DEFAULT_BUDGET_MS;
  const now = options.now ?? Date.now;
  const random = options.random ?? Math.random;
  const sleep = options.sleep ?? ((ms: number) => new Promise<void>((r) => setTimeout(r, ms)));

  return {
    timeoutMs,
    async idempotent<T>(call: (timeoutMs: number) => Promise<T>): Promise<T> {
      const started = now();
      for (let attempt = 0; ; attempt++) {
        const remaining = budgetMs - (now() - started);
        try {
          return await call(Math.max(1, Math.min(timeoutMs, remaining)));
        } catch (err) {
          const base = retryDelays[attempt];
          if (base === undefined || !TRANSIENT_CODES.has(ConnectError.from(err).code)) throw err;
          const delay = base + Math.floor(base * JITTER * random());
          if (now() - started + delay >= budgetMs) throw err;
          await sleep(delay);
        }
      }
    },
  };
}

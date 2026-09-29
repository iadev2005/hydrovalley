// Cliente gRPC de AccountService (HTTP/2 sin cifrar dentro de la red del
// laboratorio). Política de CLAUDE.md: plazo de 2 s por llamada; GetAccount
// y GetUsernames, idempotentes, se reintentan hasta 3 veces con esperas de
// 100, 200 y 400 ms (más un poco de azar) dentro de un presupuesto total de
// 3 s; Register nunca se reintenta.
import { Code, ConnectError, createClient } from "@connectrpc/connect";
import { createGrpcTransport } from "@connectrpc/connect-node";
import {
  type AccountDirectory,
  type AccountRole,
  DirectoryUnavailableError,
  InvalidAccountError,
  UsernameTakenError,
} from "./account-directory.js";
import { AccountService, Role } from "./gen/hydrovalley/persistence/v1/account_pb.js";

export interface GrpcAccountDirectoryOptions {
  /** `host:puerto` de la persistencia (`PERSISTENCE_ADDR`). */
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

const TRANSIENT_CODES: ReadonlySet<Code> = new Set([Code.Unavailable, Code.DeadlineExceeded]);

function roleOf(role: Role): AccountRole {
  return role === Role.ADMIN ? "admin" : "player";
}

export function createGrpcAccountDirectory(options: GrpcAccountDirectoryOptions): AccountDirectory {
  const timeoutMs = options.timeoutMs ?? DEFAULT_TIMEOUT_MS;
  const retryDelays = options.retryDelaysMs ?? DEFAULT_RETRY_DELAYS_MS;
  const budgetMs = options.budgetMs ?? DEFAULT_BUDGET_MS;
  const now = options.now ?? Date.now;
  const random = options.random ?? Math.random;
  const sleep = options.sleep ?? ((ms: number) => new Promise<void>((r) => setTimeout(r, ms)));
  const client = createClient(
    AccountService,
    createGrpcTransport({ baseUrl: `http://${options.address}` }),
  );

  /**
   * Ejecuta la llamada; los fallos transitorios se reintentan con espera
   * creciente y algo de azar. Todo cabe en `budgetMs`: el plazo de cada
   * intento se recorta a lo que queda y no se empieza una espera que lo
   * agotaría.
   */
  async function idempotent<T>(call: (timeoutMs: number) => Promise<T>): Promise<T> {
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
  }

  /** Traduce los errores de gRPC que la API distingue; el resto se propaga. */
  function translate(err: unknown): Error {
    const error = ConnectError.from(err);
    if (TRANSIENT_CODES.has(error.code)) {
      return new DirectoryUnavailableError(`persistence call failed: ${Code[error.code]}`);
    }
    if (error.code === Code.AlreadyExists) return new UsernameTakenError();
    if (error.code === Code.InvalidArgument) return new InvalidAccountError();
    return error;
  }

  return {
    async register(username, passwordHash) {
      try {
        const res = await client.register({ username, passwordHash }, { timeoutMs });
        return { playerId: res.accountId, role: roleOf(res.role) };
      } catch (err) {
        throw translate(err);
      }
    },

    async getAccount(username) {
      try {
        const res = await idempotent((timeout) =>
          client.getAccount({ username }, { timeoutMs: timeout }),
        );
        return {
          playerId: res.accountId,
          passwordHash: res.passwordHash,
          role: roleOf(res.role),
        };
      } catch (err) {
        if (ConnectError.from(err).code === Code.NotFound) return null;
        throw translate(err);
      }
    },

    async getUsernames(playerIds) {
      try {
        const res = await idempotent((timeout) =>
          client.getUsernames({ playerIds: [...playerIds] }, { timeoutMs: timeout }),
        );
        return new Map(Object.entries(res.usernames));
      } catch (err) {
        throw translate(err);
      }
    },
  };
}

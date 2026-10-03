// Rutas de administración. Hoy solo el aprovisionamiento de cuentas de bots
// (`POST /api/admin/bot-accounts`): crea jugadores normales para los bots de
// `src/bots` sin pasar por el límite público de registro. Las contraseñas
// son aleatorias, solo existen en memoria el tiempo del hash y nunca se
// registran ni se devuelven.
import { randomBytes } from "node:crypto";
import type { FastifyPluginAsync, FastifyReply, FastifyRequest } from "fastify";
import {
  type AccountDirectory,
  type AccountRole,
  DirectoryUnavailableError,
  UsernameTakenError,
} from "./account-directory.js";
import {
  ADMIN_LIMIT_PER_ADMIN,
  BOT_ACCOUNTS_MAX_COUNT,
  BOT_HASH_CONCURRENCY,
  BOT_NAME_ATTEMPTS,
  describeWindow,
} from "./limits.js";
import type { PasswordService } from "./passwords.js";
import type { AttemptLimiter } from "./rate-limit.js";
import type { TokenService, TokenVerifier } from "./tokens.js";

export interface AdminDeps {
  /** Solo se usa `register`. */
  accounts: Pick<AccountDirectory, "register">;
  /** Solo se usa `hash`. */
  passwords: Pick<PasswordService, "hash">;
  tokens: TokenService;
  /** Verifica el token con la clave pública derivada de la privada de la API. */
  verifier: TokenVerifier;
  /** Peticiones por administrador (clave: `player_id`). */
  adminLimiter: AttemptLimiter;
}

const DEFAULT_PREFIX = "bot";
/** `<prefijo>-<8 hex>` debe caber en 32 caracteres: 32 - 9 = 23. */
const MAX_PREFIX_LENGTH = 23;

const MESSAGES = {
  unauthorized: "Hace falta un token válido: Authorization: Bearer <token>",
  forbidden: "Esta operación es solo para administradores",
  invalidBody:
    "La solicitud no es válida: count debe ser un entero de 1 a 100 y prefix, si se indica, de 1 a 23 caracteres A-Z a-z 0-9 _ . -",
  unavailable:
    "No se pudieron crear todas las cuentas porque el servicio de cuentas no está disponible. Las ya creadas no se devuelven.",
  tooMany:
    "Demasiadas solicitudes. Espera un momento antes de volver a intentarlo.",
  invalidRequest: "La solicitud no es válida",
  internal:
    "Error interno al crear las cuentas. Las ya creadas no se devuelven.",
  internalPlain: "Error interno",
} as const;

const errorSchema = {
  type: "object",
  properties: { error: { type: "string" } },
  required: ["error"],
  additionalProperties: false,
} as const;

const partialSchema = {
  type: "object",
  properties: {
    error: { type: "string" },
    created: { type: "integer", minimum: 0 },
  },
  required: ["error", "created"],
  additionalProperties: false,
} as const;

const botAccountsSchema = {
  summary: "Crear cuentas de bots",
  description: `Solo con token de administrador. Crea \`count\` cuentas de jugador (rol \`player\`) llamadas \`<prefix>-<8 hex aleatorios>\`, cada una con una contraseña aleatoria que no se devuelve ni se registra, y devuelve un token por cuenta (mismos campos y vigencia que el acceso). No pasa por el límite público de registro; su límite por instancia (429) es de ${String(ADMIN_LIMIT_PER_ADMIN.limit)} solicitudes ${describeWindow(ADMIN_LIMIT_PER_ADMIN.windowMs)} por administrador. Los hashes Argon2id se calculan con concurrencia ${String(BOT_HASH_CONCURRENCY)}: con \`count\` 100 la respuesta puede tardar varios segundos. Si la persistencia falla a mitad, responde 503 con \`created\` (cuántas cuentas llegaron a crearse) y **sin tokens**: esas cuentas quedan huérfanas, pero no estorban (los nombres son aleatorios).`,
  tags: ["admin"],
  security: [{ bearerAuth: [] }],
  body: {
    type: "object",
    properties: {
      count: {
        type: "integer",
        minimum: 1,
        maximum: BOT_ACCOUNTS_MAX_COUNT,
        description: "Número de cuentas a crear.",
      },
      prefix: {
        type: "string",
        pattern: `^[A-Za-z0-9_.-]{1,${String(MAX_PREFIX_LENGTH)}}$`,
        description: `Prefijo del nombre; por defecto \`${DEFAULT_PREFIX}\`.`,
      },
    },
    required: ["count"],
    additionalProperties: false,
  },
  response: {
    201: {
      description: "Cuentas creadas",
      type: "object",
      properties: {
        accounts: {
          type: "array",
          items: {
            type: "object",
            properties: {
              player_id: { type: "string", format: "uuid" },
              username: { type: "string" },
              token: { type: "string" },
              expires_at: {
                type: "string",
                format: "date-time",
                description: "ISO 8601 en UTC",
              },
            },
            required: ["player_id", "username", "token", "expires_at"],
            additionalProperties: false,
          },
        },
      },
      required: ["accounts"],
      additionalProperties: false,
    },
    400: { description: "Datos no válidos", ...errorSchema },
    401: { description: "Falta el token o no es válido", ...errorSchema },
    403: { description: "El token no es de un administrador", ...errorSchema },
    429: { description: "Demasiadas solicitudes", ...errorSchema },
    413: { description: "Cuerpo demasiado grande", ...errorSchema },
    500: {
      description:
        "Error interno a mitad; `created` cuenta las cuentas ya creadas",
      ...partialSchema,
    },
    503: {
      description:
        "La persistencia no estuvo disponible a mitad; `created` cuenta las cuentas ya creadas",
      ...partialSchema,
    },
  },
} as const;

interface BotAccount {
  player_id: string;
  username: string;
  token: string;
  expires_at: string;
}

function bearerToken(req: FastifyRequest): string | undefined {
  const match = /^Bearer +(\S+)$/i.exec(req.headers.authorization ?? "");
  return match?.[1];
}

function botName(prefix: string): string {
  return `${prefix}-${randomBytes(4).toString("hex")}`;
}

/** Contraseña aleatoria de 32 caracteres (192 bits), dentro del rango de 8 a 128. */
function randomPassword(): string {
  return randomBytes(24).toString("base64url");
}

export const adminRoutes: FastifyPluginAsync<AdminDeps> = (app, deps) => {
  app.setErrorHandler(
    (
      err: Error & { validation?: readonly unknown[]; statusCode?: number },
      req,
      reply,
    ) => {
      if (err.validation)
        return reply.code(400).send({ error: MESSAGES.invalidBody });
      const status = err.statusCode;
      if (status !== undefined && status >= 400 && status < 500) {
        return reply.code(status).send({ error: MESSAGES.invalidRequest });
      }
      req.log.error({ err }, "unexpected error");
      return reply.code(500).send({ error: MESSAGES.internalPlain });
    },
  );

  // Primero el token y después el límite, con el administrador como clave:
  // un token inválido no consume cupo de nadie.
  app.addHook("onRequest", async (req: FastifyRequest, reply: FastifyReply) => {
    const token = bearerToken(req);
    const player =
      token === undefined ? null : await deps.verifier.verify(token);
    if (!player) {
      return reply
        .code(401)
        .header("WWW-Authenticate", 'Bearer realm="hydrovalley"')
        .send({ error: MESSAGES.unauthorized });
    }
    if (player.role !== "admin")
      return reply.code(403).send({ error: MESSAGES.forbidden });
    const attempt = deps.adminLimiter.hit(player.playerId);
    if (!attempt.allowed) {
      return reply
        .code(429)
        .header("Retry-After", String(attempt.retryAfterSeconds))
        .send({ error: MESSAGES.tooMany });
    }
  });

  /**
   * Crea una cuenta. Si el nombre ya existe, prueba otro (hasta
   * `BOT_NAME_ATTEMPTS`). `Register` no es idempotente: ante cualquier otro
   * error no se reintenta y el error sube.
   */
  async function createAccount(
    prefix: string,
    onRegistered: () => void,
  ): Promise<BotAccount> {
    const passwordHash = await deps.passwords.hash(randomPassword());
    for (let attempt = 1; ; attempt++) {
      const username = botName(prefix);
      let registered: { playerId: string; role: AccountRole };
      try {
        registered = await deps.accounts.register(username, passwordHash);
      } catch (err) {
        if (
          !(err instanceof UsernameTakenError) ||
          attempt >= BOT_NAME_ATTEMPTS
        )
          throw err;
        continue;
      }
      // La cuenta ya existe: se cuenta antes de firmar, para que `created`
      // refleje lo persistido aunque la firma falle.
      onRegistered();
      const { token, expiresAt } = await deps.tokens.issue({
        playerId: registered.playerId,
        username,
        role: registered.role,
      });
      return {
        player_id: registered.playerId,
        username,
        token,
        expires_at: expiresAt.toISOString(),
      };
    }
  }

  app.post<{ Body: { count: number; prefix?: string } }>(
    "/api/admin/bot-accounts",
    { schema: botAccountsSchema },
    async (req, reply) => {
      const { count, prefix = DEFAULT_PREFIX } = req.body;
      const accounts: BotAccount[] = [];
      let created = 0;
      let failure: unknown;

      // Reserva de trabajo: cada trabajador toma la siguiente cuenta mientras
      // no haya fallado nadie (fail fast). Las que ya iban en curso terminan.
      let next = 0;
      const worker = async () => {
        while (failure === undefined && next < count) {
          next++;
          try {
            accounts.push(
              await createAccount(prefix, () => {
                created++;
              }),
            );
          } catch (err) {
            failure ??= err;
          }
        }
      };
      await Promise.all(
        Array.from({ length: Math.min(BOT_HASH_CONCURRENCY, count) }, () =>
          worker(),
        ),
      );

      if (failure !== undefined) {
        // Solo la indisponibilidad de la persistencia es un 503; hash, firma,
        // nombres agotados o cualquier otro fallo es un error nuestro (500).
        if (failure instanceof DirectoryUnavailableError) {
          req.log.warn(
            { err: failure.message, created },
            "bot provisioning failed",
          );
          return reply.code(503).send({ error: MESSAGES.unavailable, created });
        }
        req.log.error({ err: failure, created }, "bot provisioning failed");
        return reply.code(500).send({ error: MESSAGES.internal, created });
      }
      return reply.code(201).send({ accounts });
    },
  );

  return Promise.resolve();
};

// Rutas de consulta con sesión: ranking (RF-10, RF-11) y reporte de las 3R
// (RF-12). El nombre de las rutas es el del documento inicial (`GET
// /api/ranking`, `GET /api/report`). Los enteros de 64 bits viajan como texto
// (regla 7 de CLAUDE.md); la API solo completa los nombres del ranking, el
// orden y los totales los calculan el simulador y la persistencia.
import type { FastifyPluginAsync, FastifyReply, FastifyRequest } from "fastify";
import type { AccountDirectory } from "./account-directory.js";
import { describeWindow, READ_LIMIT_PER_IP } from "./limits.js";
import type { RankingSource } from "./ranking-source.js";
import type { AttemptLimiter } from "./rate-limit.js";
import type { ReportQuery, ReportSource } from "./report-source.js";
import {
  SourceInvalidRequestError,
  SourceNotFoundError,
  SourceUnavailableError,
} from "./source-errors.js";
import type { TokenVerifier, VerifiedToken } from "./tokens.js";

export interface ReadDeps {
  ranking: RankingSource;
  reports: ReportSource;
  /** Solo se usa `getUsernames`, para completar el ranking. */
  accounts: Pick<AccountDirectory, "getUsernames">;
  /** Verifica el token con la clave pública derivada de la privada de la API. */
  tokens: TokenVerifier;
  /** Peticiones por IP a estas rutas. */
  readLimiter: AttemptLimiter;
}

declare module "fastify" {
  interface FastifyRequest {
    /** Jugador del token; lo fija el hook de autenticación de `readRoutes`. */
    player: VerifiedToken | null;
  }
}

/** Entradas del ranking si no se indica `limit`. */
export const DEFAULT_RANKING_LIMIT = 20;
/** Máximo de `limit`. */
export const MAX_RANKING_LIMIT = 100;
const MAX_U64 = 18_446_744_073_709_551_615n;

const MESSAGES = {
  unauthorized: "Hace falta un token válido: Authorization: Bearer <token>",
  invalidQuery: "Los parámetros de la consulta no son válidos",
  invalidPeriod: "El periodo no es válido: from_tick no puede ser mayor que to_tick",
  notFound: "Todavía no hay datos para este reporte",
  unavailable:
    "El servicio no está disponible en este momento. Inténtalo de nuevo en unos minutos.",
  tooMany: "Demasiadas consultas. Espera un momento antes de volver a intentarlo.",
  internal: "Error interno",
} as const;

const errorSchema = {
  type: "object",
  properties: { error: { type: "string" } },
  required: ["error"],
  additionalProperties: false,
} as const;

const u64 = (description: string) =>
  ({
    type: "string",
    pattern: "^[0-9]+$",
    description: `${description} (entero de 64 bits sin signo, como texto)`,
  }) as const;

const rateLimitDescription = `Límite por instancia (429): ${String(READ_LIMIT_PER_IP.limit)} consultas ${describeWindow(READ_LIMIT_PER_IP.windowMs)} por IP, compartido por el ranking y el reporte.`;
const security = [{ bearerAuth: [] }];
const commonErrors = {
  401: { description: "Falta el token o no es válido", ...errorSchema },
  429: { description: "Demasiadas consultas", ...errorSchema },
  500: { description: "Error interno", ...errorSchema },
  503: {
    description: "El servicio de origen no está disponible",
    ...errorSchema,
  },
} as const;

const rankingSchema = {
  summary: "Ranking global por hojas ganadas",
  description: `Ordenado de mayor a menor; los empatados comparten posición y la siguiente salta (1, 2, 2, 4). Se recalcula en cada llamada sin pausar el juego. Requiere el token del acceso. Si no se puede resolver el nombre de un jugador, se muestra \`jugador-\` y los 8 primeros caracteres de su identificador. ${rateLimitDescription}`,
  tags: ["ranking"],
  security,
  querystring: {
    type: "object",
    properties: {
      limit: {
        type: "string",
        pattern: "^[1-9][0-9]{0,2}$",
        description: `Entradas a devolver, de 1 a ${String(MAX_RANKING_LIMIT)} (por defecto ${String(DEFAULT_RANKING_LIMIT)}).`,
      },
    },
    additionalProperties: false,
  },
  response: {
    200: {
      description: "Ranking",
      type: "object",
      properties: {
        tick: u64("Tick en el que se calculó el ranking"),
        entries: {
          type: "array",
          items: {
            type: "object",
            properties: {
              rank: { type: "integer", minimum: 1 },
              player_id: { type: "string", format: "uuid" },
              username: { type: "string" },
              leaves_earned: u64("Hojas ganadas"),
            },
            required: ["rank", "player_id", "username", "leaves_earned"],
            additionalProperties: false,
          },
        },
      },
      required: ["tick", "entries"],
      additionalProperties: false,
    },
    400: { description: "Parámetros no válidos", ...errorSchema },
    ...commonErrors,
  },
} as const;

const reportSchema = {
  summary: "Reporte de las 3R",
  description: `Totales de agua reducida (goteo), reutilizada (aspersores) y reciclada (planta de tratamiento), más los saldos de RF-27, de la época actual del valle. \`scope=me\` (por defecto) toma al jugador del token; \`scope=global\` suma a todos. Sin periodo, va desde el inicio de la época hasta la última instantánea; con \`from_tick\` y \`to_tick\`, es la diferencia entre las instantáneas en o antes de esos ticks. Sale de las instantáneas, que se guardan cada 5 s: puede ir hasta 5 s por detrás del valle. Requiere el token del acceso. ${rateLimitDescription}`,
  tags: ["reports"],
  security,
  querystring: {
    type: "object",
    properties: {
      scope: { type: "string", enum: ["me", "global"], default: "me" },
      from_tick: {
        type: "string",
        pattern: "^[0-9]{1,20}$",
        description: "Inicio del periodo (tick). Sin valor: inicio de la época.",
      },
      to_tick: {
        type: "string",
        pattern: "^[0-9]{1,20}$",
        description: "Fin del periodo (tick). Sin valor: la última instantánea.",
      },
    },
    additionalProperties: false,
  },
  response: {
    200: {
      description: "Reporte",
      type: "object",
      properties: {
        scope: { type: "string", enum: ["me", "global"] },
        player_id: {
          type: "string",
          format: "uuid",
          description: "Solo en `scope=me`.",
        },
        epoch: { type: "integer", minimum: 0 },
        from_tick: u64("Tick de la instantánea inicial usada (0: inicio de la época)"),
        to_tick: u64("Tick de la instantánea final usada"),
        totals: {
          type: "object",
          properties: {
            water_reduced: u64("Agua reducida (Reducir)"),
            water_reused: u64("Agua reutilizada (Reutilizar)"),
            water_recycled: u64("Agua reciclada (Reciclar)"),
            water_refilled: u64("Agua recargada en el depósito"),
            water_spent: u64("Agua gastada en gotas"),
            leaves_earned: u64("Hojas ganadas"),
            leaves_spent: u64("Hojas gastadas"),
          },
          required: [
            "water_reduced",
            "water_reused",
            "water_recycled",
            "water_refilled",
            "water_spent",
            "leaves_earned",
            "leaves_spent",
          ],
          additionalProperties: false,
        },
      },
      required: ["scope", "epoch", "from_tick", "to_tick", "totals"],
      additionalProperties: false,
    },
    400: { description: "Parámetros no válidos", ...errorSchema },
    404: {
      description: "Todavía no hay instantáneas o la cuenta no existe",
      ...errorSchema,
    },
    ...commonErrors,
  },
} as const;

function bearerToken(req: FastifyRequest): string | undefined {
  const match = /^Bearer +(\S+)$/i.exec(req.headers.authorization ?? "");
  return match?.[1];
}

/** Primeros 8 caracteres del identificador, para el nombre de relleno. */
function fallbackName(playerId: string): string {
  return `jugador-${playerId.slice(0, 8)}`;
}

export const readRoutes: FastifyPluginAsync<ReadDeps> = (app, deps) => {
  app.decorateRequest("player", null);

  app.setErrorHandler(
    (err: Error & { validation?: readonly unknown[]; statusCode?: number }, req, reply) => {
      if (err.validation) return reply.code(400).send({ error: MESSAGES.invalidQuery });
      if (err instanceof SourceInvalidRequestError) {
        return reply.code(400).send({ error: MESSAGES.invalidQuery });
      }
      if (err instanceof SourceNotFoundError) {
        return reply.code(404).send({ error: MESSAGES.notFound });
      }
      if (err instanceof SourceUnavailableError) {
        req.log.warn({ err: err.message }, "ranking or report source unavailable");
        return reply.code(503).send({ error: MESSAGES.unavailable });
      }
      const status = err.statusCode;
      if (status !== undefined && status >= 400 && status < 500) {
        return reply.code(status).send({ error: MESSAGES.invalidQuery });
      }
      req.log.error({ err }, "unexpected error");
      return reply.code(500).send({ error: MESSAGES.internal });
    },
  );

  // Primero el límite por IP (barato) y después el token: así un cliente sin
  // sesión tampoco puede forzar verificaciones de firma sin límite.
  app.addHook("onRequest", (req, reply, done) => {
    const attempt = deps.readLimiter.hit(req.ip);
    if (!attempt.allowed) {
      void reply
        .code(429)
        .header("Retry-After", String(attempt.retryAfterSeconds))
        .send({ error: MESSAGES.tooMany });
      return;
    }
    done();
  });

  app.addHook("onRequest", async (req: FastifyRequest, reply: FastifyReply) => {
    const token = bearerToken(req);
    const player = token === undefined ? null : await deps.tokens.verify(token);
    if (!player) {
      return reply
        .code(401)
        .header("WWW-Authenticate", 'Bearer realm="hydrovalley"')
        .send({ error: MESSAGES.unauthorized });
    }
    req.player = player;
  });

  app.get<{ Querystring: { limit?: string } }>(
    "/api/ranking",
    { schema: rankingSchema },
    async (req, reply) => {
      const limit = req.query.limit === undefined ? DEFAULT_RANKING_LIMIT : Number(req.query.limit);
      if (limit > MAX_RANKING_LIMIT) return reply.code(400).send({ error: MESSAGES.invalidQuery });

      const { tick, entries } = await deps.ranking.getRanking(limit);

      // Si no se pueden resolver los nombres, el ranking sale igual: es lo
      // que el jugador pidió y el nombre es un dato cosmético.
      let names: ReadonlyMap<string, string> = new Map();
      if (entries.length > 0) {
        try {
          names = await deps.accounts.getUsernames([...new Set(entries.map((e) => e.playerId))]);
        } catch (err) {
          req.log.warn({ err: String(err) }, "username lookup failed; ranking without names");
        }
      }
      return {
        tick: tick.toString(),
        entries: entries.map((e) => ({
          rank: e.rank,
          player_id: e.playerId,
          username: names.get(e.playerId) ?? fallbackName(e.playerId),
          leaves_earned: e.leavesEarned.toString(),
        })),
      };
    },
  );

  app.get<{
    Querystring: {
      scope?: "me" | "global";
      from_tick?: string;
      to_tick?: string;
    };
  }>("/api/report", { schema: reportSchema }, async (req, reply) => {
    const scope = req.query.scope ?? "me";
    const fromTick = req.query.from_tick === undefined ? undefined : BigInt(req.query.from_tick);
    const toTick = req.query.to_tick === undefined ? undefined : BigInt(req.query.to_tick);
    if ((fromTick ?? 0n) > MAX_U64 || (toTick ?? 0n) > MAX_U64) {
      return reply.code(400).send({ error: MESSAGES.invalidQuery });
    }
    if (fromTick !== undefined && toTick !== undefined && fromTick > toTick) {
      return reply.code(400).send({ error: MESSAGES.invalidPeriod });
    }

    const query: ReportQuery = {
      ...(scope === "me" && req.player ? { playerId: req.player.playerId } : {}),
      ...(fromTick === undefined ? {} : { fromTick }),
      ...(toTick === undefined ? {} : { toTick }),
    };
    const report = await deps.reports.getReport(query);
    const t = report.totals;
    return {
      scope,
      ...(scope === "me" ? { player_id: report.playerId } : {}),
      epoch: report.epoch,
      from_tick: report.fromTick.toString(),
      to_tick: report.toTick.toString(),
      totals: {
        water_reduced: t.waterReduced.toString(),
        water_reused: t.waterReused.toString(),
        water_recycled: t.waterRecycled.toString(),
        water_refilled: t.waterRefilled.toString(),
        water_spent: t.waterSpent.toString(),
        leaves_earned: t.leavesEarned.toString(),
        leaves_spent: t.leavesSpent.toString(),
      },
    };
  });

  return Promise.resolve();
};

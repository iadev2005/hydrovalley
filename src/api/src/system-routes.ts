// `GET /api/system`: estado de los servicios y cifras del hub para el panel
// del sistema (RNF-08). Solo lectura, sin token y con límite por IP. Responde
// 200 aunque algún servicio esté caído: eso es justo lo que muestra.
import type { FastifyPluginAsync } from "fastify";
import { describeWindow, SYSTEM_LIMIT_PER_IP } from "./limits.js";
import type { AttemptLimiter } from "./rate-limit.js";
import type { SystemStatus } from "./system-status.js";

export interface SystemDeps {
  status: SystemStatus;
  /** Peticiones por IP a esta ruta. */
  limiter: AttemptLimiter;
}

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

const count = (description: string) => ({ type: "integer", minimum: 0, description }) as const;
const measure = (description: string) => ({ type: "number", minimum: 0, description }) as const;

const hubSchema = {
  type: ["object", "null"],
  description: "Cifras del hub (`GET /stats` interno); `null` si el hub no respondió.",
  properties: {
    tick: u64("Último tick devuelto por `Step`"),
    paused: { type: "boolean", description: "El valle está en pausa" },
    breaker: { type: "string", enum: ["closed", "open", "half_open"] },
    queue: count("Comandos en la cola del tick"),
    sessions: count("Conexiones WebSocket abiertas"),
    goroutines: count("Goroutines del proceso del hub"),
    actions_per_second: {
      type: "object",
      properties: {
        admitted: measure("Acciones admitidas por segundo"),
        applied: measure("Acciones aplicadas por segundo"),
        window_seconds: count("Ventana del promedio, en segundos"),
      },
      required: ["admitted", "applied", "window_seconds"],
      additionalProperties: false,
    },
    latency_ms: {
      type: "object",
      properties: {
        p50: measure("Mediana, de la admisión al resultado final"),
        p95: measure("Percentil 95"),
        samples: count("Muestras usadas"),
      },
      required: ["p50", "p95", "samples"],
      additionalProperties: false,
    },
    step_ms: {
      type: "object",
      properties: {
        last: measure("Duración del último `Step`"),
        p95: measure("Percentil 95 de los últimos 600 `Step`"),
        failures_total: count("`Step` fallidos desde que arrancó el hub"),
      },
      required: ["last", "p95", "failures_total"],
      additionalProperties: false,
    },
    humidity_total: u64("Humedad total del valle"),
    humidity_contributed: u64("Humedad aportada por las gotas"),
    accepted_drops: u64("Gotas aceptadas"),
    active_fires: count("Casillas que arden"),
    boot_id: { type: "string", description: "`boot_id` del último `Step`" },
    restored_tick: u64("Tick del que Rust se recuperó en el arranque que informa `boot_id`"),
    generated_at: {
      type: "string",
      description: "Hora de la foto del hub, ISO 8601 en UTC",
    },
  },
  required: [
    "tick",
    "paused",
    "breaker",
    "queue",
    "sessions",
    "goroutines",
    "actions_per_second",
    "latency_ms",
    "step_ms",
    "humidity_total",
    "humidity_contributed",
    "accepted_drops",
    "active_fires",
    "boot_id",
    "restored_tick",
    "generated_at",
  ],
  additionalProperties: false,
} as const;

const systemSchema = {
  summary: "Estado del sistema",
  description: `Estado de nginx, la API, el hub, el simulador y la persistencia, más las cifras del hub. Sin token. Responde siempre 200: un servicio que no contesta sale con \`status: "down"\` y un \`detail\` corto (\`timeout\`, \`unavailable\`, \`not_serving\`, \`http_503\`…), y si el hub cae, \`hub\` es \`null\`. Cada consulta interna tiene un plazo de 1 s y el resultado se guarda 1 s, así que muchos visores no multiplican las llamadas. Límite por instancia (429): ${String(SYSTEM_LIMIT_PER_IP.limit)} consultas ${describeWindow(SYSTEM_LIMIT_PER_IP.windowMs)} por IP.`,
  tags: ["system"],
  response: {
    200: {
      description: "Estado del sistema",
      type: "object",
      properties: {
        generated_at: {
          type: "string",
          description: "Hora de la agregación, ISO 8601 en UTC",
        },
        services: {
          type: "array",
          items: {
            type: "object",
            properties: {
              name: {
                type: "string",
                enum: ["nginx", "api", "hub", "world", "persistence"],
              },
              status: { type: "string", enum: ["up", "down"] },
              detail: {
                type: "string",
                description: "Motivo corto, solo si está caído",
              },
            },
            required: ["name", "status"],
            additionalProperties: false,
          },
        },
        hub: hubSchema,
      },
      required: ["generated_at", "services", "hub"],
      additionalProperties: false,
    },
    429: { description: "Demasiadas consultas", ...errorSchema },
  },
} as const;

export const systemRoutes: FastifyPluginAsync<SystemDeps> = (app, deps) => {
  app.get("/api/system", { schema: systemSchema }, (req, reply) => {
    const attempt = deps.limiter.hit(req.ip);
    if (!attempt.allowed) {
      return reply.code(429).header("Retry-After", String(attempt.retryAfterSeconds)).send({
        error: "Demasiadas consultas. Espera un momento antes de volver a intentarlo.",
      });
    }
    return deps.status.get();
  });
  return Promise.resolve();
};

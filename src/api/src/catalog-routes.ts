// `GET /api/catalog`: instalaciones, mejoras y cifras del juego (D-01) para que
// el cliente muestre precios y textos. Solo lectura, sin token (son datos del
// juego, no de un jugador) y con límite por IP. Los enteros de 64 bits viajan
// como texto (regla 7 de CLAUDE.md). La copia en memoria y el servido de la
// copia vencida durante una caída están en catalog-cache.ts.
import type { FastifyPluginAsync } from "fastify";
import type { CachedCatalog } from "./catalog-cache.js";
import { CATALOG_CACHE_TTL_SECONDS, CATALOG_LIMIT_PER_IP, describeWindow } from "./limits.js";
import type { AttemptLimiter } from "./rate-limit.js";
import { SourceUnavailableError } from "./source-errors.js";

export interface CatalogDeps {
  catalog: CachedCatalog;
  /** Peticiones por IP a esta ruta. */
  limiter: AttemptLimiter;
}

const MESSAGES = {
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

const u32 = (description: string) =>
  ({ type: "integer", minimum: 0, description }) as const;

/** Objeto cerrado en el que todas las propiedades son obligatorias. */
const group = (description: string, properties: Record<string, object>) =>
  ({
    type: "object",
    description,
    properties,
    required: Object.keys(properties),
    additionalProperties: false,
  }) as const;

const catalogSchema = group("Catálogo del juego", {
  catalog_version: { type: "string", description: "Hash del contenido del catálogo" },
  drop: group("Gota", {
    water_cost: u64("Agua que se toma del depósito por gota, en litros (modo demo: gratis)"),
    humidity: u64("Humedad que suma a la casilla fuera del modo demo"),
    demo_humidity: u64("Humedad que suma una gota en modo demo"),
  }),
  tank: group("Depósito", {
    capacity: u64("Capacidad inicial, en litros"),
    refill_amount: u64("Recarga por intervalo, en litros"),
    refill_interval_seconds: u32("Segundos entre recargas"),
  }),
  growth: group("Crecimiento", {
    humidity_per_stage: u64("Humedad que hace falta, y se gasta, para subir una etapa"),
    leaves_grass: u64("Hojas al llegar a pasto"),
    leaves_shrub: u64("Hojas al llegar a arbusto"),
    leaves_flowers: u64("Hojas al llegar a flores"),
    leaves_tree: u64("Hojas al llegar a árbol"),
    tree_max_humidity: u64("Humedad máxima de un árbol"),
  }),
  evaporation: group("Evaporación", {
    humidity_per_second: u64("Humedad que pierde cada casilla por segundo"),
    wither_after_ticks: u32("Ticks con humedad 0 antes de que la casilla se marchite una etapa"),
  }),
  fire: group("Incendios", {
    drops_to_extinguish: u32("Gotas necesarias para apagar un incendio"),
    extinguish_bonus_leaves: u64("Hojas para quien suelta la última gota"),
    burn_out_ticks: u32("Ticks hasta que un incendio sin atender se consume"),
    ignition_chance_ppm: u32("Probabilidad por tick de que arda una casilla seca con planta, en partes por millón"),
    propagation_chance_ppm: u32("Probabilidad por tick de que un fuego salte a una casilla vecina elegible, en partes por millón"),
  }),
  sprinkler: group("Aspersor de agua gris (Reutilizar)", {
    price: u64("Precio del primero, en hojas"),
    area_side: u32("Lado del área cuadrada que cubre, en casillas"),
    humidity: u64("Humedad que suma a cada casilla por intervalo"),
    interval_seconds: u32("Segundos entre riegos"),
  }),
  drip: group("Riego por goteo (Reducir)", {
    price: u64("Precio del primero, en hojas"),
    area_side: u32("Lado del área cuadrada que cubre, en casillas"),
    evaporation_divisor: u32("La evaporación en su área se divide entre este valor"),
  }),
  treatment_plant: group("Planta de tratamiento (Reciclar)", {
    price: u64("Precio de la primera, en hojas"),
    refill_amount: u64("Recarga extra del depósito de su dueño por intervalo, en litros"),
    interval_seconds: u32("Segundos entre recargas"),
  }),
  installation_price_increase_percent: u32(
    "Aumento de precio, en porcentaje, por cada compra del mismo tipo de instalación",
  ),
  bigger_drop: group("Mejora: gota más grande", {
    price: u64("Precio, en hojas"),
    area_side: u32("Lado del área cuadrada que cubre una gota, en casillas"),
  }),
  bigger_tank: group("Mejora: depósito mayor", {
    price: u64("Precio, en hojas"),
    extra_capacity: u64("Capacidad extra del depósito, en litros"),
  }),
  offline_installation_seconds: u64(
    "Segundos que las instalaciones siguen trabajando tras la última acción de su dueño",
  ),
});

const routeSchema = {
  summary: "Catálogo del juego",
  description: `Instalaciones (aspersor, goteo, planta de tratamiento), mejoras (gota más grande, depósito mayor) y demás cifras del juego, tal como las sirve la persistencia (\`catalog/catalog.json\`, sección D-01). Sin token. La API guarda una copia ${String(CATALOG_CACHE_TTL_SECONDS)} s (\`Cache-Control: public, max-age=${String(CATALOG_CACHE_TTL_SECONDS)}\`) y \`ETag\` es el hash del catálogo: con \`If-None-Match\` responde \`304\`. Si la persistencia no responde y hay una copia vencida, se sirve esa (con \`Cache-Control: no-cache\`); sin copia, \`503\`. Límite por instancia (429): ${String(CATALOG_LIMIT_PER_IP.limit)} consultas ${describeWindow(CATALOG_LIMIT_PER_IP.windowMs)} por IP.`,
  tags: ["catalog"],
  response: {
    200: {
      ...catalogSchema,
      headers: {
        type: "object",
        properties: {
          "cache-control": { type: "string" },
          etag: { type: "string", description: "Hash del catálogo, entre comillas" },
        },
      },
    },
    304: { description: "El catálogo no cambió (`If-None-Match`)", type: "null" },
    429: { description: "Demasiadas consultas", ...errorSchema },
    500: { description: "Error interno", ...errorSchema },
    503: { description: "La persistencia no está disponible y no hay copia", ...errorSchema },
  },
} as const;

/** `If-None-Match` coincide con el validador (acepta `*`, listas y el prefijo `W/`). */
function matches(header: string | undefined, etag: string): boolean {
  if (header === undefined) return false;
  return header
    .split(",")
    .map((candidate) => candidate.trim().replace(/^W\//, ""))
    .some((candidate) => candidate === "*" || candidate === etag);
}

const snake = (key: string) => key.replace(/[A-Z]/g, (letter) => `_${letter.toLowerCase()}`);

/** Claves en snake_case y enteros de 64 bits como texto. */
function toJson(value: unknown): unknown {
  if (typeof value === "bigint") return value.toString();
  if (typeof value !== "object" || value === null) return value;
  return Object.fromEntries(Object.entries(value).map(([k, v]) => [snake(k), toJson(v)]));
}

export const catalogRoutes: FastifyPluginAsync<CatalogDeps> = (app, deps) => {
  app.setErrorHandler((err, req, reply) => {
    if (err instanceof SourceUnavailableError) {
      req.log.warn({ err: err.message }, "catalog source unavailable and no cached copy");
      return reply.code(503).send({ error: MESSAGES.unavailable });
    }
    req.log.error({ err }, "unexpected error");
    return reply.code(500).send({ error: MESSAGES.internal });
  });

  app.get("/api/catalog", { schema: routeSchema }, async (req, reply) => {
    const attempt = deps.limiter.hit(req.ip);
    if (!attempt.allowed) {
      return reply
        .code(429)
        .header("Retry-After", String(attempt.retryAfterSeconds))
        .send({ error: MESSAGES.tooMany });
    }

    const { entry, stale, error } = await deps.catalog.get();
    if (stale) req.log.warn({ err: String(error) }, "catalog refresh failed; serving the old copy");
    void reply
      .header("ETag", entry.etag)
      .header(
        "Cache-Control",
        // Una copia vencida no debe quedarse en el navegador como si fuera fresca.
        stale ? "no-cache" : `public, max-age=${String(CATALOG_CACHE_TTL_SECONDS)}`,
      );
    if (matches(req.headers["if-none-match"], entry.etag)) return reply.code(304).send();
    return toJson(entry.catalog);
  });
  return Promise.resolve();
};

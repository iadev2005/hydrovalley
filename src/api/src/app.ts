// Montaje de la aplicación Fastify: rutas y documentación OpenAPI (RES-04).
// Se construye sin escuchar para poder probarla con `inject`.
import Fastify, { type FastifyInstance, type FastifyServerOptions } from "fastify";
import swagger from "@fastify/swagger";
import swaggerUi from "@fastify/swagger-ui";
import { type AuthDeps, authRoutes } from "./auth-routes.js";

const healthSchema = {
  summary: "Estado del proceso",
  description: "Responde 200 mientras la API atiende peticiones.",
  tags: ["system"],
  response: {
    200: {
      type: "object",
      properties: { status: { type: "string", const: "ok" } },
      required: ["status"],
      additionalProperties: false,
    },
  },
} as const;

/**
 * Función de confianza para `trustProxy` que acepta exactamente `hops` saltos
 * (el hop 0 es el socket que conectó). Con 1, solo nginx: la IP del cliente
 * es la última entrada de `X-Forwarded-For` y las anteriores, que puede
 * inventar el cliente, se ignoran.
 */
export function trustHops(hops: number): (address: string, hop: number) => boolean {
  return (_address, hop) => hop < hops;
}

export async function buildApp(
  options: FastifyServerOptions = {},
  auth?: AuthDeps,
): Promise<FastifyInstance> {
  // Sin coerción de tipos: un número no se acepta como nombre de usuario.
  const app = Fastify({
    ajv: { customOptions: { coerceTypes: false } },
    ...options,
  });

  await app.register(swagger, {
    openapi: {
      openapi: "3.1.0",
      info: {
        title: "hydrovalley API",
        description:
          "Registro, acceso, catálogo, ranking y reportes. Por ahora expone la salud del servicio, el registro y el acceso.",
        version: "0.1.0",
      },
      tags: [
        { name: "system", description: "Operación del servicio" },
        { name: "auth", description: "Registro y acceso con token firmado" },
      ],
    },
  });
  await app.register(swaggerUi, { routePrefix: "/api/docs" });

  app.get("/api/health", { schema: healthSchema }, () => ({
    status: "ok" as const,
  }));

  if (auth) {
    await app.register(authRoutes, auth);
  }

  // TODO: catálogo, ranking y reportes.

  return app;
}

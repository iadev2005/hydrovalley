// Montaje de la aplicación Fastify: rutas y documentación OpenAPI (RES-04).
// Se construye sin escuchar para poder probarla con `inject`.
import Fastify, { type FastifyInstance, type FastifyServerOptions } from "fastify";
import swagger from "@fastify/swagger";
import swaggerUi from "@fastify/swagger-ui";

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

export async function buildApp(
  options: FastifyServerOptions = {},
): Promise<FastifyInstance> {
  const app = Fastify(options);

  await app.register(swagger, {
    openapi: {
      openapi: "3.1.0",
      info: {
        title: "hydrovalley API",
        description:
          "Registro, acceso, catálogo, ranking y reportes. Por ahora solo expone la salud del servicio.",
        version: "0.1.0",
      },
      tags: [{ name: "system", description: "Operación del servicio" }],
    },
  });
  await app.register(swaggerUi, { routePrefix: "/api/docs" });

  app.get("/api/health", { schema: healthSchema }, () => ({ status: "ok" as const }));

  // TODO(PR de acceso): registro y acceso con AccountService por gRPC
  // (PERSISTENCE_ADDR), token Ed25519 y el resto de rutas REST.

  return app;
}

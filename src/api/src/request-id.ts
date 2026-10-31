// Identificador de petición de punta a punta (RNF-07).
//
// nginx pone `X-Request-Id` (el que traía el cliente, si es válido, o su
// `$request_id`). Aquí se valida, se usa como id de petición de Fastify (todas
// las líneas de registro de esa petición lo llevan), se devuelve en la
// respuesta y se reenvía como metadato gRPC `x-request-id` en cada llamada a
// persistencia y al simulador. El id viaja por un AsyncLocalStorage para que
// los adaptadores gRPC lo lean sin cambiar las interfaces de las rutas.
import { randomUUID } from "node:crypto";
import { AsyncLocalStorage } from "node:async_hooks";
import type { Interceptor } from "@connectrpc/connect";
import { createGrpcTransport } from "@connectrpc/connect-node";
import type { FastifyInstance } from "fastify";

export const REQUEST_ID_HEADER = "x-request-id";

const MAX_LENGTH = 64;
const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const HEX32 = /^[0-9a-f]{32}$/i;

/** UUID o 32 caracteres hexadecimales, de 64 caracteres como máximo. */
export function isValidRequestId(value: unknown): value is string {
  return (
    typeof value === "string" &&
    value.length <= MAX_LENGTH &&
    (UUID.test(value) || HEX32.test(value))
  );
}

/** El id recibido si es válido; si falta o no lo es, un UUID nuevo. */
export function resolveRequestId(value: unknown): string {
  return isValidRequestId(value) ? value : randomUUID();
}

const store = new AsyncLocalStorage<string>();

/** Id de la petición HTTP en curso, o `undefined` fuera de una petición. */
export function currentRequestId(): string | undefined {
  return store.getStore();
}

/** Añade `x-request-id` a las llamadas gRPC hechas durante una petición. */
export const requestIdInterceptor: Interceptor = (next) => (req) => {
  const id = store.getStore();
  if (id !== undefined) req.header.set(REQUEST_ID_HEADER, id);
  return next(req);
};

/** Transporte gRPC sin cifrar con la propagación del id: el único sitio donde se crea. */
export function createGrpcTransportFor(address: string) {
  return createGrpcTransport({
    baseUrl: `http://${address}`,
    interceptors: [requestIdInterceptor],
  });
}

/** Opciones de Fastify: el id de la petición sale de la cabecera validada. */
export const requestIdOptions = {
  requestIdHeader: false,
  genReqId: (req: { headers: Record<string, unknown> }) =>
    resolveRequestId(req.headers[REQUEST_ID_HEADER]),
} as const;

/** Devuelve el id en la respuesta y lo deja en el contexto de la petición. */
export function registerRequestId(app: FastifyInstance): void {
  app.addHook("onRequest", (req, reply, done) => {
    void reply.header(REQUEST_ID_HEADER, req.id);
    store.run(req.id, done);
  });
}

// Errores de dominio de las fuentes de ranking y reportes. Las rutas dependen
// de estas clases, no de los códigos de gRPC.
import { Code, ConnectError } from "@connectrpc/connect";
import { TRANSIENT_CODES } from "./grpc-retry.js";

/** UNAVAILABLE o plazo agotado: el servicio no respondió a tiempo. */
export class SourceUnavailableError extends Error {
  constructor(message = "source unavailable") {
    super(message);
    this.name = "SourceUnavailableError";
  }
}

/** NOT_FOUND: no hay datos para lo pedido (sin instantáneas o sin cuenta). */
export class SourceNotFoundError extends Error {
  constructor(message = "source has no data for the request") {
    super(message);
    this.name = "SourceNotFoundError";
  }
}

/** INVALID_ARGUMENT: el servicio rechazó los parámetros de la consulta. */
export class SourceInvalidRequestError extends Error {
  constructor(message = "request rejected by the source") {
    super(message);
    this.name = "SourceInvalidRequestError";
  }
}

/** Traduce los códigos de gRPC que la API distingue; el resto se propaga. */
export function translateGrpcError(err: unknown, service: string): Error {
  const error = ConnectError.from(err);
  if (TRANSIENT_CODES.has(error.code)) {
    return new SourceUnavailableError(`${service} call failed: ${Code[error.code]}`);
  }
  if (error.code === Code.NotFound) return new SourceNotFoundError();
  if (error.code === Code.InvalidArgument) return new SourceInvalidRequestError();
  return error;
}

"""Servidor gRPC de PersistenceService (SaveSnapshot y LoadSnapshot).

Traduce entre los mensajes del contrato y el dominio, y entre las
excepciones del puerto y los códigos estándar de gRPC. No sabe nada de
PostgreSQL: depende solo del puerto SnapshotRepository.
"""

import logging

import grpc

from hydrovalley.persistence.v1 import persistence_pb2 as pb
from hydrovalley.persistence.v1 import persistence_pb2_grpc as pb_grpc
from persistence.domain import (
    ConcurrentSnapshotChangeError,
    InvalidSnapshotError,
    RepositoryUnavailableError,
    SaveOutcome,
    Snapshot,
    SnapshotConflictError,
)
from persistence.ports import SnapshotRepository

log = logging.getLogger(__name__)

# La columna epoch es INTEGER (diagrama 07); el contrato la declara uint32.
MAX_EPOCH = 2**31 - 1

# catalog_version es el hash del catálogo (64 caracteres en hexadecimal con
# SHA-256); se deja margen para otros formatos. Puede ir vacía mientras no
# exista CatalogService: el simulador la envía así hoy.
MAX_CATALOG_VERSION_LENGTH = 128


def _validate(message: pb.Snapshot) -> str | None:
    """Comprueba la instantánea antes de tocar la base de datos. Devuelve
    el motivo del rechazo o None. tick ya es u64 por el contrato y la
    columna NUMERIC(20,0) admite todo su rango."""
    if message.epoch > MAX_EPOCH:
        return f"epoch mayor que {MAX_EPOCH}"
    if len(message.catalog_version) > MAX_CATALOG_VERSION_LENGTH:
        return f"catalog_version con más de {MAX_CATALOG_VERSION_LENGTH} caracteres"
    if "\x00" in message.catalog_version:
        return "catalog_version contiene el carácter NUL"
    return None


def _status_for(error: Exception) -> grpc.StatusCode:
    """Traduce una excepción del puerto a su código de gRPC. Lo inesperado
    es INTERNAL, nunca UNKNOWN."""
    if isinstance(error, RepositoryUnavailableError):
        return grpc.StatusCode.UNAVAILABLE
    if isinstance(error, SnapshotConflictError):
        return grpc.StatusCode.ALREADY_EXISTS
    if isinstance(error, InvalidSnapshotError):
        return grpc.StatusCode.INVALID_ARGUMENT
    if isinstance(error, ConcurrentSnapshotChangeError):
        return grpc.StatusCode.ABORTED
    return grpc.StatusCode.INTERNAL


class PersistenceServicer(pb_grpc.PersistenceServiceServicer):
    def __init__(self, repository: SnapshotRepository) -> None:
        self._repository = repository

    def SaveSnapshot(self, request: pb.SaveSnapshotRequest, context: grpc.ServicerContext):
        if not request.HasField("snapshot"):
            context.abort(grpc.StatusCode.INVALID_ARGUMENT, "falta snapshot")
        message = request.snapshot
        problem = _validate(message)
        if problem is not None:
            context.abort(grpc.StatusCode.INVALID_ARGUMENT, problem)
        snapshot = Snapshot(
            tick=message.tick,
            epoch=message.epoch,
            catalog_version=message.catalog_version,
            world_state=message.world_state,
        )
        # context.abort lanza su propia excepción: se llama fuera del try
        # para que el except genérico no la capture.
        try:
            outcome = self._repository.save(snapshot)
        except Exception as error:
            code, detail = self._log_failure("SaveSnapshot", error), str(error)
        else:
            if outcome is SaveOutcome.INSERTED:
                self._prune()
            return pb.SaveSnapshotResponse()
        context.abort(code, detail)

    def LoadSnapshot(self, request: pb.LoadSnapshotRequest, context: grpc.ServicerContext):
        try:
            snapshot = self._repository.load_latest()
        except Exception as error:
            code, detail = self._log_failure("LoadSnapshot", error), str(error)
        else:
            if snapshot is None:
                context.abort(grpc.StatusCode.NOT_FOUND, "no hay ninguna instantánea")
            return pb.LoadSnapshotResponse(
                snapshot=pb.Snapshot(
                    tick=snapshot.tick,
                    epoch=snapshot.epoch,
                    catalog_version=snapshot.catalog_version,
                    world_state=snapshot.world_state,
                )
            )
        context.abort(code, detail)

    @staticmethod
    def _log_failure(method: str, error: Exception) -> grpc.StatusCode:
        code = _status_for(error)
        if code is grpc.StatusCode.INTERNAL:
            log.error("%s: error inesperado", method, exc_info=error)
        elif code is not grpc.StatusCode.UNAVAILABLE:
            log.error("%s rechazado (%s): %s", method, code.name, error)
        return code

    def _prune(self) -> None:
        # La poda va en otra transacción y su fallo no invalida el guardado:
        # se registra y se reintenta tras el siguiente guardado nuevo.
        try:
            removed = self._repository.prune()
        except Exception:
            log.exception("no se pudo podar la retención de instantáneas")
            return
        if removed:
            log.info("poda de retención: %d instantáneas borradas", removed)

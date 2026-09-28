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
    RepositoryUnavailableError,
    SaveOutcome,
    Snapshot,
    SnapshotConflictError,
)
from persistence.ports import SnapshotRepository

log = logging.getLogger(__name__)

# La columna epoch es INTEGER (diagrama 07); el contrato la declara uint32.
MAX_EPOCH = 2**31 - 1


class PersistenceServicer(pb_grpc.PersistenceServiceServicer):
    def __init__(self, repository: SnapshotRepository) -> None:
        self._repository = repository

    def SaveSnapshot(self, request: pb.SaveSnapshotRequest, context: grpc.ServicerContext):
        if not request.HasField("snapshot"):
            context.abort(grpc.StatusCode.INVALID_ARGUMENT, "falta snapshot")
        message = request.snapshot
        if message.epoch > MAX_EPOCH:
            context.abort(grpc.StatusCode.INVALID_ARGUMENT, f"epoch mayor que {MAX_EPOCH}")
        snapshot = Snapshot(
            tick=message.tick,
            epoch=message.epoch,
            catalog_version=message.catalog_version,
            world_state=message.world_state,
        )
        try:
            outcome = self._repository.save(snapshot)
        except SnapshotConflictError as error:
            log.error("SaveSnapshot rechazado: %s", error)
            context.abort(grpc.StatusCode.ALREADY_EXISTS, str(error))
        except RepositoryUnavailableError as error:
            context.abort(grpc.StatusCode.UNAVAILABLE, str(error))
        if outcome is SaveOutcome.INSERTED:
            self._prune()
        return pb.SaveSnapshotResponse()

    def LoadSnapshot(self, request: pb.LoadSnapshotRequest, context: grpc.ServicerContext):
        try:
            snapshot = self._repository.load_latest()
        except RepositoryUnavailableError as error:
            context.abort(grpc.StatusCode.UNAVAILABLE, str(error))
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

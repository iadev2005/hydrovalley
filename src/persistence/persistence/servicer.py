"""Servidor gRPC de PersistenceService (SaveSnapshot y LoadSnapshot).

Traduce entre los mensajes del contrato y el dominio, y entre las
excepciones del puerto y los códigos estándar de gRPC. No sabe nada de
PostgreSQL: depende solo del puerto SnapshotRepository.
"""

import logging
import uuid

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
    SnapshotPlayer,
)
from persistence.ports import SnapshotRepository

log = logging.getLogger(__name__)

# La columna epoch es INTEGER (diagrama 07); el contrato la declara uint32.
MAX_EPOCH = 2**31 - 1

# catalog_version es el hash del catálogo (64 caracteres en hexadecimal con
# SHA-256); se deja margen para otros formatos. Puede ir vacía: no se
# contrasta con catalog_entries.
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
    seen: set[uuid.UUID] = set()
    for player in message.players:
        player_id = parse_uuid(player.player_id)
        if player_id is None:
            return "players contiene un player_id que no es un UUID"
        if player_id in seen:
            return "players repite un player_id"
        seen.add(player_id)
    return None


def parse_uuid(text: str) -> uuid.UUID | None:
    """UUID en texto de 36 caracteres (con guiones, en mayúsculas o minúsculas)
    o None. Rechaza las demás grafías que admite uuid.UUID (llaves, urn:)."""
    if len(text) != 36:
        return None
    try:
        return uuid.UUID(text)
    except ValueError:
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


# Detalle fijo por código: nunca se envía el texto de la excepción, que puede
# citar SQL o datos del controlador.
_DETAIL = {
    grpc.StatusCode.UNAVAILABLE: "base de datos no disponible",
    grpc.StatusCode.ALREADY_EXISTS: "el tick ya existe con otro contenido",
    grpc.StatusCode.INVALID_ARGUMENT: "los datos de la instantánea no son válidos",
    grpc.StatusCode.ABORTED: "cambio concurrente; se puede reintentar",
    grpc.StatusCode.INTERNAL: "error interno",
}


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
            players=tuple(
                SnapshotPlayer(
                    player_id=uuid.UUID(p.player_id),
                    water=p.water,
                    leaves=p.leaves,
                    water_refilled=p.water_refilled,
                    water_spent=p.water_spent,
                    leaves_earned=p.leaves_earned,
                    leaves_spent=p.leaves_spent,
                    water_reduced=p.water_reduced,
                    water_reused=p.water_reused,
                    water_recycled=p.water_recycled,
                )
                for p in message.players
            ),
        )
        # context.abort lanza su propia excepción: se llama fuera del try
        # para que el except genérico no la capture.
        try:
            outcome = self._repository.save(snapshot)
        except Exception as error:
            code, detail = self._status_and_detail_for("SaveSnapshot", error)
        else:
            if outcome is SaveOutcome.INSERTED:
                self._prune()
            return pb.SaveSnapshotResponse()
        context.abort(code, detail)

    def LoadSnapshot(self, request: pb.LoadSnapshotRequest, context: grpc.ServicerContext):
        try:
            snapshot = self._repository.load_latest()
        except Exception as error:
            code, detail = self._status_and_detail_for("LoadSnapshot", error)
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
    def _status_and_detail_for(method: str, error: Exception) -> tuple[grpc.StatusCode, str]:
        """Traduce la excepción a su código y su detalle fijo, y la registra."""
        code = _status_for(error)
        if code is grpc.StatusCode.INTERNAL:
            log.error("%s: error inesperado", method, exc_info=error)
        elif code is not grpc.StatusCode.UNAVAILABLE:
            log.error("%s rechazado (%s): %s", method, code.name, error)
        return code, _DETAIL[code]

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

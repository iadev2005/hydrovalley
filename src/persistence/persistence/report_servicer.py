"""Servidor gRPC de ReportService (GetReport, RF-12).

Valida la petición, traduce entre los mensajes del contrato y el dominio, y
entre las excepciones del puerto y los códigos estándar de gRPC. Las reglas de
periodo y de época están en persistence.reports.
"""

import logging

import grpc

from hydrovalley.persistence.v1 import report_pb2 as pb
from hydrovalley.persistence.v1 import report_pb2_grpc as pb_grpc
from persistence.domain import (
    InvalidReportPeriodError,
    NoSnapshotError,
    RepositoryUnavailableError,
    UnknownPlayerError,
)
from persistence.ports import ReportRepository
from persistence.servicer import parse_uuid

log = logging.getLogger(__name__)

# Los totales viajan como uint64; una suma global mayor (imposible con las
# cifras del juego) se satura en vez de romper la respuesta.
U64_MAX = 2**64 - 1


def _status_for(error: Exception) -> grpc.StatusCode:
    """Traduce una excepción del puerto a su código de gRPC. Lo inesperado
    es INTERNAL, nunca UNKNOWN."""
    if isinstance(error, RepositoryUnavailableError):
        return grpc.StatusCode.UNAVAILABLE
    if isinstance(error, (NoSnapshotError, UnknownPlayerError)):
        return grpc.StatusCode.NOT_FOUND
    if isinstance(error, InvalidReportPeriodError):
        return grpc.StatusCode.INVALID_ARGUMENT
    return grpc.StatusCode.INTERNAL


# Detalle fijo por código: nunca se envía el texto de la excepción, que puede
# citar SQL o datos del controlador.
_DETAIL = {
    grpc.StatusCode.UNAVAILABLE: "base de datos no disponible",
    grpc.StatusCode.NOT_FOUND: "no hay datos para el reporte",
    grpc.StatusCode.INVALID_ARGUMENT: "el periodo no es válido",
    grpc.StatusCode.INTERNAL: "error interno",
}


class ReportServicer(pb_grpc.ReportServiceServicer):
    def __init__(self, repository: ReportRepository) -> None:
        self._repository = repository

    def GetReport(self, request: pb.GetReportRequest, context: grpc.ServicerContext):
        player_id = None
        if request.player_id:
            player_id = parse_uuid(request.player_id)
            if player_id is None:
                context.abort(grpc.StatusCode.INVALID_ARGUMENT, "player_id no es un UUID")
        from_tick = request.from_tick if request.HasField("from_tick") else None
        to_tick = request.to_tick if request.HasField("to_tick") else None
        # context.abort lanza su propia excepción: se llama fuera del try
        # para que el except genérico no la capture.
        try:
            report = self._repository.report(player_id, from_tick, to_tick)
        except Exception as error:
            code = _status_for(error)
            if code is grpc.StatusCode.INTERNAL:
                log.error("GetReport: error inesperado", exc_info=error)
            elif code is not grpc.StatusCode.UNAVAILABLE:
                log.info("GetReport rechazado (%s): %s", code.name, error)
        else:
            totals = report.totals
            return pb.GetReportResponse(
                player_id=str(report.player_id) if report.player_id else "",
                epoch=report.epoch,
                from_tick=report.from_tick,
                to_tick=report.to_tick,
                totals=pb.ReportTotals(
                    water_reduced=min(totals.water_reduced, U64_MAX),
                    water_reused=min(totals.water_reused, U64_MAX),
                    water_recycled=min(totals.water_recycled, U64_MAX),
                    water_refilled=min(totals.water_refilled, U64_MAX),
                    water_spent=min(totals.water_spent, U64_MAX),
                    leaves_earned=min(totals.leaves_earned, U64_MAX),
                    leaves_spent=min(totals.leaves_spent, U64_MAX),
                    fires_extinguished=min(totals.fires_extinguished, U64_MAX),
                    tiles_grown=min(totals.tiles_grown, U64_MAX),
                ),
            )
        context.abort(code, _DETAIL[code])

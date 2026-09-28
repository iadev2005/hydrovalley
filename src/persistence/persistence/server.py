"""Montaje del servidor gRPC: PersistenceService y el servicio de salud."""

import logging
from concurrent import futures

import grpc
from grpc_health.v1 import health, health_pb2, health_pb2_grpc

from hydrovalley.persistence.v1 import persistence_pb2 as pb
from hydrovalley.persistence.v1 import persistence_pb2_grpc as pb_grpc
from persistence.ports import SnapshotRepository
from persistence.servicer import PersistenceServicer

log = logging.getLogger(__name__)

SERVICE_NAME = pb.DESCRIPTOR.services_by_name["PersistenceService"].full_name


def create_server(
    repository: SnapshotRepository, listen: str, max_workers: int = 8
) -> tuple[grpc.Server, int, health.HealthServicer]:
    """Crea el servidor sin arrancarlo. Devuelve el servidor, el puerto
    enlazado y el servicio de salud (empieza en NOT_SERVING).

    Solo se registra PersistenceService: AccountService, CatalogService y
    ReportService llegarán en otros pull requests; mientras tanto gRPC
    responde UNIMPLEMENTED a sus métodos.
    """
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=max_workers))
    pb_grpc.add_PersistenceServiceServicer_to_server(PersistenceServicer(repository), server)
    health_servicer = health.HealthServicer()
    health_pb2_grpc.add_HealthServicer_to_server(health_servicer, server)
    set_serving(health_servicer, False)
    port = server.add_insecure_port(listen)
    if port == 0:
        raise RuntimeError(f"no se pudo escuchar en {listen}")
    return server, port, health_servicer


def set_serving(health_servicer: health.HealthServicer, ready: bool) -> None:
    """SERVING solo cuando la base de datos está lista."""
    status = (
        health_pb2.HealthCheckResponse.SERVING
        if ready
        else health_pb2.HealthCheckResponse.NOT_SERVING
    )
    for service in ("", SERVICE_NAME):
        health_servicer.set(service, status)

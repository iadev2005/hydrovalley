"""Montaje del servidor gRPC: PersistenceService, AccountService y el servicio
de salud."""

import logging
from concurrent import futures

import grpc
from grpc_health.v1 import health, health_pb2, health_pb2_grpc

from hydrovalley.persistence.v1 import account_pb2 as account_pb
from hydrovalley.persistence.v1 import account_pb2_grpc as account_pb_grpc
from hydrovalley.persistence.v1 import persistence_pb2 as pb
from hydrovalley.persistence.v1 import persistence_pb2_grpc as pb_grpc
from hydrovalley.persistence.v1 import report_pb2 as report_pb
from hydrovalley.persistence.v1 import report_pb2_grpc as report_pb_grpc
from hydrovalley.persistence.v1 import catalog_pb2 as catalog_pb
from hydrovalley.persistence.v1 import catalog_pb2_grpc as catalog_pb_grpc
from persistence.account_servicer import AccountServicer
from persistence.catalog_servicer import CatalogServicer
from persistence.ports import (
    AccountRepository,
    CatalogRepository,
    ReportRepository,
    SnapshotRepository,
)
from persistence.report_servicer import ReportServicer
from persistence.request_id import RequestIdInterceptor
from persistence.servicer import PersistenceServicer

log = logging.getLogger(__name__)

SERVICE_NAME = pb.DESCRIPTOR.services_by_name["PersistenceService"].full_name
ACCOUNT_SERVICE_NAME = account_pb.DESCRIPTOR.services_by_name["AccountService"].full_name
REPORT_SERVICE_NAME = report_pb.DESCRIPTOR.services_by_name["ReportService"].full_name
CATALOG_SERVICE_NAME = catalog_pb.DESCRIPTOR.services_by_name["CatalogService"].full_name


def create_server(
    repository: SnapshotRepository,
    listen: str,
    max_workers: int = 8,
    *,
    accounts: AccountRepository,
    catalog: CatalogRepository,
    reports: ReportRepository | None = None,
) -> tuple[grpc.Server, int, health.HealthServicer]:
    """Crea el servidor sin arrancarlo. Devuelve el servidor, el puerto
    enlazado y el servicio de salud (empieza en NOT_SERVING).

    Se registran PersistenceService, AccountService, CatalogService y
    ReportService. Sin `reports`, los reportes se leen del mismo repositorio
    de instantáneas (los adaptadores de PostgreSQL y de memoria implementan
    los dos puertos).
    """
    server = grpc.server(
        futures.ThreadPoolExecutor(max_workers=max_workers),
        interceptors=[RequestIdInterceptor()],
    )
    pb_grpc.add_PersistenceServiceServicer_to_server(PersistenceServicer(repository), server)
    account_pb_grpc.add_AccountServiceServicer_to_server(AccountServicer(accounts), server)
    catalog_pb_grpc.add_CatalogServiceServicer_to_server(CatalogServicer(catalog), server)
    report_pb_grpc.add_ReportServiceServicer_to_server(
        ReportServicer(reports if reports is not None else repository), server
    )
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
    for service in ("", SERVICE_NAME, ACCOUNT_SERVICE_NAME, CATALOG_SERVICE_NAME, REPORT_SERVICE_NAME):
        health_servicer.set(service, status)

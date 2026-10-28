"""Pruebas del servidor gRPC de CatalogService con el repositorio en memoria.

Servidor gRPC real en un puerto libre de 127.0.0.1 y stub generado, para
comprobar mensajes y códigos de estado tal como los verá el simulador.
"""

import json
from pathlib import Path

import grpc
import pytest
from grpc_health.v1 import health_pb2, health_pb2_grpc

from hydrovalley.persistence.v1 import catalog_pb2 as pb
from hydrovalley.persistence.v1 import catalog_pb2_grpc as pb_grpc
from persistence.adapters.memory import (
    InMemoryAccountRepository,
    InMemoryCatalogRepository,
    InMemorySnapshotRepository,
)
from persistence.catalog import CatalogEntry, load_catalog_file
from persistence.domain import RepositoryUnavailableError
from persistence.server import CATALOG_SERVICE_NAME, create_server, set_serving

REPO_CATALOG = Path(__file__).resolve().parents[3] / "catalog" / "catalog.json"


@pytest.fixture
def catalog_repository():
    repository = InMemoryCatalogRepository()
    repository.replace(load_catalog_file(REPO_CATALOG))
    return repository


@pytest.fixture
def running(catalog_repository):
    server, port, health = create_server(
        InMemorySnapshotRepository(),
        "127.0.0.1:0",
        accounts=InMemoryAccountRepository(),
        catalog=catalog_repository,
    )
    server.start()
    channel = grpc.insecure_channel(f"127.0.0.1:{port}")
    yield pb_grpc.CatalogServiceStub(channel), channel, health
    channel.close()
    server.stop(grace=None)


@pytest.fixture
def stub(running):
    return running[0]


def get_catalog(stub) -> pb.Catalog:
    return stub.GetCatalog(pb.GetCatalogRequest(), timeout=2).catalog


def error_of(call) -> grpc.RpcError:
    with pytest.raises(grpc.RpcError) as error:
        call()
    return error.value


def test_get_catalog_returns_the_d01_figures_as_typed_fields(stub):
    catalog = get_catalog(stub)
    assert (catalog.drop.water_cost, catalog.drop.humidity) == (1, 100)
    assert catalog.drop.demo_humidity == 1
    assert (catalog.tank.capacity, catalog.tank.refill_amount) == (10, 1)
    assert catalog.tank.refill_interval_seconds == 2
    assert catalog.growth.humidity_per_stage == 100
    assert [
        catalog.growth.leaves_grass,
        catalog.growth.leaves_shrub,
        catalog.growth.leaves_flowers,
        catalog.growth.leaves_tree,
    ] == [1, 2, 3, 5]
    assert catalog.growth.tree_max_humidity == 100
    assert catalog.evaporation.humidity_per_second == 2
    assert catalog.evaporation.wither_after_ticks == 140
    assert catalog.fire.drops_to_extinguish == 3
    assert catalog.fire.extinguish_bonus_leaves == 3
    assert catalog.fire.burn_out_ticks == 300
    assert (catalog.sprinkler.price, catalog.sprinkler.area_side) == (10, 3)
    assert (catalog.sprinkler.humidity, catalog.sprinkler.interval_seconds) == (25, 2)
    assert (catalog.drip.price, catalog.drip.area_side) == (25, 5)
    assert catalog.drip.evaporation_divisor == 2
    assert catalog.treatment_plant.price == 40
    assert catalog.treatment_plant.refill_amount == 3
    assert catalog.treatment_plant.interval_seconds == 2
    assert catalog.installation_price_increase_percent == 15
    assert (catalog.bigger_drop.price, catalog.bigger_drop.area_side) == (30, 3)
    assert (catalog.bigger_tank.price, catalog.bigger_tank.extra_capacity) == (20, 10)
    assert catalog.offline_installation_seconds == 7200


def test_the_seeded_ignition_chance_is_delivered(stub):
    assert get_catalog(stub).fire.ignition_chance_ppm == 1250


def test_a_changed_ignition_chance_is_delivered(stub, catalog_repository):
    document = json.loads(REPO_CATALOG.read_text(encoding="utf-8"))
    document["rules"]["fire"]["ignition_chance_ppm"] = 500
    from persistence.catalog import parse_catalog

    catalog_repository.replace(parse_catalog(document))
    assert get_catalog(stub).fire.ignition_chance_ppm == 500


def test_get_catalog_returns_the_version_of_the_stored_catalog(stub, catalog_repository):
    version = load_catalog_file(REPO_CATALOG).version
    assert get_catalog(stub).catalog_version == version
    assert len(version) == 64


def test_the_version_changes_when_a_figure_changes(stub, catalog_repository):
    before = get_catalog(stub).catalog_version
    document = json.loads(REPO_CATALOG.read_text(encoding="utf-8"))
    document["installations"]["drip"]["price_leaves"] = 26
    from persistence.catalog import parse_catalog

    catalog_repository.replace(parse_catalog(document))
    after = get_catalog(stub)
    assert after.catalog_version != before
    assert after.drip.price == 26


def test_get_catalog_without_database_is_unavailable_with_a_fixed_detail(stub, catalog_repository):
    catalog_repository.available = False
    error = error_of(lambda: get_catalog(stub))
    assert error.code() is grpc.StatusCode.UNAVAILABLE
    assert error.details() == "base de datos no disponible"


def test_an_empty_store_is_an_internal_error_without_leaking_details(stub, catalog_repository):
    catalog_repository.clear()
    error = error_of(lambda: get_catalog(stub))
    assert error.code() is grpc.StatusCode.INTERNAL
    assert error.details() == "error interno"


def test_a_stored_catalog_that_is_not_valid_is_an_internal_error(stub, catalog_repository):
    catalog_repository.set_raw([CatalogEntry("drop", "rule", {"water_cost_liters": "secreto"})])
    error = error_of(lambda: get_catalog(stub))
    assert error.code() is grpc.StatusCode.INTERNAL
    assert error.details() == "error interno"
    assert "secreto" not in error.details()


def test_an_unexpected_failure_is_internal_never_unknown(stub, catalog_repository):
    catalog_repository.error = RuntimeError("SELECT * FROM catalog_entries falló")
    error = error_of(lambda: get_catalog(stub))
    assert error.code() is grpc.StatusCode.INTERNAL
    assert error.details() == "error interno"


def test_health_of_the_catalog_service_follows_the_database(running):
    _, channel, health = running
    health_stub = health_pb2_grpc.HealthStub(channel)

    def status():
        return health_stub.Check(
            health_pb2.HealthCheckRequest(service=CATALOG_SERVICE_NAME), timeout=2
        ).status

    assert status() == health_pb2.HealthCheckResponse.NOT_SERVING
    set_serving(health, True)
    assert status() == health_pb2.HealthCheckResponse.SERVING
    set_serving(health, False)
    assert status() == health_pb2.HealthCheckResponse.NOT_SERVING


def test_a_repository_outage_error_type_is_translated(stub, catalog_repository):
    catalog_repository.error = RepositoryUnavailableError("caída")
    assert error_of(lambda: get_catalog(stub)).code() is grpc.StatusCode.UNAVAILABLE

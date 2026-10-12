"""Pruebas del servidor gRPC con el repositorio en memoria.

Se levanta un servidor gRPC real en un puerto libre de 127.0.0.1 (solo
dentro de la prueba) y se le habla con el stub generado, así se comprueban
los códigos de estado tal como los verá el simulador.
"""

import grpc
import pytest
from grpc_health.v1 import health_pb2, health_pb2_grpc

from hydrovalley.persistence.v1 import persistence_pb2 as pb
from hydrovalley.persistence.v1 import persistence_pb2_grpc as pb_grpc
from persistence.adapters.memory import (
    InMemoryAccountRepository,
    InMemoryCatalogRepository,
    InMemorySnapshotRepository,
)
from persistence.domain import (
    ConcurrentSnapshotChangeError,
    InvalidSnapshotError,
    RepositoryUnavailableError,
    Snapshot,
    SnapshotConflictError,
)
from persistence.retention import TICKS_PER_HOUR
from persistence.server import SERVICE_NAME, create_server, set_serving
from persistence.servicer import MAX_CATALOG_VERSION_LENGTH


@pytest.fixture
def repository():
    return InMemorySnapshotRepository()


@pytest.fixture
def running(repository):
    server, port, health = create_server(
        repository, "127.0.0.1:0", accounts=InMemoryAccountRepository(),
        catalog=InMemoryCatalogRepository(),
    )
    server.start()
    channel = grpc.insecure_channel(f"127.0.0.1:{port}")
    yield pb_grpc.PersistenceServiceStub(channel), channel, health
    channel.close()
    server.stop(grace=None)


@pytest.fixture
def stub(running):
    return running[0]


def snapshot_msg(tick: int, epoch: int = 1, world_state: bytes = b"world") -> pb.Snapshot:
    return pb.Snapshot(
        tick=tick, epoch=epoch, catalog_version="cat-v1", world_state=world_state
    )


def save(stub, snapshot: pb.Snapshot):
    return stub.SaveSnapshot(pb.SaveSnapshotRequest(snapshot=snapshot), timeout=2)


def status_of(call) -> grpc.StatusCode:
    with pytest.raises(grpc.RpcError) as error:
        call()
    return error.value.code()


def test_save_new_snapshot_stores_it(stub, repository):
    save(stub, snapshot_msg(50, world_state=b"abc"))
    assert repository.load_latest() == Snapshot(
        tick=50, epoch=1, catalog_version="cat-v1", world_state=b"abc"
    )


def test_identical_retry_is_ok(stub, repository):
    save(stub, snapshot_msg(100))
    save(stub, snapshot_msg(100))
    assert repository.ticks() == [100]


def test_same_tick_with_different_world_state_is_already_exists(stub):
    save(stub, snapshot_msg(100, world_state=b"one"))
    code = status_of(lambda: save(stub, snapshot_msg(100, world_state=b"two")))
    assert code == grpc.StatusCode.ALREADY_EXISTS


def test_same_tick_with_different_epoch_is_already_exists(stub):
    save(stub, snapshot_msg(100, epoch=1))
    code = status_of(lambda: save(stub, snapshot_msg(100, epoch=2)))
    assert code == grpc.StatusCode.ALREADY_EXISTS


def test_save_without_snapshot_is_invalid_argument(stub):
    code = status_of(lambda: stub.SaveSnapshot(pb.SaveSnapshotRequest(), timeout=2))
    assert code == grpc.StatusCode.INVALID_ARGUMENT


def test_epoch_beyond_integer_column_is_invalid_argument(stub):
    # La columna epoch es INTEGER (diagrama 07): como mucho 2^31 - 1.
    code = status_of(lambda: save(stub, snapshot_msg(50, epoch=2**31)))
    assert code == grpc.StatusCode.INVALID_ARGUMENT


def test_load_returns_the_highest_tick(stub):
    for tick in (50, 150, 100):
        save(stub, snapshot_msg(tick, world_state=f"w{tick}".encode()))
    response = stub.LoadSnapshot(pb.LoadSnapshotRequest(), timeout=2)
    assert response.snapshot == snapshot_msg(150, world_state=b"w150")


def test_load_on_empty_table_is_not_found(stub):
    code = status_of(lambda: stub.LoadSnapshot(pb.LoadSnapshotRequest(), timeout=2))
    assert code == grpc.StatusCode.NOT_FOUND


def test_new_save_prunes_snapshots_outside_retention(stub, repository):
    for tick in (0, 50, 100):
        save(stub, snapshot_msg(tick))
    latest = 2 * TICKS_PER_HOUR
    save(stub, snapshot_msg(latest))
    # 50 y 100 son de una hora antigua que ya conserva la del tick 0.
    assert repository.ticks() == [0, latest]


def test_prune_failure_does_not_fail_the_save(stub, repository):
    repository.fail_prune = True
    save(stub, snapshot_msg(50))
    assert repository.ticks() == [50]


def test_unavailable_repository_answers_unavailable(stub, repository):
    repository.available = False
    assert status_of(lambda: save(stub, snapshot_msg(50))) == grpc.StatusCode.UNAVAILABLE
    code = status_of(lambda: stub.LoadSnapshot(pb.LoadSnapshotRequest(), timeout=2))
    assert code == grpc.StatusCode.UNAVAILABLE


def test_health_reports_serving_state(running):
    _, channel, health = running
    health_stub = health_pb2_grpc.HealthStub(channel)

    def check(service: str) -> int:
        request = health_pb2.HealthCheckRequest(service=service)
        return health_stub.Check(request, timeout=2).status

    set_serving(health, True)
    assert check("") == health_pb2.HealthCheckResponse.SERVING
    assert check(SERVICE_NAME) == health_pb2.HealthCheckResponse.SERVING
    set_serving(health, False)
    assert check(SERVICE_NAME) == health_pb2.HealthCheckResponse.NOT_SERVING


# --- Validación y traducción de errores ------------------------------------


def test_oversized_catalog_version_is_invalid_argument_without_touching_the_repository(
    stub, repository
):
    message = snapshot_msg(50)
    message.catalog_version = "x" * (MAX_CATALOG_VERSION_LENGTH + 1)
    assert status_of(lambda: save(stub, message)) == grpc.StatusCode.INVALID_ARGUMENT
    assert repository.ticks() == []


def test_catalog_version_with_nul_is_invalid_argument(stub, repository):
    message = snapshot_msg(50)
    message.catalog_version = "cat\x00v1"
    assert status_of(lambda: save(stub, message)) == grpc.StatusCode.INVALID_ARGUMENT
    assert repository.ticks() == []


def test_empty_catalog_version_is_accepted_until_catalog_service_exists(stub, repository):
    message = snapshot_msg(50)
    message.catalog_version = ""
    save(stub, message)
    assert repository.ticks() == [50]


def test_data_that_does_not_fit_the_schema_is_invalid_argument(stub, repository):
    repository.save_error = InvalidSnapshotError("valor fuera de rango")
    assert status_of(lambda: save(stub, snapshot_msg(50))) == grpc.StatusCode.INVALID_ARGUMENT


def test_concurrent_change_is_aborted(stub, repository):
    repository.save_error = ConcurrentSnapshotChangeError("borrada a la vez")
    assert status_of(lambda: save(stub, snapshot_msg(50))) == grpc.StatusCode.ABORTED


def test_unexpected_errors_are_internal_never_unknown(stub, repository):
    repository.save_error = RuntimeError("fallo inesperado")
    repository.load_error = RuntimeError("fallo inesperado")
    assert status_of(lambda: save(stub, snapshot_msg(50))) == grpc.StatusCode.INTERNAL
    code = status_of(lambda: stub.LoadSnapshot(pb.LoadSnapshotRequest(), timeout=2))
    assert code == grpc.StatusCode.INTERNAL


LEAK = "SECRET-SQL-TEXT tick=99"


@pytest.mark.parametrize(
    ("failure", "expected"),
    [
        (RuntimeError(LEAK), "error interno"),
        (InvalidSnapshotError(LEAK), "los datos de la instantánea no son válidos"),
        (SnapshotConflictError(LEAK), "el tick ya existe con otro contenido"),
        (ConcurrentSnapshotChangeError(LEAK), "cambio concurrente; se puede reintentar"),
        (RepositoryUnavailableError(LEAK), "base de datos no disponible"),
    ],
)
def test_error_details_are_fixed_and_never_echo_the_exception(stub, repository, failure, expected):
    repository.save_error = failure
    repository.load_error = failure
    for call in (
        lambda: save(stub, snapshot_msg(50)),
        lambda: stub.LoadSnapshot(pb.LoadSnapshotRequest(), timeout=2),
    ):
        with pytest.raises(grpc.RpcError) as error:
            call()
        assert error.value.details() == expected


# --- Jugadores de la instantánea (snapshot_players) --------------------------

import uuid  # noqa: E402

PLAYER_A = str(uuid.uuid4())
PLAYER_B = str(uuid.uuid4())


def with_players(message: pb.Snapshot, *players: pb.SnapshotPlayer) -> pb.Snapshot:
    message.players.extend(players)
    return message


def test_players_are_stored_with_the_snapshot(stub, repository):
    save(stub, with_players(
        snapshot_msg(50),
        pb.SnapshotPlayer(player_id=PLAYER_A.upper(), water_reduced=7, leaves_earned=3),
    ))
    (row,) = repository.load_latest().players
    assert row.player_id == uuid.UUID(PLAYER_A)
    assert (row.water_reduced, row.leaves_earned) == (7, 3)


@pytest.mark.parametrize("bad", ["", "no-uuid", "123"])
def test_malformed_player_id_in_a_snapshot_is_invalid_argument(stub, repository, bad):
    message = with_players(snapshot_msg(50), pb.SnapshotPlayer(player_id=bad))
    assert status_of(lambda: save(stub, message)) == grpc.StatusCode.INVALID_ARGUMENT
    assert repository.ticks() == []


def test_duplicate_player_ids_in_a_snapshot_are_invalid_argument(stub, repository):
    message = with_players(
        snapshot_msg(50),
        pb.SnapshotPlayer(player_id=PLAYER_A),
        pb.SnapshotPlayer(player_id=PLAYER_A.upper()),
    )
    assert status_of(lambda: save(stub, message)) == grpc.StatusCode.INVALID_ARGUMENT
    assert repository.ticks() == []


def test_identical_retry_with_players_is_ok(stub, repository):
    message = with_players(snapshot_msg(50), pb.SnapshotPlayer(player_id=PLAYER_A))
    save(stub, message)
    save(stub, message)
    assert repository.ticks() == [50]

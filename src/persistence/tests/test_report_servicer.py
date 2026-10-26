"""Pruebas del servidor gRPC de ReportService con repositorios en memoria."""

import uuid

import grpc
import pytest
from grpc_health.v1 import health_pb2, health_pb2_grpc

from hydrovalley.persistence.v1 import persistence_pb2 as snap_pb
from hydrovalley.persistence.v1 import report_pb2 as pb
from hydrovalley.persistence.v1 import report_pb2_grpc as pb_grpc
from persistence.adapters.memory import (
    InMemoryAccountRepository,
    InMemoryCatalogRepository,
    InMemorySnapshotRepository,
)
from persistence.domain import RepositoryUnavailableError
from persistence.server import REPORT_SERVICE_NAME, create_server, set_serving

HASH = "$argon2id$v=19$m=19456,t=2,p=1$c29tZXNhbHQ$aGFzaGhhc2hoYXNo"


@pytest.fixture
def accounts():
    return InMemoryAccountRepository()


@pytest.fixture
def snapshots(accounts):
    return InMemorySnapshotRepository(accounts=accounts)


@pytest.fixture
def running(snapshots, accounts):
    server, port, health = create_server(
        snapshots, "127.0.0.1:0", accounts=accounts, catalog=InMemoryCatalogRepository(),
        reports=snapshots,
    )
    server.start()
    channel = grpc.insecure_channel(f"127.0.0.1:{port}")
    yield pb_grpc.ReportServiceStub(channel), channel, health
    channel.close()
    server.stop(grace=None)


@pytest.fixture
def stub(running):
    return running[0]


def player(player_id, **counters) -> snap_pb.SnapshotPlayer:
    return snap_pb.SnapshotPlayer(player_id=str(player_id), **counters)


def store(snapshots, tick, players, epoch=1):
    from persistence.domain import Snapshot, SnapshotPlayer

    snapshots.save(
        Snapshot(
            tick=tick, epoch=epoch, catalog_version="c", world_state=f"w{tick}-{epoch}".encode(),
            players=tuple(
                SnapshotPlayer(
                    player_id=uuid.UUID(p.player_id), water=p.water, leaves=p.leaves,
                    water_refilled=p.water_refilled, water_spent=p.water_spent,
                    leaves_earned=p.leaves_earned, leaves_spent=p.leaves_spent,
                    water_reduced=p.water_reduced, water_reused=p.water_reused,
                    water_recycled=p.water_recycled,
                    fires_extinguished=p.fires_extinguished, tiles_grown=p.tiles_grown,
                )
                for p in players
            ),
        )
    )


def get_report(stub, **kwargs) -> pb.GetReportResponse:
    return stub.GetReport(pb.GetReportRequest(**kwargs), timeout=2)


def code_of(call) -> grpc.RpcError:
    with pytest.raises(grpc.RpcError) as error:
        call()
    return error.value


@pytest.fixture
def ana(accounts):
    return accounts.register("ana", HASH).id


@pytest.fixture
def beto(accounts):
    return accounts.register("beto", HASH).id


def test_no_snapshot_is_not_found(stub, ana):
    assert code_of(lambda: get_report(stub, player_id=str(ana))).code() == grpc.StatusCode.NOT_FOUND
    assert code_of(lambda: get_report(stub)).code() == grpc.StatusCode.NOT_FOUND


def test_player_report_is_their_row_in_the_latest_snapshot(stub, snapshots, ana, beto):
    store(snapshots, 50, [player(ana, water_reduced=1), player(beto, water_reduced=9)])
    store(snapshots, 100, [player(ana, water_reduced=5, water_reused=2, water_recycled=3,
                                  water_refilled=40, water_spent=30, leaves_earned=8,
                                  leaves_spent=6), player(beto, water_reduced=11)])
    response = get_report(stub, player_id=str(ana))
    assert (response.player_id, response.epoch, response.from_tick, response.to_tick) == (
        str(ana), 1, 0, 100)
    t = response.totals
    assert (t.water_reduced, t.water_reused, t.water_recycled) == (5, 2, 3)
    assert (t.water_refilled, t.water_spent, t.leaves_earned, t.leaves_spent) == (40, 30, 8, 6)


def test_report_carries_the_sprinkler_counters_as_u64(stub, snapshots, ana, beto):
    big = 2**40
    store(snapshots, 50, [player(ana, fires_extinguished=2, tiles_grown=big),
                          player(beto, fires_extinguished=1, tiles_grown=4)])
    mine = get_report(stub, player_id=str(ana)).totals
    assert (mine.fires_extinguished, mine.tiles_grown) == (2, big)
    everyone = get_report(stub).totals
    assert (everyone.fires_extinguished, everyone.tiles_grown) == (3, big + 4)


def test_period_report_subtracts_the_sprinkler_counters(stub, snapshots, ana):
    store(snapshots, 50, [player(ana, fires_extinguished=1, tiles_grown=3)])
    store(snapshots, 100, [player(ana, fires_extinguished=4, tiles_grown=10)])
    totals = get_report(stub, player_id=str(ana), from_tick=50, to_tick=100).totals
    assert (totals.fires_extinguished, totals.tiles_grown) == (3, 7)


def test_global_report_sums_all_players(stub, snapshots, ana, beto):
    store(snapshots, 50, [player(ana, water_reduced=5, leaves_earned=1),
                          player(beto, water_reduced=7, leaves_earned=2)])
    response = get_report(stub)
    assert response.player_id == ""
    assert response.totals.water_reduced == 12
    assert response.totals.leaves_earned == 3


def test_period_is_the_difference_between_two_snapshots(stub, snapshots, ana):
    store(snapshots, 50, [player(ana, water_reduced=10)])
    store(snapshots, 100, [player(ana, water_reduced=25)])
    store(snapshots, 150, [player(ana, water_reduced=60)])
    response = get_report(stub, player_id=str(ana), from_tick=60, to_tick=149)
    assert (response.from_tick, response.to_tick) == (50, 100)
    assert response.totals.water_reduced == 15


def test_only_to_tick_counts_from_the_epoch_start(stub, snapshots, ana):
    store(snapshots, 50, [player(ana, water_reduced=10)])
    store(snapshots, 100, [player(ana, water_reduced=25)])
    response = get_report(stub, player_id=str(ana), to_tick=70)
    assert (response.from_tick, response.to_tick) == (0, 50)
    assert response.totals.water_reduced == 10


def test_player_who_joined_after_the_start_counts_from_zero(stub, snapshots, ana, beto):
    store(snapshots, 50, [player(ana, water_reduced=10)])
    store(snapshots, 100, [player(ana, water_reduced=10), player(beto, water_reduced=4)])
    response = get_report(stub, player_id=str(beto), from_tick=50)
    assert response.totals.water_reduced == 4


def test_epoch_boundary_ignores_previous_epochs(stub, snapshots, ana):
    store(snapshots, 50, [player(ana, water_reduced=900)], epoch=1)
    store(snapshots, 100, [player(ana, water_reduced=3)], epoch=2)
    store(snapshots, 150, [player(ana, water_reduced=8)], epoch=2)
    response = get_report(stub, player_id=str(ana))
    assert response.epoch == 2
    assert response.totals.water_reduced == 8
    assert get_report(stub, player_id=str(ana), from_tick=100).totals.water_reduced == 5
    # Un tick de la época anterior queda fuera de la época actual.
    assert code_of(lambda: get_report(stub, player_id=str(ana), from_tick=50)).code() == (
        grpc.StatusCode.INVALID_ARGUMENT)
    assert code_of(lambda: get_report(stub, player_id=str(ana), to_tick=99)).code() == (
        grpc.StatusCode.INVALID_ARGUMENT)


def test_known_account_without_a_row_gets_zeros(stub, snapshots, ana, beto):
    store(snapshots, 50, [player(ana, water_reduced=5)])
    response = get_report(stub, player_id=str(beto))
    assert response.totals == pb.ReportTotals()


def test_unknown_account_is_not_found(stub, snapshots, ana):
    store(snapshots, 50, [player(ana, water_reduced=5)])
    error = code_of(lambda: get_report(stub, player_id=str(uuid.uuid4())))
    assert error.code() == grpc.StatusCode.NOT_FOUND


@pytest.mark.parametrize("bad", ["no-uuid", "123", " " * 3, "{" + str(uuid.uuid4()) + "}"])
def test_malformed_player_id_is_invalid_argument(stub, bad):
    assert code_of(lambda: get_report(stub, player_id=bad)).code() == (
        grpc.StatusCode.INVALID_ARGUMENT)


def test_from_tick_after_to_tick_is_invalid_argument(stub, snapshots, ana):
    store(snapshots, 50, [player(ana)])
    assert code_of(lambda: get_report(stub, from_tick=60, to_tick=50)).code() == (
        grpc.StatusCode.INVALID_ARGUMENT)


def test_uppercase_player_id_is_normalized(stub, snapshots, ana):
    store(snapshots, 50, [player(ana, water_reduced=2)])
    response = get_report(stub, player_id=str(ana).upper())
    assert response.player_id == str(ana)
    assert response.totals.water_reduced == 2


def test_unavailable_database_is_unavailable_with_fixed_detail(stub, snapshots):
    snapshots.available = False
    error = code_of(lambda: get_report(stub))
    assert error.code() == grpc.StatusCode.UNAVAILABLE
    assert error.details() == "base de datos no disponible"


def test_unexpected_error_is_internal_without_leaking(stub, snapshots):
    snapshots.report_error = RuntimeError("SELECT secreto FROM x")
    error = code_of(lambda: get_report(stub))
    assert error.code() == grpc.StatusCode.INTERNAL
    assert error.details() == "error interno"


def test_health_of_report_service_follows_the_database(running):
    _, channel, health = running
    health_stub = health_pb2_grpc.HealthStub(channel)

    def status():
        return health_stub.Check(
            health_pb2.HealthCheckRequest(service=REPORT_SERVICE_NAME), timeout=2).status

    assert status() == health_pb2.HealthCheckResponse.NOT_SERVING
    set_serving(health, True)
    assert status() == health_pb2.HealthCheckResponse.SERVING

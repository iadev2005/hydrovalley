"""Pruebas de snapshot_players y de los reportes en PostgreSQL.

Las de integración reutilizan los fixtures de test_postgres y solo corren
con TEST_DATABASE_URL (base de datos desechable).
"""

import logging
import uuid

import psycopg
import pytest

from persistence.domain import (
    InvalidReportPeriodError,
    InvalidSnapshotError,
    NoSnapshotError,
    ReportTotals,
    SaveOutcome,
    Snapshot,
    SnapshotConflictError,
    SnapshotPlayer,
    UnknownPlayerError,
)
from persistence.retention import TICKS_PER_HOUR
from tests.test_postgres import (  # noqa: F401 (fixtures de pytest)
    FakeConnection,
    clean_database,
    integration,
    query,
    repository,
    repository_with,
)

HASH = "$argon2id$v=19$m=19456,t=2,p=1$c29tZXNhbHQ$aGFzaGhhc2hoYXNo"
U64_MAX = 2**64 - 1


def row(player_id, **counters) -> SnapshotPlayer:
    return SnapshotPlayer(player_id=player_id, **counters)


def snap(tick, *players, epoch=1) -> Snapshot:
    return Snapshot(
        tick=tick, epoch=epoch, catalog_version="c", world_state=f"w{tick}/{epoch}".encode(),
        players=tuple(players),
    )


def totals(**kwargs) -> ReportTotals:
    return ReportTotals(**{**dict.fromkeys(ReportTotals.__dataclass_fields__, 0), **kwargs})


# --- Sin base de datos ------------------------------------------------------


def test_snapshot_without_players_runs_no_player_statements():
    conn = FakeConnection(rows=[(50,)])
    assert repository_with(conn).save(snap(50)) is SaveOutcome.INSERTED
    assert not any("snapshot_players" in sql or "accounts" in sql for sql in conn.statements)


def test_retry_of_an_existing_snapshot_does_not_touch_player_rows():
    import hashlib

    sha = hashlib.sha256(b"w50/1").digest()
    conn = FakeConnection(rows=[None, (1, sha)])
    outcome = repository_with(conn).save(snap(50, row(uuid.uuid4(), water_reduced=1)))
    assert outcome is SaveOutcome.ALREADY_STORED
    assert not any("snapshot_players" in sql for sql in conn.statements)


# --- Con PostgreSQL real ----------------------------------------------------


@pytest.fixture
def ana(repository):
    return repository.register("ana", HASH).id


@pytest.fixture
def beto(repository):
    return repository.register("beto", HASH).id


@integration
def test_migration_creates_snapshot_players_with_the_diagram_columns(repository):
    columns = query(
        "SELECT column_name, data_type, is_nullable FROM information_schema.columns "
        "WHERE table_name = 'snapshot_players' ORDER BY ordinal_position"
    )
    assert columns == [
        ("tick", "numeric", "NO"),
        ("player_id", "uuid", "NO"),
        ("water", "numeric", "NO"),
        ("leaves", "numeric", "NO"),
        ("water_refilled", "numeric", "NO"),
        ("water_spent", "numeric", "NO"),
        ("leaves_earned", "numeric", "NO"),
        ("leaves_spent", "numeric", "NO"),
        ("water_reduced", "numeric", "NO"),
        ("water_reused", "numeric", "NO"),
        ("water_recycled", "numeric", "NO"),
    ]


@integration
def test_snapshot_and_players_are_stored_together(repository, ana, beto):
    repository.save(snap(50, row(ana, water=3, leaves=2, water_reduced=7),
                         row(beto, water_recycled=U64_MAX)))
    rows = query("SELECT player_id, water, leaves, water_reduced, water_recycled "
                 "FROM snapshot_players WHERE tick = 50 ORDER BY water_recycled")
    assert [(r[0], int(r[1]), int(r[2]), int(r[3]), int(r[4])) for r in rows] == [
        (ana, 3, 2, 7, 0),
        (beto, 0, 0, 0, U64_MAX),
    ]


@integration
def test_identical_retry_does_not_duplicate_player_rows(repository, ana):
    message = snap(50, row(ana, water_reduced=7))
    assert repository.save(message) is SaveOutcome.INSERTED
    assert repository.save(message) is SaveOutcome.ALREADY_STORED
    assert query("SELECT count(*) FROM snapshot_players") == [(1,)]


@integration
def test_conflicting_retry_keeps_the_original_player_rows(repository, ana):
    repository.save(snap(50, row(ana, water_reduced=7)))
    other = Snapshot(50, 1, "c", b"otro", (row(ana, water_reduced=99),))
    with pytest.raises(SnapshotConflictError):
        repository.save(other)
    assert [int(r[0]) for r in query("SELECT water_reduced FROM snapshot_players")] == [7]


@integration
def test_failing_player_row_rolls_back_the_whole_snapshot(repository, ana):
    with pytest.raises(InvalidSnapshotError):
        repository.save(snap(50, row(ana, water_reduced=2**64)))
    assert query("SELECT count(*) FROM snapshots") == [(0,)]
    assert query("SELECT count(*) FROM snapshot_players") == [(0,)]


@integration
def test_player_without_account_is_skipped_with_a_warning(repository, ana, caplog):
    stranger = uuid.uuid4()
    with caplog.at_level(logging.WARNING):
        outcome = repository.save(snap(50, row(ana, water_reduced=1),
                                       row(stranger, water_reduced=9)))
    assert outcome is SaveOutcome.INSERTED
    assert [r[0] for r in query("SELECT player_id FROM snapshot_players")] == [ana]
    assert str(stranger) in caplog.text
    assert query("SELECT count(*) FROM snapshots") == [(1,)]


@integration
def test_pruning_a_snapshot_cascades_to_its_player_rows(repository, ana):
    old = 50
    repository.save(snap(old, row(ana, water_reduced=1)))
    repository.save(snap(old + 1, row(ana, water_reduced=2)))  # misma hora: se poda
    repository.save(snap(old + 2 * TICKS_PER_HOUR, row(ana, water_reduced=3)))
    assert repository.prune() == 1
    ticks = [int(r[0]) for r in query("SELECT tick FROM snapshot_players ORDER BY tick")]
    assert ticks == [old, old + 2 * TICKS_PER_HOUR]


@integration
def test_report_without_snapshots_raises_no_snapshot(repository, ana):
    with pytest.raises(NoSnapshotError):
        repository.report(ana, None, None)
    with pytest.raises(NoSnapshotError):
        repository.report(None, None, None)


@integration
def test_player_and_global_totals(repository, ana, beto):
    repository.save(snap(50, row(ana, water_reduced=5, water_reused=1, water_recycled=2,
                                 water_refilled=10, water_spent=4, leaves_earned=6,
                                 leaves_spent=3),
                         row(beto, water_reduced=7, leaves_earned=1)))
    mine = repository.report(ana, None, None)
    assert (mine.player_id, mine.epoch, mine.from_tick, mine.to_tick) == (ana, 1, 0, 50)
    assert mine.totals == totals(water_reduced=5, water_reused=1, water_recycled=2,
                                 water_refilled=10, water_spent=4, leaves_earned=6,
                                 leaves_spent=3)
    everyone = repository.report(None, None, None)
    assert everyone.player_id is None
    assert everyone.totals.water_reduced == 12
    assert everyone.totals.leaves_earned == 7


@integration
def test_period_difference_between_two_snapshots(repository, ana, beto):
    repository.save(snap(50, row(ana, water_reduced=10), row(beto, water_reduced=1)))
    repository.save(snap(100, row(ana, water_reduced=25), row(beto, water_reduced=2)))
    repository.save(snap(150, row(ana, water_reduced=60), row(beto, water_reduced=4)))
    report = repository.report(ana, 60, 149)
    assert (report.from_tick, report.to_tick) == (50, 100)
    assert report.totals.water_reduced == 15
    assert repository.report(None, 60, 149).totals.water_reduced == 16
    assert repository.report(ana, None, 70).totals.water_reduced == 10


@integration
def test_period_boundaries_are_validated(repository, ana):
    repository.save(snap(100, row(ana)))
    repository.save(snap(150, row(ana)))
    with pytest.raises(InvalidReportPeriodError):
        repository.report(ana, 50, None)
    with pytest.raises(InvalidReportPeriodError):
        repository.report(ana, None, 99)
    with pytest.raises(InvalidReportPeriodError):
        repository.report(ana, 150, 100)


@integration
def test_epoch_boundary_after_a_reset(repository, ana):
    repository.save(snap(50, row(ana, water_reduced=900), epoch=1))
    repository.save(snap(100, row(ana, water_reduced=3), epoch=2))
    repository.save(snap(150, row(ana, water_reduced=8), epoch=2))
    report = repository.report(ana, None, None)
    assert report.epoch == 2 and report.totals.water_reduced == 8
    assert repository.report(ana, 100, None).totals.water_reduced == 5
    with pytest.raises(InvalidReportPeriodError):
        repository.report(ana, 50, None)
    with pytest.raises(InvalidReportPeriodError):
        repository.report(ana, None, 99)


@integration
def test_account_without_row_gets_zeros_and_unknown_account_is_rejected(repository, ana, beto):
    repository.save(snap(50, row(ana, water_reduced=5)))
    assert repository.report(beto, None, None).totals == totals()
    with pytest.raises(UnknownPlayerError):
        repository.report(uuid.uuid4(), None, None)


@integration
def test_player_who_joined_later_counts_from_zero(repository, ana, beto):
    repository.save(snap(50, row(ana, water_reduced=1)))
    repository.save(snap(100, row(ana, water_reduced=1), row(beto, water_reduced=4)))
    assert repository.report(beto, 50, None).totals.water_reduced == 4

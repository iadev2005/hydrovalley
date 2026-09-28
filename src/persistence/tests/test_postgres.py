"""Pruebas del adaptador de PostgreSQL.

Las de integración solo corren si TEST_DATABASE_URL apunta a una base de
datos desechable: borran y recrean las tablas del servicio.
"""

import hashlib
import os
import socket
import time

import psycopg
import pytest

from persistence.adapters.postgres import PostgresSnapshotRepository
from persistence.domain import (
    RepositoryUnavailableError,
    SaveOutcome,
    Snapshot,
    SnapshotConflictError,
)
from persistence.migrations import DEFAULT_MIGRATIONS_DIR, apply_migrations
from persistence.retention import TICKS_PER_HOUR, ticks_to_prune

TEST_DATABASE_URL = os.environ.get("TEST_DATABASE_URL")

_needs_database = pytest.mark.skipif(
    not TEST_DATABASE_URL, reason="TEST_DATABASE_URL no está definida"
)


def integration(test):
    """Marca la prueba como de integración y la salta sin TEST_DATABASE_URL."""
    return pytest.mark.integration(_needs_database(test))


def free_port() -> int:
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


def snapshot(tick: int, epoch: int = 1, world_state: bytes = b"world") -> Snapshot:
    return Snapshot(tick=tick, epoch=epoch, catalog_version="cat-v1", world_state=world_state)


# --- Sin base de datos -----------------------------------------------------


def test_operations_are_unavailable_before_connecting():
    repository = PostgresSnapshotRepository("postgresql://nobody@127.0.0.1:1/none")
    with pytest.raises(RepositoryUnavailableError):
        repository.load_latest()
    with pytest.raises(RepositoryUnavailableError):
        repository.save(snapshot(50))
    with pytest.raises(RepositoryUnavailableError):
        repository.prune()


def test_unreachable_database_keeps_retrying_without_crashing():
    readiness: list[bool] = []
    repository = PostgresSnapshotRepository(
        f"postgresql://nobody@127.0.0.1:{free_port()}/none",
        on_ready_change=readiness.append,
        connect_timeout_s=1,
        backoff_initial_s=0.05,
        backoff_max_s=0.1,
    )
    repository.start()
    try:
        assert repository.wait_ready(timeout_s=0.5) is False
        assert repository.is_ready is False
        with pytest.raises(RepositoryUnavailableError):
            repository.load_latest()
    finally:
        repository.stop()
    assert True not in readiness


# --- Con PostgreSQL real ---------------------------------------------------


@pytest.fixture
def clean_database():
    with psycopg.connect(TEST_DATABASE_URL, autocommit=True) as conn:
        conn.execute("DROP TABLE IF EXISTS snapshots, schema_migrations")
    yield TEST_DATABASE_URL


@pytest.fixture
def repository(clean_database):
    readiness: list[bool] = []
    repo = PostgresSnapshotRepository(
        clean_database,
        on_ready_change=readiness.append,
        backoff_initial_s=0.05,
        backoff_max_s=0.2,
    )
    repo.readiness = readiness
    repo.start()
    assert repo.wait_ready(timeout_s=10)
    yield repo
    repo.stop()


def query(sql: str, params=None):
    with psycopg.connect(TEST_DATABASE_URL, autocommit=True) as conn:
        return conn.execute(sql, params).fetchall()


@integration
def test_migrations_create_the_table_and_are_idempotent(clean_database):
    with psycopg.connect(clean_database, autocommit=True) as conn:
        assert apply_migrations(conn, DEFAULT_MIGRATIONS_DIR) == ["0001"]
        assert apply_migrations(conn, DEFAULT_MIGRATIONS_DIR) == []
    columns = query(
        "SELECT column_name, data_type, is_nullable FROM information_schema.columns "
        "WHERE table_name = 'snapshots' ORDER BY ordinal_position"
    )
    assert columns == [
        ("tick", "numeric", "NO"),
        ("epoch", "integer", "NO"),
        ("catalog_version", "text", "NO"),
        ("taken_at", "timestamp with time zone", "NO"),
        ("world_state", "bytea", "NO"),
        ("world_state_sha256", "bytea", "NO"),
    ]


@integration
def test_start_reports_ready(repository):
    assert repository.is_ready
    assert repository.readiness == [True]


@integration
def test_save_and_load_round_trip_with_full_u64_tick(repository):
    tick = 2**64 - 1
    assert repository.save(snapshot(tick, world_state=b"\x00\x01")) is SaveOutcome.INSERTED
    assert repository.load_latest() == snapshot(tick, world_state=b"\x00\x01")
    stored = query("SELECT world_state_sha256 FROM snapshots")
    assert bytes(stored[0][0]) == hashlib.sha256(b"\x00\x01").digest()


@integration
def test_load_on_empty_table_returns_none(repository):
    assert repository.load_latest() is None


@integration
def test_load_returns_highest_tick(repository):
    for tick in (50, 150, 100):
        repository.save(snapshot(tick, world_state=str(tick).encode()))
    assert repository.load_latest() == snapshot(150, world_state=b"150")


@integration
def test_identical_retry_is_already_stored(repository):
    repository.save(snapshot(100))
    assert repository.save(snapshot(100)) is SaveOutcome.ALREADY_STORED
    assert query("SELECT count(*) FROM snapshots") == [(1,)]


@integration
def test_different_content_for_same_tick_is_a_conflict(repository):
    repository.save(snapshot(100, epoch=1, world_state=b"a"))
    with pytest.raises(SnapshotConflictError):
        repository.save(snapshot(100, epoch=1, world_state=b"b"))
    with pytest.raises(SnapshotConflictError):
        repository.save(snapshot(100, epoch=2, world_state=b"a"))
    assert repository.load_latest() == snapshot(100, epoch=1, world_state=b"a")


@integration
def test_prune_applies_the_retention_rule(repository):
    ticks = list(range(0, 2 * TICKS_PER_HOUR + 1, 50))
    for tick in ticks:
        repository.save(snapshot(tick))
    expected = sorted(set(ticks) - ticks_to_prune(ticks))
    assert repository.prune() == len(ticks) - len(expected)
    remaining = [int(row[0]) for row in query("SELECT tick FROM snapshots ORDER BY tick")]
    assert remaining == expected
    assert repository.prune() == 0


@integration
def test_recovers_after_losing_the_connection(repository):
    repository.save(snapshot(50))
    query(
        "SELECT pg_terminate_backend(pid) FROM pg_stat_activity "
        "WHERE application_name = 'hydrovalley-persistence'"
    )
    with pytest.raises(RepositoryUnavailableError):
        repository.load_latest()
    deadline = time.monotonic() + 10
    while not repository.is_ready and time.monotonic() < deadline:
        time.sleep(0.05)
    assert repository.load_latest() == snapshot(50)
    assert repository.readiness == [True, False, True]

"""Pruebas del adaptador de PostgreSQL.

Las de integración solo corren si TEST_DATABASE_URL apunta a una base de
datos desechable: borran y recrean las tablas del servicio.
"""

import contextlib
import hashlib
import os
import shutil
import socket
import threading
import time

import psycopg
import pytest

from persistence.adapters.postgres import PostgresSnapshotRepository
from persistence.domain import (
    ConcurrentSnapshotChangeError,
    InvalidSnapshotError,
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


class FakeCursor:
    def __init__(self, row):
        self._row = row

    def fetchone(self):
        return self._row


class FakeConnection:
    """Conexión mínima para probar el adaptador sin base de datos.

    rows son las filas que devuelven, en orden, los fetchone() de cada
    execute() (None cuando se acaban); error, si existe, lo lanza cada
    execute(). drop() imita una conexión que el servidor cortó por
    inactividad: la siguiente sentencia falla y la conexión queda rota.
    """

    def __init__(self, rows=(), error: Exception | None = None):
        self.rows = list(rows)
        self.error = error
        self.statements: list[str] = []
        self.broken = False
        self.closed = False

    @contextlib.contextmanager
    def transaction(self):
        yield

    def execute(self, sql, params=None):
        self.statements.append(sql)
        if self.error is not None:
            if isinstance(self.error, psycopg.OperationalError):
                self.broken = True
            raise self.error
        return FakeCursor(self.rows.pop(0) if self.rows else None)

    def close(self):
        self.closed = True

    def drop(self):
        self.error = psycopg.OperationalError("server closed the connection unexpectedly")


def repository_with(conn: FakeConnection) -> PostgresSnapshotRepository:
    repository = PostgresSnapshotRepository("postgresql://nobody@127.0.0.1:1/none")
    repository._conn = conn
    return repository


@pytest.mark.parametrize(
    "error",
    [
        psycopg.errors.NumericValueOutOfRange("fuera de rango"),
        psycopg.errors.NotNullViolation("nulo"),
        psycopg.DataError("dato inválido"),
        psycopg.IntegrityError("restricción"),
    ],
)
def test_data_and_integrity_errors_become_invalid_snapshot(error):
    conn = FakeConnection(error=error)
    repository = repository_with(conn)
    with pytest.raises(InvalidSnapshotError):
        repository.save(snapshot(50))
    assert repository._conn is conn, "la conexión sigue sana"


def test_conflict_row_deleted_concurrently_retries_the_insert_once():
    # INSERT choca, el SELECT no encuentra la fila (la poda la borró a la
    # vez) y el segundo INSERT la guarda.
    conn = FakeConnection(rows=[None, None, (50,)])
    assert repository_with(conn).save(snapshot(50)) is SaveOutcome.INSERTED
    assert len(conn.statements) == 3


def test_conflict_row_deleted_twice_is_a_concurrent_change():
    conn = FakeConnection(rows=[None, None, None, None])
    with pytest.raises(ConcurrentSnapshotChangeError):
        repository_with(conn).save(snapshot(50))
    assert len(conn.statements) == 4


def fast_repository(**kwargs) -> PostgresSnapshotRepository:
    return PostgresSnapshotRepository(
        "postgresql://nobody@127.0.0.1:1/none",
        backoff_initial_s=0.01,
        backoff_max_s=0.02,
        **kwargs,
    )


def test_connector_survives_unexpected_errors_while_connecting():
    repository = fast_repository()
    attempts: list[int] = []

    def open_connection():
        attempts.append(1)
        if len(attempts) <= 2:
            raise RuntimeError("fallo inesperado al conectar")
        return FakeConnection()

    repository._open_connection = open_connection
    repository.start()
    try:
        assert repository.wait_ready(timeout_s=5)
        assert len(attempts) == 3
        assert repository._thread.is_alive()
    finally:
        repository.stop()


def test_connector_survives_a_failing_ready_callback():
    calls: list[bool] = []
    reconnected = threading.Event()

    def failing_callback(ready: bool) -> None:
        calls.append(ready)
        if calls == [True, False, True]:
            reconnected.set()
        raise RuntimeError("fallo en el aviso de salud")

    repository = fast_repository(on_ready_change=failing_callback)
    repository._open_connection = lambda: FakeConnection(
        error=psycopg.OperationalError("caída")
    )
    repository.start()
    try:
        assert repository.wait_ready(timeout_s=5)
        assert repository._thread.is_alive()
        # La conexión se pierde: el hilo, vivo, reconecta.
        repository._conn.broken = True
        with pytest.raises(RepositoryUnavailableError):
            repository.load_latest()
        assert reconnected.wait(timeout=5)
        assert repository.wait_ready(timeout_s=5)
    finally:
        repository.stop()


def wait_until(condition, timeout_s: float = 5.0) -> bool:
    deadline = time.monotonic() + timeout_s
    while time.monotonic() < deadline:
        if condition():
            return True
        time.sleep(0.005)
    return condition()


def test_liveness_ping_keeps_a_healthy_connection():
    readiness: list[bool] = []
    conn = FakeConnection()
    repository = fast_repository(on_ready_change=readiness.append, liveness_interval_s=0.01)
    repository._open_connection = lambda: conn
    repository.start()
    try:
        assert repository.wait_ready(timeout_s=5)
        assert wait_until(lambda: conn.statements.count("SELECT 1") >= 3)
        assert repository.is_ready
        assert repository._conn is conn
    finally:
        repository.stop()
    assert readiness == [True]


def test_liveness_ping_detects_an_idle_dropped_connection_and_reconnects():
    readiness: list[bool] = []
    first, second = FakeConnection(), FakeConnection()
    connections = [first, second]
    repository = fast_repository(on_ready_change=readiness.append, liveness_interval_s=0.01)
    repository._open_connection = lambda: connections.pop(0)
    repository.start()
    try:
        assert repository.wait_ready(timeout_s=5)
        # Nadie llama al repositorio: solo el ping puede notar la caída.
        first.drop()
        assert wait_until(lambda: repository._conn is second and repository.is_ready)
        assert first.closed
        assert wait_until(lambda: readiness == [True, False, True])
    finally:
        repository.stop()


def test_ready_callback_runs_outside_the_repository_lock():
    lock_held: list[bool] = []
    connections = [FakeConnection(), FakeConnection()]
    repository = fast_repository(liveness_interval_s=60)
    repository.on_ready_change = lambda ready: lock_held.append(repository._lock.locked())
    repository._open_connection = lambda: connections.pop(0)
    repository.start()
    try:
        assert repository.wait_ready(timeout_s=5)
        # Caída detectada por una operación (camino de _call).
        repository._conn.drop()
        with pytest.raises(RepositoryUnavailableError):
            repository.load_latest()
        assert wait_until(lambda: len(lock_held) == 3)
    finally:
        repository.stop()
    assert lock_held == [False, False, False]


def test_tcp_user_timeout_is_a_constructor_parameter(monkeypatch):
    captured: dict = {}

    def fake_connect(dsn, **kwargs):
        captured.update(kwargs)
        return FakeConnection()

    monkeypatch.setattr("persistence.adapters.postgres.psycopg.connect", fake_connect)
    monkeypatch.setattr("persistence.adapters.postgres.apply_migrations", lambda *_: [])
    repository = PostgresSnapshotRepository(
        "postgresql://nobody@127.0.0.1:1/none", tcp_user_timeout_ms=1234
    )
    repository._open_connection()
    assert captured["tcp_user_timeout"] == 1234


def test_tcp_user_timeout_defaults_to_five_seconds(monkeypatch):
    captured: dict = {}

    def fake_connect(dsn, **kwargs):
        captured.update(kwargs)
        return FakeConnection()

    monkeypatch.setattr("persistence.adapters.postgres.psycopg.connect", fake_connect)
    monkeypatch.setattr("persistence.adapters.postgres.apply_migrations", lambda *_: [])
    PostgresSnapshotRepository("postgresql://nobody@127.0.0.1:1/none")._open_connection()
    assert captured["tcp_user_timeout"] == 5000


# --- Con PostgreSQL real ---------------------------------------------------


@pytest.fixture
def clean_database():
    with psycopg.connect(TEST_DATABASE_URL, autocommit=True) as conn:
        conn.execute(
            "DROP TABLE IF EXISTS snapshot_players, snapshots, accounts, catalog_entries,"
            " schema_migrations"
        )
    yield TEST_DATABASE_URL


@pytest.fixture
def repository(clean_database):
    readiness: list[bool] = []
    repo = PostgresSnapshotRepository(
        clean_database,
        on_ready_change=readiness.append,
        backoff_initial_s=0.05,
        backoff_max_s=0.2,
        # Sin ping durante la prueba: así es la operación la que detecta la
        # caída en test_recovers_after_losing_the_connection.
        liveness_interval_s=60,
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
        assert apply_migrations(conn, DEFAULT_MIGRATIONS_DIR) == ["0001", "0002", "0003", "0004", "0005"]
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


@integration
def test_liveness_ping_detects_a_connection_the_server_dropped(clean_database):
    readiness: list[bool] = []
    repo = PostgresSnapshotRepository(
        clean_database,
        on_ready_change=readiness.append,
        backoff_initial_s=0.05,
        backoff_max_s=0.2,
        liveness_interval_s=0.1,
    )
    repo.start()
    try:
        assert repo.wait_ready(timeout_s=10)
        query(
            "SELECT pg_terminate_backend(pid) FROM pg_stat_activity "
            "WHERE application_name = 'hydrovalley-persistence'"
        )
        # Sin ninguna operación: el ping nota la caída y reconecta.
        deadline = time.monotonic() + 10
        while readiness != [True, False, True] and time.monotonic() < deadline:
            time.sleep(0.05)
        assert readiness == [True, False, True]
        assert repo.load_latest() is None
    finally:
        repo.stop()


@integration
def test_a_migration_longer_than_the_statement_timeout_succeeds(clean_database, tmp_path):
    shutil.copy(DEFAULT_MIGRATIONS_DIR / "0001_create_snapshots.sql", tmp_path)
    (tmp_path / "0002_slow.sql").write_text("SELECT pg_sleep(3);\n", encoding="utf-8")
    repo = PostgresSnapshotRepository(
        clean_database,
        migrations_dir=tmp_path,
        statement_timeout_ms=2000,
        backoff_initial_s=0.05,
        backoff_max_s=0.2,
    )
    repo.start()
    try:
        assert repo.wait_ready(timeout_s=15)
        assert query("SELECT version FROM schema_migrations ORDER BY version") == [
            ("0001",),
            ("0002",),
        ]
        # La conexión del servicio conserva su plazo de 2 s.
        assert repo._call(lambda conn: conn.execute("SHOW statement_timeout").fetchone()) == (
            "2s",
        )
    finally:
        repo.stop()


@integration
def test_values_the_schema_rejects_are_invalid_snapshot(repository):
    with pytest.raises(InvalidSnapshotError):
        repository.save(snapshot(50, epoch=2**31))
    with pytest.raises(InvalidSnapshotError):
        repository.save(Snapshot(tick=50, epoch=1, catalog_version="a\x00b", world_state=b""))
    assert repository.save(snapshot(50)) is SaveOutcome.INSERTED

"""Adaptador de PostgreSQL del puerto SnapshotRepository.

El servicio arranca aunque PostgreSQL no esté (portabilidad, regla 5): un
hilo en segundo plano se conecta con esperas crecientes, aplica las
migraciones y, si luego se pierde la conexión, vuelve a conectarse solo.
Mientras no hay conexión, cada operación lanza RepositoryUnavailableError,
que el servidor gRPC traduce a UNAVAILABLE.

Se usa una sola conexión protegida por un cerrojo: el simulador guarda una
instantánea cada 5 s y solo la carga al arrancar, así que no hace falta un
pool.
"""

import logging
import threading
from collections.abc import Callable
from decimal import Decimal
from pathlib import Path
from typing import TypeVar

import psycopg

from persistence.domain import (
    RepositoryUnavailableError,
    SaveOutcome,
    Snapshot,
    SnapshotConflictError,
)
from persistence.migrations import DEFAULT_MIGRATIONS_DIR, apply_migrations
from persistence.retention import ticks_to_prune

log = logging.getLogger(__name__)

APPLICATION_NAME = "hydrovalley-persistence"

T = TypeVar("T")


class PostgresSnapshotRepository:
    def __init__(
        self,
        dsn: str,
        *,
        migrations_dir: Path = DEFAULT_MIGRATIONS_DIR,
        on_ready_change: Callable[[bool], None] | None = None,
        connect_timeout_s: int = 2,
        statement_timeout_ms: int = 2000,
        backoff_initial_s: float = 0.5,
        backoff_max_s: float = 5.0,
    ) -> None:
        self._dsn = dsn
        self._migrations_dir = migrations_dir
        self.on_ready_change = on_ready_change
        self._connect_timeout_s = connect_timeout_s
        # Plazo por sentencia, en línea con el plazo de 2 s de las llamadas.
        self._statement_timeout_ms = statement_timeout_ms
        self._backoff_initial_s = backoff_initial_s
        self._backoff_max_s = backoff_max_s

        self._lock = threading.Lock()
        self._conn: psycopg.Connection | None = None
        self._need_connection = threading.Event()
        self._need_connection.set()
        self._ready = threading.Event()
        self._stopping = threading.Event()
        self._thread: threading.Thread | None = None

    # --- Ciclo de vida -----------------------------------------------------

    @property
    def is_ready(self) -> bool:
        return self._ready.is_set()

    def wait_ready(self, timeout_s: float) -> bool:
        return self._ready.wait(timeout_s)

    def start(self) -> None:
        self._thread = threading.Thread(target=self._connect_loop, name="pg-connector", daemon=True)
        self._thread.start()

    def stop(self) -> None:
        self._stopping.set()
        self._need_connection.set()
        if self._thread is not None:
            self._thread.join(timeout=self._connect_timeout_s + 1)
        with self._lock:
            if self._conn is not None:
                self._conn.close()
                self._conn = None
        self._ready.clear()

    def _connect_loop(self) -> None:
        delay = self._backoff_initial_s
        while True:
            self._need_connection.wait()
            if self._stopping.is_set():
                return
            try:
                conn = self._open_connection()
            except (psycopg.Error, OSError) as error:
                log.warning("PostgreSQL no disponible (%s); nuevo intento en %.1f s", error, delay)
                if self._stopping.wait(delay):
                    return
                delay = min(delay * 2, self._backoff_max_s)
                continue
            with self._lock:
                if self._stopping.is_set():
                    conn.close()
                    return
                self._conn = conn
                self._need_connection.clear()
                self._set_ready(True)
            delay = self._backoff_initial_s
            log.info("conectado a PostgreSQL")

    def _open_connection(self) -> psycopg.Connection:
        conn = psycopg.connect(
            self._dsn,
            autocommit=True,
            connect_timeout=self._connect_timeout_s,
            application_name=APPLICATION_NAME,
            options=f"-c statement_timeout={self._statement_timeout_ms}",
            # Si la red se corta sin aviso, la conexión se da por perdida a
            # los 5 s en vez de esperar al plazo de TCP del sistema.
            tcp_user_timeout=5000,
        )
        try:
            applied = apply_migrations(conn, self._migrations_dir)
        except BaseException:
            conn.close()
            raise
        if applied:
            log.info("migraciones aplicadas: %s", ", ".join(applied))
        return conn

    def _set_ready(self, ready: bool) -> None:
        if ready == self._ready.is_set():
            return
        if ready:
            self._ready.set()
        else:
            self._ready.clear()
        if self.on_ready_change is not None:
            self.on_ready_change(ready)

    def _call(self, work: Callable[[psycopg.Connection], T]) -> T:
        """Ejecuta work en una transacción con la conexión compartida."""
        with self._lock:
            conn = self._conn
            if conn is None:
                raise RepositoryUnavailableError("PostgreSQL no está conectado todavía")
            try:
                with conn.transaction():
                    return work(conn)
            except psycopg.OperationalError as error:
                # Un plazo agotado deja la conexión sana; una caída, no.
                if conn.broken or conn.closed:
                    log.warning("se perdió la conexión con PostgreSQL; reconectando")
                    conn.close()
                    self._conn = None
                    # Primero se marca no disponible y luego se despierta al
                    # hilo que reconecta, para que el orden de avisos sea el real.
                    self._set_ready(False)
                    self._need_connection.set()
                raise RepositoryUnavailableError(f"PostgreSQL no respondió: {error}") from error

    # --- Puerto SnapshotRepository ------------------------------------------

    def save(self, snapshot: Snapshot) -> SaveOutcome:
        def work(conn: psycopg.Connection) -> SaveOutcome:
            inserted = conn.execute(
                "INSERT INTO snapshots"
                " (tick, epoch, catalog_version, taken_at, world_state, world_state_sha256)"
                " VALUES (%s, %s, %s, now(), %s, %s)"
                " ON CONFLICT (tick) DO NOTHING RETURNING tick",
                (
                    Decimal(snapshot.tick),
                    snapshot.epoch,
                    snapshot.catalog_version,
                    snapshot.world_state,
                    snapshot.world_state_sha256,
                ),
            ).fetchone()
            if inserted is not None:
                return SaveOutcome.INSERTED
            epoch, sha256 = conn.execute(
                "SELECT epoch, world_state_sha256 FROM snapshots WHERE tick = %s",
                (Decimal(snapshot.tick),),
            ).fetchone()
            if epoch == snapshot.epoch and bytes(sha256) == snapshot.world_state_sha256:
                return SaveOutcome.ALREADY_STORED
            raise SnapshotConflictError(f"tick {snapshot.tick} ya existe con otro contenido")

        return self._call(work)

    def load_latest(self) -> Snapshot | None:
        def work(conn: psycopg.Connection) -> Snapshot | None:
            row = conn.execute(
                "SELECT tick, epoch, catalog_version, world_state FROM snapshots"
                " ORDER BY tick DESC LIMIT 1"
            ).fetchone()
            if row is None:
                return None
            tick, epoch, catalog_version, world_state = row
            return Snapshot(
                tick=int(tick),
                epoch=epoch,
                catalog_version=catalog_version,
                world_state=bytes(world_state),
            )

        return self._call(work)

    def prune(self) -> int:
        def work(conn: psycopg.Connection) -> int:
            ticks = [int(row[0]) for row in conn.execute("SELECT tick FROM snapshots")]
            doomed = sorted(ticks_to_prune(ticks))
            if not doomed:
                return 0
            conn.execute(
                "DELETE FROM snapshots WHERE tick = ANY(%s)",
                ([Decimal(tick) for tick in doomed],),
            )
            return len(doomed)

        return self._call(work)

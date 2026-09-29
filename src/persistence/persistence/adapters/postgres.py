"""Adaptador de PostgreSQL de los puertos SnapshotRepository y AccountRepository
(una sola conexión y un solo ciclo de vida para los dos).

El servicio arranca aunque PostgreSQL no esté (portabilidad, regla 5): un
hilo en segundo plano se conecta con esperas crecientes, aplica las
migraciones y, si luego se pierde la conexión, vuelve a conectarse solo.
Mientras no hay conexión, cada operación lanza RepositoryUnavailableError,
que el servidor gRPC traduce a UNAVAILABLE.

Mientras hay conexión, el mismo hilo la comprueba cada liveness_interval_s
(5 s por defecto) con un SELECT 1: así se detecta una conexión que el
servidor o la red cortaron por inactividad aunque nadie llame al servicio,
la salud pasa a NOT_SERVING y se reconecta.

El aviso on_ready_change se ejecuta siempre fuera del cerrojo del
repositorio: un aviso lento o que llame al propio repositorio no bloquea
las operaciones. Los avisos se serializan con un cerrojo propio y cada uno
publica el estado vigente en ese momento, así que el último aviso siempre
coincide con el estado real (dos cambios muy seguidos pueden fundirse).

Se usa una sola conexión protegida por un cerrojo: el simulador guarda una
instantánea cada 5 s y solo la carga al arrancar, así que no hace falta un
pool.
"""

import logging
import threading
from collections.abc import Callable
import uuid
from decimal import Decimal
from pathlib import Path
from typing import TypeVar

import psycopg

from persistence.domain import (
    Account,
    AccountAlreadyExistsError,
    AdminSeed,
    InvalidAccountError,
    ConcurrentSnapshotChangeError,
    InvalidSnapshotError,
    RepositoryUnavailableError,
    Role,
    SaveOutcome,
    Snapshot,
    SnapshotConflictError,
)
from persistence.migrations import (
    DEFAULT_MIGRATION_TIMEOUT_MS,
    DEFAULT_MIGRATIONS_DIR,
    apply_migrations,
)
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
        migration_timeout_ms: int = DEFAULT_MIGRATION_TIMEOUT_MS,
        backoff_initial_s: float = 0.5,
        backoff_max_s: float = 5.0,
        tcp_user_timeout_ms: int = 5000,
        liveness_interval_s: float = 5.0,
        admin_seed: AdminSeed | None = None,
    ) -> None:
        self._dsn = dsn
        # Cuenta de administrador que se crea, si no existe, tras migrar.
        self._admin_seed = admin_seed
        self._migrations_dir = migrations_dir
        self.on_ready_change = on_ready_change
        self._connect_timeout_s = connect_timeout_s
        # Plazo por sentencia, en línea con el plazo de 2 s de las llamadas.
        self._statement_timeout_ms = statement_timeout_ms
        # Las migraciones llevan su propio plazo (ver persistence.migrations).
        self._migration_timeout_ms = migration_timeout_ms
        self._backoff_initial_s = backoff_initial_s
        self._backoff_max_s = backoff_max_s
        # Si la red se corta sin aviso, la conexión se da por perdida a los
        # tcp_user_timeout_ms en vez de esperar al plazo de TCP del sistema.
        self._tcp_user_timeout_ms = tcp_user_timeout_ms
        # Cada cuánto se comprueba la conexión cuando no hay que reconectar.
        self._liveness_interval_s = liveness_interval_s

        self._lock = threading.Lock()
        # Serializa los avisos de disponibilidad, que van fuera de _lock.
        self._notify_lock = threading.Lock()
        self._notified_ready = False
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
            if not self._need_connection.wait(self._liveness_interval_s):
                # Conectado y sin nada que hacer: se comprueba la conexión.
                self._check_liveness()
                continue
            if self._stopping.is_set():
                return
            # Cualquier excepción (no solo las de red) se registra y se
            # reintenta: si el hilo muriera, el servicio no volvería a
            # conectarse nunca y nadie lo notaría.
            try:
                conn = self._open_connection()
            except Exception as error:
                if isinstance(error, (psycopg.Error, OSError)):
                    log.warning("PostgreSQL no disponible (%s); nuevo intento en %.1f s", error, delay)
                else:
                    log.exception("error inesperado al conectar; nuevo intento en %.1f s", delay)
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
                self._ready.set()
            self._publish_readiness()
            delay = self._backoff_initial_s
            log.info("conectado a PostgreSQL")

    def _open_connection(self) -> psycopg.Connection:
        conn = psycopg.connect(
            self._dsn,
            autocommit=True,
            connect_timeout=self._connect_timeout_s,
            application_name=APPLICATION_NAME,
            options=f"-c statement_timeout={self._statement_timeout_ms}",
            tcp_user_timeout=self._tcp_user_timeout_ms,
        )
        try:
            applied = apply_migrations(conn, self._migrations_dir, self._migration_timeout_ms)
        except BaseException:
            conn.close()
            raise
        if applied:
            log.info("migraciones aplicadas: %s", ", ".join(applied))
        # La siembra va antes de publicar la conexión: la salud solo pasa a
        # SERVING con el administrador ya creado y, si falla, se reintenta
        # con el resto de la conexión.
        try:
            if self._admin_seed is not None:
                self._seed_admin(conn, self._admin_seed)
        except BaseException:
            conn.close()
            raise
        return conn

    @staticmethod
    def _seed_admin(conn: psycopg.Connection, seed: AdminSeed) -> None:
        """Crea la cuenta de administrador si no existe. Idempotente: nunca
        modifica una cuenta existente. No registra el hash."""
        with conn.transaction():
            inserted = conn.execute(
                "INSERT INTO accounts (id, username, password_hash, role)"
                " VALUES (%s, %s, %s, 'admin') ON CONFLICT DO NOTHING RETURNING id",
                (uuid.uuid4(), seed.username, seed.password_hash),
            ).fetchone()
            if inserted is not None:
                log.info("cuenta de administrador %r creada", seed.username)
                return
            row = conn.execute(
                "SELECT role FROM accounts WHERE lower(username) = lower(%s)",
                (seed.username,),
            ).fetchone()
        if row is not None and row[0] != Role.ADMIN.value:
            log.warning(
                "la cuenta %r ya existe con el rol %s: no se modifica", seed.username, row[0]
            )
        else:
            log.info("cuenta de administrador %r ya existía", seed.username)

    def _publish_readiness(self) -> None:
        """Avisa del estado vigente si cambió desde el último aviso.

        Se llama siempre fuera de _lock.
        """
        with self._notify_lock:
            ready = self._ready.is_set()
            if ready == self._notified_ready:
                return
            self._notified_ready = ready
            if self.on_ready_change is None:
                return
            # Un fallo del aviso no debe romper ni el hilo que conecta ni la
            # operación que detectó la caída.
            try:
                self.on_ready_change(ready)
            except Exception:
                log.exception("falló el aviso de cambio de disponibilidad (%s)", ready)

    def _drop_connection_locked(self, conn: psycopg.Connection) -> None:
        """Descarta la conexión perdida. Se llama con _lock tomado.

        Quien llama debe luego avisar (_publish_readiness) y despertar al
        hilo que reconecta (_need_connection.set()), en ese orden y fuera
        del cerrojo, para que el orden de avisos sea el real.
        """
        conn.close()
        self._conn = None
        self._ready.clear()

    def _connection_lost(self) -> None:
        self._publish_readiness()
        self._need_connection.set()

    def _check_liveness(self) -> None:
        with self._lock:
            conn = self._conn
            if conn is None:
                return
            try:
                conn.execute("SELECT 1")
                return
            except psycopg.Error as error:
                # Cualquier fallo del ping se trata como conexión perdida:
                # reconectar es barato y una conexión dudosa no sirve.
                log.warning("la comprobación de PostgreSQL falló (%s); reconectando", error)
                self._drop_connection_locked(conn)
        self._connection_lost()

    def _call(
        self,
        work: Callable[[psycopg.Connection], T],
        invalid: type[Exception] = InvalidSnapshotError,
    ) -> T:
        """Ejecuta work en una transacción con la conexión compartida.

        invalid es la excepción del dominio en la que se convierten los datos
        que el esquema no admite (según la operación: instantánea o cuenta).
        """
        lost: RepositoryUnavailableError | None = None
        with self._lock:
            conn = self._conn
            if conn is None:
                raise RepositoryUnavailableError("PostgreSQL no está conectado todavía")
            try:
                with conn.transaction():
                    return work(conn)
            except psycopg.OperationalError as error:
                unavailable = RepositoryUnavailableError(f"PostgreSQL no respondió: {error}")
                # Un plazo agotado deja la conexión sana; una caída, no.
                if not (conn.broken or conn.closed):
                    raise unavailable from error
                log.warning("se perdió la conexión con PostgreSQL; reconectando")
                self._drop_connection_locked(conn)
                unavailable.__cause__ = error
                lost = unavailable
            except (psycopg.DataError, psycopg.IntegrityError) as error:
                # Datos que el esquema no admite: la transacción se deshizo y
                # la conexión sigue sana.
                raise invalid(str(error)) from error
        # Caída: se avisa y se despierta al hilo que reconecta fuera del
        # cerrojo, y solo entonces se informa del error a quien llamó.
        assert lost is not None  # solo se llega aquí tras una caída
        self._connection_lost()
        raise lost

    # --- Puerto SnapshotRepository ------------------------------------------

    def save(self, snapshot: Snapshot) -> SaveOutcome:
        params = (
            Decimal(snapshot.tick),
            snapshot.epoch,
            snapshot.catalog_version,
            snapshot.world_state,
            snapshot.world_state_sha256,
        )

        def work(conn: psycopg.Connection) -> SaveOutcome:
            # Si la fila que choca desaparece antes de leerla (la poda de
            # retención la borró a la vez), se reintenta el INSERT una vez:
            # así el guardado sigue siendo idempotente. Si vuelve a pasar,
            # ABORTED (ConcurrentSnapshotChangeError): el simulador lo
            # registra y lo reintenta en el siguiente tick de instantánea.
            for _ in range(2):
                inserted = conn.execute(
                    "INSERT INTO snapshots"
                    " (tick, epoch, catalog_version, taken_at, world_state, world_state_sha256)"
                    " VALUES (%s, %s, %s, now(), %s, %s)"
                    " ON CONFLICT (tick) DO NOTHING RETURNING tick",
                    params,
                ).fetchone()
                if inserted is not None:
                    return SaveOutcome.INSERTED
                existing = conn.execute(
                    "SELECT epoch, world_state_sha256 FROM snapshots WHERE tick = %s",
                    (Decimal(snapshot.tick),),
                ).fetchone()
                if existing is None:
                    continue
                epoch, sha256 = existing
                if epoch == snapshot.epoch and bytes(sha256) == snapshot.world_state_sha256:
                    return SaveOutcome.ALREADY_STORED
                raise SnapshotConflictError(f"tick {snapshot.tick} ya existe con otro contenido")
            raise ConcurrentSnapshotChangeError(
                f"la fila del tick {snapshot.tick} cambió mientras se guardaba"
            )

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

    # --- Puerto AccountRepository -------------------------------------------

    def register(self, username: str, password_hash: str) -> Account:
        account_id = uuid.uuid4()

        def work(conn: psycopg.Connection) -> Account:
            # Sin objetivo en ON CONFLICT: cubre el índice único de
            # lower(username) y, en teoría, el de la clave primaria.
            inserted = conn.execute(
                "INSERT INTO accounts (id, username, password_hash, role)"
                " VALUES (%s, %s, %s, 'player') ON CONFLICT DO NOTHING RETURNING id",
                (account_id, username, password_hash),
            ).fetchone()
            if inserted is None:
                raise AccountAlreadyExistsError(f"la cuenta {username} ya existe")
            return Account(account_id, username, password_hash, Role.PLAYER)

        return self._call(work, invalid=InvalidAccountError)

    def get_by_username(self, username: str) -> Account | None:
        def work(conn: psycopg.Connection) -> Account | None:
            row = conn.execute(
                "SELECT id, username, password_hash, role FROM accounts"
                " WHERE lower(username) = lower(%s)",
                (username,),
            ).fetchone()
            if row is None:
                return None
            account_id, stored_username, password_hash, role = row
            return Account(account_id, stored_username, password_hash, Role(role))

        return self._call(work, invalid=InvalidAccountError)

    def get_usernames(self, player_ids: list[uuid.UUID]) -> dict[uuid.UUID, str]:
        def work(conn: psycopg.Connection) -> dict[uuid.UUID, str]:
            rows = conn.execute(
                "SELECT id, username FROM accounts WHERE id = ANY(%s)",
                (list(set(player_ids)),),
            ).fetchall()
            return {account_id: username for account_id, username in rows}

        return self._call(work, invalid=InvalidAccountError)

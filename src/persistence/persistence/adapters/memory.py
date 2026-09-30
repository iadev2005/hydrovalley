"""Repositorios en memoria: dobles de pruebas de los puertos SnapshotRepository
y AccountRepository."""

import threading
import uuid

from persistence.catalog import Catalog, CatalogEntry
from persistence.domain import (
    Account,
    AccountAlreadyExistsError,
    RepositoryUnavailableError,
    Role,
    SaveOutcome,
    Snapshot,
    SnapshotConflictError,
)
from persistence.retention import ticks_to_prune


class InMemorySnapshotRepository:
    def __init__(self) -> None:
        self._rows: dict[int, Snapshot] = {}
        self._lock = threading.Lock()
        # Interruptores para simular fallos en las pruebas.
        self.available = True
        self.fail_prune = False
        # Si existen, save o load_latest lanzan esta excepción.
        self.save_error: Exception | None = None
        self.load_error: Exception | None = None

    def _check_available(self) -> None:
        if not self.available:
            raise RepositoryUnavailableError("repositorio en memoria desactivado")

    def save(self, snapshot: Snapshot) -> SaveOutcome:
        self._check_available()
        if self.save_error is not None:
            raise self.save_error
        with self._lock:
            existing = self._rows.get(snapshot.tick)
            if existing is None:
                self._rows[snapshot.tick] = snapshot
                return SaveOutcome.INSERTED
            same = (
                existing.epoch == snapshot.epoch
                and existing.world_state_sha256 == snapshot.world_state_sha256
            )
            if not same:
                raise SnapshotConflictError(f"tick {snapshot.tick} ya existe con otro contenido")
            return SaveOutcome.ALREADY_STORED

    def load_latest(self) -> Snapshot | None:
        self._check_available()
        if self.load_error is not None:
            raise self.load_error
        with self._lock:
            return self._rows[max(self._rows)] if self._rows else None

    def prune(self) -> int:
        self._check_available()
        if self.fail_prune:
            raise RepositoryUnavailableError("poda simulada fallida")
        with self._lock:
            doomed = ticks_to_prune(self._rows)
            for tick in doomed:
                del self._rows[tick]
            return len(doomed)

    def ticks(self) -> list[int]:
        with self._lock:
            return sorted(self._rows)


class InMemoryAccountRepository:
    """Doble de pruebas del puerto AccountRepository."""

    def __init__(self) -> None:
        self._accounts: dict[str, Account] = {}  # clave: nombre en minúsculas
        self._lock = threading.Lock()
        # Interruptores para simular fallos en las pruebas.
        self.available = True
        # Si existe, cada operación lanza esta excepción.
        self.error: Exception | None = None

    def _check(self) -> None:
        if not self.available:
            raise RepositoryUnavailableError("repositorio en memoria desactivado")
        if self.error is not None:
            raise self.error

    def register(self, username: str, password_hash: str) -> Account:
        return self._insert(username, password_hash, Role.PLAYER)

    def add_admin(self, username: str, password_hash: str) -> Account:
        """Siembra una cuenta de administrador (solo para las pruebas)."""
        return self._insert(username, password_hash, Role.ADMIN)

    def _insert(self, username: str, password_hash: str, role: Role) -> Account:
        self._check()
        with self._lock:
            key = username.lower()
            if key in self._accounts:
                raise AccountAlreadyExistsError(f"la cuenta {username} ya existe")
            account = Account(uuid.uuid4(), username, password_hash, role)
            self._accounts[key] = account
            return account

    def get_by_username(self, username: str) -> Account | None:
        self._check()
        with self._lock:
            return self._accounts.get(username.lower())

    def get_usernames(self, player_ids: list[uuid.UUID]) -> dict[uuid.UUID, str]:
        self._check()
        wanted = set(player_ids)
        with self._lock:
            return {a.id: a.username for a in self._accounts.values() if a.id in wanted}


class InMemoryCatalogRepository:
    """Doble de pruebas del puerto CatalogRepository."""

    def __init__(self) -> None:
        self._entries: list[CatalogEntry] = []
        self._lock = threading.Lock()
        # Interruptores para simular fallos en las pruebas.
        self.available = True
        # Si existe, load_entries lanza esta excepción.
        self.error: Exception | None = None

    def replace(self, catalog: Catalog) -> None:
        """Siembra un catálogo validado (lo que hace el adaptador real al conectar)."""
        self.set_raw(list(catalog.entries))

    def set_raw(self, entries: list[CatalogEntry]) -> None:
        """Guarda filas sin validar (para probar un contenido corrupto)."""
        with self._lock:
            self._entries = list(entries)

    def clear(self) -> None:
        self.set_raw([])

    def load_entries(self) -> list[CatalogEntry]:
        if not self.available:
            raise RepositoryUnavailableError("repositorio en memoria desactivado")
        if self.error is not None:
            raise self.error
        with self._lock:
            return list(self._entries)

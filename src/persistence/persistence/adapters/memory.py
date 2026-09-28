"""Repositorio en memoria: doble de pruebas del puerto SnapshotRepository."""

import threading

from persistence.domain import (
    RepositoryUnavailableError,
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

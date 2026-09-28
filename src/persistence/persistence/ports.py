"""Puertos de la persistencia: lo que el servidor gRPC necesita del almacén."""

from typing import Protocol

from persistence.domain import SaveOutcome, Snapshot


class SnapshotRepository(Protocol):
    def save(self, snapshot: Snapshot) -> SaveOutcome:
        """Guarda la instantánea en una sola transacción.

        Lanza SnapshotConflictError si el tick ya existe con otra época u otro
        world_state, y RepositoryUnavailableError si no hay base de datos.
        """
        ...

    def load_latest(self) -> Snapshot | None:
        """Devuelve la instantánea de mayor tick, o None si no hay ninguna."""
        ...

    def prune(self) -> int:
        """Borra, en su propia transacción, las instantáneas fuera de la
        retención (persistence.retention). Devuelve cuántas borró."""
        ...

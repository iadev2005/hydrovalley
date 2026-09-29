"""Puertos de la persistencia: lo que el servidor gRPC necesita del almacén."""

import uuid
from typing import Protocol

from persistence.domain import Account, SaveOutcome, Snapshot


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


class AccountRepository(Protocol):
    def register(self, username: str, password_hash: str) -> Account:
        """Crea una cuenta de jugador con un UUID nuevo. El llamador ya validó
        los datos.

        Lanza AccountAlreadyExistsError si el nombre existe (sin distinguir
        mayúsculas) y RepositoryUnavailableError si no hay base de datos.
        """
        ...

    def get_by_username(self, username: str) -> Account | None:
        """Busca por nombre de usuario sin distinguir mayúsculas; None si no existe."""
        ...

    def get_usernames(self, player_ids: list[uuid.UUID]) -> dict[uuid.UUID, str]:
        """Nombres de las cuentas que existen; los identificadores
        desconocidos no aparecen."""
        ...

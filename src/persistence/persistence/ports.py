"""Puertos de la persistencia: lo que el servidor gRPC necesita del almacén."""

import uuid
from typing import Protocol

from persistence.catalog import CatalogEntry
from persistence.domain import Account, Report, SaveOutcome, Snapshot


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


class CatalogRepository(Protocol):
    def load_entries(self) -> list[CatalogEntry]:
        """Todas las filas de `catalog_entries`.

        Lanza RepositoryUnavailableError si no hay base de datos. El catálogo
        lo siembra el adaptador al conectar, desde el archivo de `catalog/`.
        """
        ...


class ReportRepository(Protocol):
    def report(
        self, player_id: uuid.UUID | None, from_tick: int | None, to_tick: int | None
    ) -> Report:
        """Reporte de las 3R de la época actual (ver persistence.reports).

        player_id None = global (suma de todas las filas). Lee todo en una
        sola transacción coherente.

        Lanza NoSnapshotError si no hay ninguna instantánea, UnknownPlayerError
        si no existe esa cuenta, InvalidReportPeriodError si el periodo no es
        válido y RepositoryUnavailableError si no hay base de datos. Una cuenta
        que existe pero no tiene fila en las instantáneas devuelve ceros.
        """
        ...

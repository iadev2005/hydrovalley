"""Modelo del dominio de la persistencia: instantáneas y sus errores."""

import enum
import hashlib
from dataclasses import dataclass


@dataclass(frozen=True)
class Snapshot:
    """Fila de `snapshots` (diagrama 07). world_state es opaco para Python."""

    tick: int
    epoch: int
    catalog_version: str
    world_state: bytes

    @property
    def world_state_sha256(self) -> bytes:
        return hashlib.sha256(self.world_state).digest()


class SaveOutcome(enum.Enum):
    """Resultado de un guardado correcto."""

    INSERTED = "inserted"
    # Ya había una fila con el mismo tick, época y world_state: un reintento.
    ALREADY_STORED = "already_stored"


class SnapshotConflictError(Exception):
    """Ya hay una instantánea con ese tick y contenido distinto."""


class RepositoryUnavailableError(Exception):
    """La base de datos no está disponible todavía (o se perdió la conexión)."""

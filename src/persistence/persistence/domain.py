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


class InvalidSnapshotError(Exception):
    """Los datos de la instantánea no caben en el esquema (valor fuera de
    rango, carácter no admitido, restricción violada)."""


class ConcurrentSnapshotChangeError(Exception):
    """La fila de ese tick cambió a la vez que se guardaba (p. ej. la poda de
    retención la borró entre el INSERT y la lectura) y el reintento tampoco
    pudo resolverlo."""

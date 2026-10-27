"""Modelo del dominio de la persistencia: instantáneas, cuentas y sus errores."""

import enum
import hashlib
import re
import uuid
from dataclasses import dataclass


@dataclass(frozen=True)
class SnapshotPlayer:
    """Fila de `snapshot_players` (diagrama 07): contadores de un jugador en
    una instantánea. Todos son u64, salvo los del resumen offline que siguen
    siendo contadores del mundo y caben sin problema en u32."""

    player_id: uuid.UUID
    water: int = 0
    leaves: int = 0
    water_refilled: int = 0
    water_spent: int = 0
    leaves_earned: int = 0
    leaves_spent: int = 0
    water_reduced: int = 0
    water_reused: int = 0
    water_recycled: int = 0
    fires_extinguished: int = 0
    tiles_grown: int = 0


@dataclass(frozen=True)
class Snapshot:
    """Fila de `snapshots` (diagrama 07) y sus filas de `snapshot_players`.
    world_state es opaco para Python. `players` solo se rellena al guardar:
    load_latest no lo lee (el simulador recupera todo de world_state)."""

    tick: int
    epoch: int
    catalog_version: str
    world_state: bytes
    players: tuple[SnapshotPlayer, ...] = ()

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


# --- Reportes (RF-12) -------------------------------------------------------


@dataclass(frozen=True)
class ReportTotals:
    """Totales de un reporte: contadores acumulados de las 3R, saldos de
    RF-27 y resumen offline del “día siguiente”."""

    water_reduced: int
    water_reused: int
    water_recycled: int
    water_refilled: int
    water_spent: int
    leaves_earned: int
    leaves_spent: int
    fires_extinguished: int = 0
    tiles_grown: int = 0

    def minus(self, other: "ReportTotals") -> "ReportTotals":
        """Diferencia campo a campo, sin bajar de cero (los contadores solo
        crecen dentro de una época; el tope es una defensa)."""
        return ReportTotals(
            **{
                name: max(0, getattr(self, name) - getattr(other, name))
                for name in self.__dataclass_fields__
            }
        )


@dataclass(frozen=True)
class Report:
    """Resultado de GetReport. player_id es None en el reporte global.
    from_tick = 0 significa el inicio de la época (contadores a cero)."""

    player_id: uuid.UUID | None
    epoch: int
    from_tick: int
    to_tick: int
    totals: ReportTotals


class NoSnapshotError(Exception):
    """No hay ninguna instantánea (y por tanto tampoco época actual)."""


class UnknownPlayerError(Exception):
    """No existe una cuenta con ese player_id."""


class InvalidReportPeriodError(Exception):
    """El periodo pedido queda fuera de la época actual o está invertido."""


# --- Cuentas ----------------------------------------------------------------

# Nombre de usuario: 3 a 32 caracteres ASCII. Se restringe el alfabeto para
# que la comparación sin distinguir mayúsculas (lower() en PostgreSQL, que
# depende de la configuración regional) sea inequívoca.
USERNAME_PATTERN = re.compile(r"[A-Za-z0-9_.-]{3,32}")

# Hash en formato PHC de Argon2id (RNF-12): `$argon2id$v=19$m=…,t=…,p=…$sal$hash`.
# Solo se comprueba la forma: verificarlo es cosa de la API. ASCII imprimible
# sin espacios, con un tope holgado (un hash típico mide unos 100 caracteres).
PASSWORD_HASH_PATTERN = re.compile(r"\$argon2id\$[\x21-\x7e]+")
MAX_PASSWORD_HASH_LENGTH = 512


class Role(enum.Enum):
    """Rol de una cuenta; el valor es el que guarda la columna `role`."""

    PLAYER = "player"
    ADMIN = "admin"


@dataclass(frozen=True)
class Account:
    """Fila de `accounts` (diagrama 07)."""

    id: uuid.UUID
    username: str
    password_hash: str
    role: Role


@dataclass(frozen=True)
class AdminSeed:
    """Cuenta de administrador que se crea al arrancar (ADMIN_USERNAME y
    ADMIN_PASSWORD_HASH)."""

    username: str
    password_hash: str


def username_problem(username: str) -> str | None:
    """Motivo por el que el nombre no es válido, o None."""
    if USERNAME_PATTERN.fullmatch(username) is None:
        return "username debe tener de 3 a 32 caracteres entre A-Z, a-z, 0-9, _, . y -"
    return None


def is_valid_username(username: str) -> bool:
    return username_problem(username) is None


def password_hash_problem(password_hash: str) -> str | None:
    """Motivo por el que el hash no parece un Argon2id en formato PHC, o None.
    Nunca incluye el valor recibido: podría ser una contraseña en claro."""
    if not password_hash:
        return "password_hash está vacío"
    if len(password_hash) > MAX_PASSWORD_HASH_LENGTH:
        return f"password_hash tiene más de {MAX_PASSWORD_HASH_LENGTH} caracteres"
    if PASSWORD_HASH_PATTERN.fullmatch(password_hash) is None:
        return "password_hash debe ser un hash Argon2id en formato PHC ($argon2id$…)"
    return None


class AccountAlreadyExistsError(Exception):
    """Ya hay una cuenta con ese nombre de usuario (sin distinguir mayúsculas)."""


class InvalidAccountError(Exception):
    """Los datos de la cuenta no caben en el esquema (carácter no admitido,
    restricción violada)."""

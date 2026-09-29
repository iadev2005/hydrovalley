"""Modelo del dominio de la persistencia: instantáneas, cuentas y sus errores."""

import enum
import hashlib
import re
import uuid
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

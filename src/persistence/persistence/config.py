"""Configuración leída de variables de entorno (portabilidad, regla 1)."""

from collections.abc import Mapping
from dataclasses import dataclass

from persistence.domain import AdminSeed, password_hash_problem, username_problem

DEFAULT_LISTEN = "0.0.0.0:50051"


class ConfigError(Exception):
    """Falta o es inválida una variable de entorno."""


@dataclass(frozen=True)
class Settings:
    listen: str
    database_url: str
    # None si no se definieron ADMIN_USERNAME y ADMIN_PASSWORD_HASH.
    admin_seed: AdminSeed | None = None

    @classmethod
    def from_env(cls, env: Mapping[str, str]) -> "Settings":
        # Sin valor por defecto: las credenciales nunca van en el código (RNF-13).
        database_url = env.get("DATABASE_URL", "").strip()
        if not database_url:
            raise ConfigError("DATABASE_URL es obligatoria (p. ej. postgresql://usuario:clave@db:5432/hydrovalley)")
        listen = env.get("PERSISTENCE_LISTEN", "").strip() or DEFAULT_LISTEN
        return cls(
            listen=listen,
            database_url=database_url,
            admin_seed=_admin_seed(env),
        )


def _admin_seed(env: Mapping[str, str]) -> AdminSeed | None:
    """Lee la cuenta de administrador: o están las dos variables o ninguna.
    Los mensajes nunca incluyen el hash."""
    username = env.get("ADMIN_USERNAME", "").strip()
    password_hash = env.get("ADMIN_PASSWORD_HASH", "").strip()
    if not username and not password_hash:
        return None
    if not username or not password_hash:
        raise ConfigError(
            "ADMIN_USERNAME y ADMIN_PASSWORD_HASH van juntas: define las dos o ninguna"
        )
    if (problem := username_problem(username)) is not None:
        raise ConfigError(f"ADMIN_USERNAME inválido: {problem}")
    if (problem := password_hash_problem(password_hash)) is not None:
        raise ConfigError(f"ADMIN_PASSWORD_HASH inválido: {problem}")
    return AdminSeed(username=username, password_hash=password_hash)

"""Configuración leída de variables de entorno (portabilidad, regla 1)."""

from collections.abc import Mapping
from dataclasses import dataclass

DEFAULT_LISTEN = "0.0.0.0:50051"


class ConfigError(Exception):
    """Falta o es inválida una variable de entorno."""


@dataclass(frozen=True)
class Settings:
    listen: str
    database_url: str

    @classmethod
    def from_env(cls, env: Mapping[str, str]) -> "Settings":
        # Sin valor por defecto: las credenciales nunca van en el código (RNF-13).
        database_url = env.get("DATABASE_URL", "").strip()
        if not database_url:
            raise ConfigError("DATABASE_URL es obligatoria (p. ej. postgresql://usuario:clave@db:5432/hydrovalley)")
        listen = env.get("PERSISTENCE_LISTEN", "").strip() or DEFAULT_LISTEN
        return cls(listen=listen, database_url=database_url)

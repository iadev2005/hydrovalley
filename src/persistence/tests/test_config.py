"""Pruebas de la configuración leída del entorno."""

import pytest

from persistence.config import ConfigError, Settings


def test_database_url_is_required():
    with pytest.raises(ConfigError, match="DATABASE_URL"):
        Settings.from_env({})


def test_listen_address_has_a_default():
    settings = Settings.from_env({"DATABASE_URL": "postgresql://u@db:5432/hv"})
    assert settings.listen == "0.0.0.0:50051"
    assert settings.database_url == "postgresql://u@db:5432/hv"


def test_listen_address_comes_from_the_environment():
    settings = Settings.from_env(
        {"DATABASE_URL": "postgresql://u@db/hv", "PERSISTENCE_LISTEN": "0.0.0.0:6000"}
    )
    assert settings.listen == "0.0.0.0:6000"

"""Pruebas de la configuración leída del entorno."""

import pytest

from persistence.config import ConfigError, Settings
from persistence.domain import AdminSeed


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


DB = {"DATABASE_URL": "postgresql://u@db/hv"}
ADMIN_HASH = "$argon2id$v=19$m=19456,t=2,p=1$c29tZXNhbHQ$aGFzaGhhc2hoYXNo"


def test_admin_seed_is_optional():
    settings = Settings.from_env(DB)
    assert settings.admin_seed is None


def test_admin_seed_comes_from_both_variables():
    settings = Settings.from_env(
        {**DB, "ADMIN_USERNAME": " root ", "ADMIN_PASSWORD_HASH": ADMIN_HASH}
    )
    assert settings.admin_seed == AdminSeed(username="root", password_hash=ADMIN_HASH)


def test_blank_admin_variables_count_as_unset():
    settings = Settings.from_env({**DB, "ADMIN_USERNAME": "", "ADMIN_PASSWORD_HASH": "  "})
    assert settings.admin_seed is None


@pytest.mark.parametrize(
    "env",
    [{"ADMIN_USERNAME": "root"}, {"ADMIN_PASSWORD_HASH": ADMIN_HASH}],
)
def test_only_one_admin_variable_is_an_error_naming_both(env):
    with pytest.raises(ConfigError) as error:
        Settings.from_env({**DB, **env})
    assert "ADMIN_USERNAME" in str(error.value)
    assert "ADMIN_PASSWORD_HASH" in str(error.value)


def test_invalid_admin_username_is_an_error():
    with pytest.raises(ConfigError, match="ADMIN_USERNAME"):
        Settings.from_env({**DB, "ADMIN_USERNAME": "a b", "ADMIN_PASSWORD_HASH": ADMIN_HASH})


def test_invalid_admin_hash_is_an_error_that_does_not_print_it():
    secret = "clave-en-claro"
    with pytest.raises(ConfigError, match="ADMIN_PASSWORD_HASH") as error:
        Settings.from_env({**DB, "ADMIN_USERNAME": "root", "ADMIN_PASSWORD_HASH": secret})
    assert secret not in str(error.value)

"""Pruebas de las reglas de validación de cuentas (función pura)."""

import pytest

from persistence.domain import (
    is_valid_username,
    password_hash_problem,
    username_problem,
)

VALID_HASH = "$argon2id$v=19$m=19456,t=2,p=1$c29tZXNhbHQ$aGFzaGhhc2hoYXNo"


@pytest.mark.parametrize("username", ["ana", "Ana_01", "a.b-c", "x" * 32, "A1b"])
def test_valid_usernames(username):
    assert username_problem(username) is None
    assert is_valid_username(username)


@pytest.mark.parametrize(
    "username",
    ["", "ab", "x" * 33, "con espacio", "ñandú", "a@b", "ana\n", "ana\x00", "  ana", "a/b"],
)
def test_invalid_usernames(username):
    assert username_problem(username) is not None
    assert not is_valid_username(username)


def test_valid_password_hash():
    assert password_hash_problem(VALID_HASH) is None


@pytest.mark.parametrize(
    "value",
    [
        "",
        "hunter2",
        "$argon2i$v=19$m=1,t=1,p=1$c2FsdA$aGFzaA",
        "$argon2id$",
        "$argon2id$v=19$con espacio",
        "$argon2id$v=19$m=1,t=1,p=1$c2FsdA$aGFzaA\n",
        "$argon2id$v=19$\x00",
        "$argon2id$" + "a" * 600,
    ],
)
def test_invalid_password_hashes(value):
    assert password_hash_problem(value) is not None

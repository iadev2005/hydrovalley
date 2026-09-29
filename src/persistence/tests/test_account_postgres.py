"""Pruebas del adaptador de PostgreSQL para cuentas.

Las de integración reutilizan los fixtures de test_postgres y solo corren
con TEST_DATABASE_URL (base de datos desechable).
"""

import uuid

import psycopg
import pytest

from persistence.adapters.postgres import PostgresSnapshotRepository
from persistence.domain import (
    AccountAlreadyExistsError,
    AdminSeed,
    InvalidAccountError,
    Role,
)
from tests.test_postgres import (  # noqa: F401 (fixtures de pytest)
    FakeConnection,
    clean_database,
    integration,
    query,
    repository,
    repository_with,
)

HASH = "$argon2id$v=19$m=19456,t=2,p=1$c29tZXNhbHQ$aGFzaGhhc2hoYXNo"


# --- Sin base de datos ------------------------------------------------------


@pytest.mark.parametrize(
    "error",
    [
        psycopg.errors.NotNullViolation("nulo"),
        psycopg.errors.CheckViolation("restricción"),
        psycopg.DataError("dato inválido"),
    ],
)
def test_data_errors_on_accounts_become_invalid_account(error):
    repository = repository_with(FakeConnection(error=error))
    with pytest.raises(InvalidAccountError):
        repository.register("ana", HASH)
    with pytest.raises(InvalidAccountError):
        repository.get_by_username("ana")
    with pytest.raises(InvalidAccountError):
        repository.get_usernames([uuid.uuid4()])


def test_a_failing_admin_seed_fails_the_connection_attempt(monkeypatch):
    # Si la siembra falla, la conexión no se publica: el hilo reintenta.
    conn = FakeConnection(error=psycopg.OperationalError("caída"))
    monkeypatch.setattr(psycopg, "connect", lambda *a, **k: conn)
    monkeypatch.setattr("persistence.adapters.postgres.apply_migrations", lambda *a, **k: [])
    repository = PostgresSnapshotRepository(
        "postgresql://nobody@127.0.0.1:1/none",
        admin_seed=AdminSeed("root", HASH),
    )
    with pytest.raises(psycopg.OperationalError):
        repository._open_connection()
    assert conn.closed


# --- Con PostgreSQL real ----------------------------------------------------


@integration
def test_migration_creates_accounts_table(repository):
    columns = query(
        "SELECT column_name, data_type, is_nullable FROM information_schema.columns "
        "WHERE table_name = 'accounts' ORDER BY ordinal_position"
    )
    assert columns == [
        ("id", "uuid", "NO"),
        ("username", "text", "NO"),
        ("password_hash", "text", "NO"),
        ("role", "text", "NO"),
        ("created_at", "timestamp with time zone", "NO"),
    ]


@integration
def test_role_column_only_admits_player_and_admin(repository):
    with pytest.raises(psycopg.errors.CheckViolation):
        with psycopg.connect(repository._dsn, autocommit=True) as conn:
            conn.execute(
                "INSERT INTO accounts (id, username, password_hash, role)"
                " VALUES (%s, 'x', 'h', 'root')",
                (uuid.uuid4(),),
            )


@integration
def test_register_and_get_round_trip(repository):
    created = repository.register("Ana", HASH)
    assert created.role is Role.PLAYER
    found = repository.get_by_username("Ana")
    assert found is not None
    assert (found.id, found.password_hash, found.role) == (created.id, HASH, Role.PLAYER)
    assert repository.get_by_username("nadie") is None


@integration
def test_username_lookup_and_uniqueness_ignore_case(repository):
    created = repository.register("Ana", HASH)
    assert repository.get_by_username("aNA").id == created.id
    with pytest.raises(AccountAlreadyExistsError):
        repository.register("ANA", HASH)
    assert query("SELECT count(*) FROM accounts") == [(1,)]


@integration
def test_get_usernames_returns_only_existing_ids(repository):
    ana = repository.register("Ana", HASH)
    beto = repository.register("beto", HASH)
    result = repository.get_usernames([ana.id, beto.id, uuid.uuid4()])
    assert result == {ana.id: "Ana", beto.id: "beto"}
    assert repository.get_usernames([]) == {}


@integration
def test_values_the_schema_rejects_are_invalid_account(repository):
    with pytest.raises(InvalidAccountError):
        repository.register("a\x00b", HASH)
    with pytest.raises(InvalidAccountError):
        repository.register("ana", "hash\x00")


def _seeded(clean_database, seed):
    repo = PostgresSnapshotRepository(
        clean_database,
        admin_seed=seed,
        backoff_initial_s=0.05,
        backoff_max_s=0.2,
        liveness_interval_s=60,
    )
    repo.start()
    assert repo.wait_ready(timeout_s=10)
    return repo


@integration
def test_admin_is_seeded_before_the_service_is_ready(clean_database):
    repo = _seeded(clean_database, AdminSeed("Root", HASH))
    try:
        account = repo.get_by_username("root")
        assert account is not None
        assert (account.role, account.password_hash) == (Role.ADMIN, HASH)
    finally:
        repo.stop()


@integration
def test_admin_seeding_is_idempotent_and_never_overwrites(clean_database):
    repo = _seeded(clean_database, AdminSeed("root", HASH))
    first = repo.get_by_username("root")
    repo.stop()
    other_hash = HASH.replace("aGFzaGhhc2hoYXNo", "b3Ryb2hhc2g")
    repo = _seeded(clean_database, AdminSeed("ROOT", other_hash))
    try:
        again = repo.get_by_username("root")
        assert (again.id, again.password_hash) == (first.id, HASH)
        assert query("SELECT count(*) FROM accounts") == [(1,)]
    finally:
        repo.stop()


@integration
def test_seeding_does_not_promote_an_existing_player(clean_database):
    repo = _seeded(clean_database, None)
    player = repo.register("root", HASH)
    repo.stop()
    repo = _seeded(clean_database, AdminSeed("root", HASH))
    try:
        assert repo.get_by_username("root").role is Role.PLAYER
        assert repo.get_by_username("root").id == player.id
    finally:
        repo.stop()

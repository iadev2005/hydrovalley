"""Pruebas de la siembra y la lectura del catálogo en PostgreSQL.

Las de integración reutilizan los fixtures de test_postgres y solo corren
con TEST_DATABASE_URL (base de datos desechable).
"""

import json
from pathlib import Path

import psycopg
import pytest

from persistence.adapters.postgres import PostgresSnapshotRepository
from persistence.catalog import catalog_from_entries, load_catalog_file, parse_catalog
from tests.test_postgres import (  # noqa: F401 (fixtures de pytest)
    FakeConnection,
    clean_database,
    integration,
    query,
)

REPO_CATALOG = Path(__file__).resolve().parents[3] / "catalog" / "catalog.json"


def document() -> dict:
    return json.loads(REPO_CATALOG.read_text(encoding="utf-8"))


# --- Sin base de datos ------------------------------------------------------


def test_the_catalog_is_seeded_after_the_migrations_and_before_the_connection_is_published(
    monkeypatch,
):
    events: list[str] = []
    connection = FakeConnection()

    monkeypatch.setattr(psycopg, "connect", lambda *a, **k: connection)
    monkeypatch.setattr(
        "persistence.adapters.postgres.apply_migrations",
        lambda *a, **k: events.append("migrate") or [],
    )
    repository = PostgresSnapshotRepository(
        "postgresql://nobody@127.0.0.1:1/none", catalog=load_catalog_file(REPO_CATALOG)
    )
    repository._open_connection()
    assert events == ["migrate"]
    inserts = [sql for sql in connection.statements if "INSERT INTO catalog_entries" in sql]
    assert len(inserts) == 12
    assert any("DELETE FROM catalog_entries" in sql for sql in connection.statements)
    assert not connection.closed


def test_without_a_catalog_nothing_is_seeded(monkeypatch):
    connection = FakeConnection()
    monkeypatch.setattr(psycopg, "connect", lambda *a, **k: connection)
    monkeypatch.setattr("persistence.adapters.postgres.apply_migrations", lambda *a, **k: [])
    PostgresSnapshotRepository("postgresql://nobody@127.0.0.1:1/none")._open_connection()
    assert not any("catalog_entries" in sql for sql in connection.statements)


class SeedFailingConnection(FakeConnection):
    def execute(self, sql, params=None):
        if "INSERT INTO catalog_entries" in sql:
            self.statements.append(sql)
            raise psycopg.OperationalError("caída")
        return super().execute(sql, params)


def test_a_failing_catalog_seed_fails_the_attempt_and_closes_the_connection(monkeypatch):
    connection = SeedFailingConnection()
    monkeypatch.setattr(psycopg, "connect", lambda *a, **k: connection)
    monkeypatch.setattr("persistence.adapters.postgres.apply_migrations", lambda *a, **k: [])
    repository = PostgresSnapshotRepository(
        "postgresql://nobody@127.0.0.1:1/none", catalog=load_catalog_file(REPO_CATALOG)
    )
    with pytest.raises(psycopg.OperationalError):
        repository._open_connection()
    assert connection.closed


# --- Con PostgreSQL real ----------------------------------------------------


@pytest.fixture
def seeded(clean_database):
    repo = PostgresSnapshotRepository(
        clean_database,
        catalog=load_catalog_file(REPO_CATALOG),
        backoff_initial_s=0.05,
        backoff_max_s=0.2,
        liveness_interval_s=60,
    )
    repo.start()
    assert repo.wait_ready(timeout_s=10)
    yield repo
    repo.stop()


@integration
def test_migration_creates_catalog_entries(seeded):
    columns = query(
        "SELECT column_name, data_type, is_nullable FROM information_schema.columns "
        "WHERE table_name = 'catalog_entries' ORDER BY ordinal_position"
    )
    assert columns == [
        ("key", "text", "NO"),
        ("kind", "text", "NO"),
        ("value", "jsonb", "NO"),
        ("updated_at", "timestamp with time zone", "NO"),
    ]


@integration
def test_kind_only_admits_rule_installation_and_upgrade(seeded):
    with pytest.raises(psycopg.errors.CheckViolation):
        with psycopg.connect(seeded._dsn, autocommit=True) as conn:
            conn.execute(
                "INSERT INTO catalog_entries (key, kind, value) VALUES ('x', 'other', '1')"
            )


@integration
def test_connecting_seeds_one_row_per_element_and_load_returns_the_same_catalog(seeded):
    assert query("SELECT key, kind FROM catalog_entries ORDER BY key") == [
        ("bigger_drop", "upgrade"),
        ("bigger_tank", "upgrade"),
        ("drip", "installation"),
        ("drop", "rule"),
        ("evaporation", "rule"),
        ("fire", "rule"),
        ("growth", "rule"),
        ("installation_price_increase_percent", "rule"),
        ("offline_installation_seconds", "rule"),
        ("sprinkler", "installation"),
        ("tank", "rule"),
        ("treatment_plant", "installation"),
    ]
    stored = catalog_from_entries(seeded.load_entries())
    assert stored == load_catalog_file(REPO_CATALOG)
    assert stored.version == load_catalog_file(REPO_CATALOG).version


@integration
def test_seeding_again_changes_nothing(seeded):
    before = query("SELECT key, value, updated_at FROM catalog_entries ORDER BY key")
    seeded.stop()
    again = PostgresSnapshotRepository(
        seeded._dsn, catalog=load_catalog_file(REPO_CATALOG), liveness_interval_s=60
    )
    again.start()
    try:
        assert again.wait_ready(timeout_s=10)
        assert query("SELECT key, value, updated_at FROM catalog_entries ORDER BY key") == before
    finally:
        again.stop()


@integration
def test_a_changed_figure_is_updated_and_a_removed_one_is_deleted(seeded):
    seeded.stop()
    changed = document()
    changed["installations"]["sprinkler"]["price_leaves"] = 12
    catalog = parse_catalog(changed)
    # Una fila huérfana (p. ej. de una versión anterior del archivo).
    with psycopg.connect(seeded._dsn, autocommit=True) as conn:
        conn.execute(
            "INSERT INTO catalog_entries (key, kind, value) VALUES ('old', 'rule', '1')"
        )
    again = PostgresSnapshotRepository(seeded._dsn, catalog=catalog, liveness_interval_s=60)
    again.start()
    try:
        assert again.wait_ready(timeout_s=10)
        assert query("SELECT value->>'price_leaves' FROM catalog_entries WHERE key = 'sprinkler'") == [
            ("12",)
        ]
        assert query("SELECT count(*) FROM catalog_entries") == [(12,)]
        assert catalog_from_entries(again.load_entries()).version == catalog.version
    finally:
        again.stop()


@integration
def test_the_ignition_chance_survives_the_round_trip(seeded):
    fire = {e.key: e.value for e in seeded.load_entries()}["fire"]
    assert fire["ignition_chance_ppm"] == 50
    assert fire["propagation_chance_ppm"] == 2000

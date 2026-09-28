"""Migraciones versionadas y mínimas, aplicadas al arrancar.

Cada archivo `NNNN_descripcion.sql` de src/persistence/migrations/ es una
versión (`NNNN`). Las ya aplicadas se anotan en `schema_migrations`; las
nuevas se aplican en orden, todas en una transacción y bajo un cerrojo
consultivo, así que dos arranques simultáneos no chocan y repetir el
arranque no cambia nada.
"""

from pathlib import Path

import psycopg

DEFAULT_MIGRATIONS_DIR = Path(__file__).resolve().parent.parent / "migrations"

# Identificador arbitrario y fijo del cerrojo consultivo de las migraciones.
_ADVISORY_LOCK_ID = 7_220_501


def apply_migrations(conn: psycopg.Connection, directory: Path = DEFAULT_MIGRATIONS_DIR) -> list[str]:
    """Aplica las migraciones pendientes y devuelve sus versiones.

    La conexión debe estar en modo autocommit: la transacción la abre aquí.
    """
    files = sorted(directory.glob("*.sql"))
    if not files:
        raise FileNotFoundError(f"no hay migraciones en {directory}")
    applied_now: list[str] = []
    with conn.transaction():
        conn.execute("SELECT pg_advisory_xact_lock(%s)", (_ADVISORY_LOCK_ID,))
        conn.execute(
            "CREATE TABLE IF NOT EXISTS schema_migrations ("
            " version TEXT PRIMARY KEY,"
            " applied_at TIMESTAMPTZ NOT NULL DEFAULT now())"
        )
        applied = {row[0] for row in conn.execute("SELECT version FROM schema_migrations")}
        for path in files:
            version = path.name.split("_", 1)[0]
            if version in applied:
                continue
            # Sin parámetros, psycopg admite varias sentencias en un execute.
            conn.execute(path.read_text(encoding="utf-8"))
            conn.execute("INSERT INTO schema_migrations (version) VALUES (%s)", (version,))
            applied_now.append(version)
    return applied_now

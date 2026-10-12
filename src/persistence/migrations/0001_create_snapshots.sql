-- Capa 5: una instantánea del mundo cada 50 ticks (diagrama 07).
-- u64 -> NUMERIC(20,0) con CHECK del rango completo: PostgreSQL no tiene
-- enteros sin signo y BIGINT llega solo a 2^63 - 1.
CREATE TABLE snapshots (
    tick               NUMERIC(20,0) PRIMARY KEY
                       CHECK (tick BETWEEN 0 AND 18446744073709551615),
    epoch              INTEGER       NOT NULL,
    catalog_version    TEXT          NOT NULL,
    taken_at           TIMESTAMPTZ   NOT NULL,
    world_state        BYTEA         NOT NULL,
    world_state_sha256 BYTEA         NOT NULL
);

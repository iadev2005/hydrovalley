-- Contadores de cada jugador en cada instantánea (diagrama 07). Son una copia
-- en columnas de lo que ya viaja dentro de world_state, para que los reportes
-- (RF-12) usen SQL sin abrir el protobuf.
-- u64 -> NUMERIC(20,0) con CHECK del rango completo (ver 0001).
--
-- ON DELETE CASCADE sobre snapshots: la poda de retención solo borra de
-- snapshots y arrastra sus filas de jugadores.
-- La clave foránea a accounts no tiene acción: las cuentas nunca se borran.
-- SaveSnapshot omite (con un aviso) las filas de jugadores sin cuenta para
-- no perder la instantánea entera.
CREATE TABLE snapshot_players (
    tick           NUMERIC(20,0) NOT NULL
                   REFERENCES snapshots (tick) ON DELETE CASCADE,
    player_id      UUID          NOT NULL REFERENCES accounts (id),
    water          NUMERIC(20,0) NOT NULL CHECK (water          BETWEEN 0 AND 18446744073709551615),
    leaves         NUMERIC(20,0) NOT NULL CHECK (leaves         BETWEEN 0 AND 18446744073709551615),
    water_refilled NUMERIC(20,0) NOT NULL CHECK (water_refilled BETWEEN 0 AND 18446744073709551615),
    water_spent    NUMERIC(20,0) NOT NULL CHECK (water_spent    BETWEEN 0 AND 18446744073709551615),
    leaves_earned  NUMERIC(20,0) NOT NULL CHECK (leaves_earned  BETWEEN 0 AND 18446744073709551615),
    leaves_spent   NUMERIC(20,0) NOT NULL CHECK (leaves_spent   BETWEEN 0 AND 18446744073709551615),
    water_reduced  NUMERIC(20,0) NOT NULL CHECK (water_reduced  BETWEEN 0 AND 18446744073709551615),
    water_reused   NUMERIC(20,0) NOT NULL CHECK (water_reused   BETWEEN 0 AND 18446744073709551615),
    water_recycled NUMERIC(20,0) NOT NULL CHECK (water_recycled BETWEEN 0 AND 18446744073709551615),
    PRIMARY KEY (tick, player_id)
);

-- Los reportes por jugador buscan por player_id y tick.
CREATE INDEX snapshot_players_player_idx ON snapshot_players (player_id, tick);

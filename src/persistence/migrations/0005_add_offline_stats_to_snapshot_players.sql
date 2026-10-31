-- Lo que hicieron los aspersores de cada jugador en la época (RF-25): fuegos
-- apagados y casillas que crecieron. Son contadores acumulados como los de
-- las 3R: u64 -> NUMERIC(20,0) con CHECK del rango completo (ver 0001).
ALTER TABLE snapshot_players
    ADD COLUMN fires_extinguished NUMERIC(20,0) NOT NULL DEFAULT 0
        CHECK (fires_extinguished BETWEEN 0 AND 18446744073709551615),
    ADD COLUMN tiles_grown NUMERIC(20,0) NOT NULL DEFAULT 0
        CHECK (tiles_grown BETWEEN 0 AND 18446744073709551615);

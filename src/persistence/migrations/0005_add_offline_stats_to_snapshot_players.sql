-- Resumen de actividad offline del “día siguiente” (RF-25): fuegos apagados
-- y casillas que crecieron mientras el propietario estaba ausente.
ALTER TABLE snapshot_players
    ADD COLUMN fires_extinguished NUMERIC(20,0) NOT NULL DEFAULT 0
        CHECK (fires_extinguished BETWEEN 0 AND 18446744073709551615),
    ADD COLUMN tiles_grown NUMERIC(20,0) NOT NULL DEFAULT 0
        CHECK (tiles_grown BETWEEN 0 AND 18446744073709551615);

-- Catálogo del juego (diagrama 07): una fila por cifra o elemento (drop,
-- tank, sprinkler…). El valor es JSONB porque las cifras son heterogéneas y
-- GetCatalog las lee todas de golpe: añadir una cifra no migra el esquema.
-- La persistencia siembra estas filas al conectar, desde catalog/catalog.json.
CREATE TABLE catalog_entries (
    key        TEXT        PRIMARY KEY,
    kind       TEXT        NOT NULL CHECK (kind IN ('rule', 'installation', 'upgrade')),
    value      JSONB       NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

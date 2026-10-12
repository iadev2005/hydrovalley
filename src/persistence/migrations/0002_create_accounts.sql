-- Cuentas de jugador y de administrador (diagrama 07). Solo guarda el hash
-- Argon2id que calcula la API; la contraseña nunca llega aquí.
-- El id es el player_id del token y del mundo.
CREATE TABLE accounts (
    id            UUID        PRIMARY KEY,
    username      TEXT        NOT NULL,
    password_hash TEXT        NOT NULL,
    role          TEXT        NOT NULL CHECK (role IN ('player', 'admin')),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Nombre de usuario único sin distinguir mayúsculas: "Ana" y "ana" son la
-- misma cuenta. La aplicación limita el alfabeto a ASCII, así que lower()
-- no depende de la configuración regional. GetAccount busca por
-- lower(username) para aprovechar este índice.
CREATE UNIQUE INDEX accounts_username_lower_key ON accounts (lower(username));

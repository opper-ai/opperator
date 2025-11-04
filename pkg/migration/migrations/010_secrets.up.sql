CREATE TABLE IF NOT EXISTS secrets (
    name TEXT PRIMARY KEY,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_secrets_name ON secrets(name);

-- +migrate Up
-- Monitored PostgreSQL instances (single in v1, multi-instance ready)
CREATE TABLE instances (
    id          INTEGER PRIMARY KEY,
    name        TEXT NOT NULL,
    host        TEXT NOT NULL,
    port        INTEGER NOT NULL DEFAULT 5432,
    database    TEXT NOT NULL,
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(host, port, database)
);

-- +migrate Down
DROP TABLE IF EXISTS instances;

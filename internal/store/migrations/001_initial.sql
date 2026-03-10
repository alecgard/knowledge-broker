CREATE TABLE IF NOT EXISTS fragments (
    id            TEXT PRIMARY KEY,
    content       TEXT NOT NULL,
    source_type   TEXT NOT NULL,
    source_name   TEXT DEFAULT '',
    source_path   TEXT NOT NULL,
    source_uri    TEXT NOT NULL,
    last_modified DATETIME NOT NULL,
    author        TEXT DEFAULT '',
    file_type     TEXT NOT NULL,
    checksum      TEXT NOT NULL,
    confidence_adj REAL DEFAULT 0.0,
    created_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at    DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_fragments_source ON fragments(source_type, source_path);
CREATE INDEX IF NOT EXISTS idx_fragments_checksum ON fragments(source_type, checksum);

CREATE TABLE IF NOT EXISTS metadata (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS sources (
    source_type TEXT NOT NULL,
    source_name TEXT NOT NULL,
    config      TEXT NOT NULL DEFAULT '{}',
    last_ingest DATETIME,
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (source_type, source_name)
);

CREATE TABLE IF NOT EXISTS sources (
    source_type TEXT NOT NULL,
    source_name TEXT NOT NULL,
    config      TEXT NOT NULL DEFAULT '{}',
    last_ingest DATETIME,
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (source_type, source_name)
);

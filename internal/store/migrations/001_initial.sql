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
    description TEXT NOT NULL DEFAULT '',
    config      TEXT NOT NULL DEFAULT '{}',
    last_ingest DATETIME,
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (source_type, source_name)
);

CREATE TABLE IF NOT EXISTS knowledge_units (
    id TEXT PRIMARY KEY,
    topic TEXT NOT NULL,
    summary TEXT NOT NULL DEFAULT '',
    confidence_freshness REAL DEFAULT 0,
    confidence_corroboration REAL DEFAULT 0,
    confidence_consistency REAL DEFAULT 0,
    confidence_authority REAL DEFAULT 0,
    last_computed DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS knowledge_unit_fragments (
    unit_id TEXT NOT NULL,
    fragment_id TEXT NOT NULL,
    PRIMARY KEY (unit_id, fragment_id),
    FOREIGN KEY (unit_id) REFERENCES knowledge_units(id) ON DELETE CASCADE,
    FOREIGN KEY (fragment_id) REFERENCES fragments(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS query_cache (
    cache_key    TEXT PRIMARY KEY,
    query_text   TEXT NOT NULL,
    concise      INTEGER NOT NULL DEFAULT 0,
    fragment_sigs TEXT NOT NULL,
    answer_json  TEXT NOT NULL,
    created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_query_cache_created ON query_cache(created_at);

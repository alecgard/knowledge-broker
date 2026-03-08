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

CREATE TABLE IF NOT EXISTS feedback (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    fragment_id TEXT NOT NULL REFERENCES fragments(id),
    type        TEXT NOT NULL CHECK(type IN ('correction', 'challenge', 'confirmation')),
    content     TEXT DEFAULT '',
    evidence    TEXT DEFAULT '',
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_feedback_fragment ON feedback(fragment_id);

CREATE TABLE IF NOT EXISTS correction_fragments (
    feedback_id  INTEGER NOT NULL REFERENCES feedback(id),
    fragment_id  TEXT NOT NULL REFERENCES fragments(id),
    PRIMARY KEY (feedback_id, fragment_id)
);

CREATE TABLE IF NOT EXISTS metadata (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

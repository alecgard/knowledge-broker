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

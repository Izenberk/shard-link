-- Standard Shards (Active Memory)
CREATE TABLE IF NOT EXISTS shards (
    id          TEXT PRIMARY KEY,
    category    TEXT NOT NULL,      -- 'core', 'session', 'memory'
    content     TEXT NOT NULL,
    vector      BLOB NOT NULL,      -- 768-D float32 vector (Optimized for Gemini & RAM)
    metadata    BLOB,               -- SQLite JSONB
    
    -- Phase 9: Source Provenance
    source_type TEXT,               -- 'manual', 'github', 'chat', 'web_scrape'
    source_ref  TEXT,               -- URI, File Path, or ID
    confidence  REAL DEFAULT 1.0,   -- 0.0 - 1.0 Reliability Score

    last_used   DATETIME DEFAULT CURRENT_TIMESTAMP,
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- THE BASEMENT (Tiered/Archival Storage)
CREATE TABLE IF NOT EXISTS shards_archive (
    id          TEXT PRIMARY KEY,
    category    TEXT NOT NULL,
    content     TEXT NOT NULL,
    vector      BLOB NOT NULL,
    metadata    BLOB,

    -- Phase 9: Source Provenance
    source_type TEXT,
    source_ref  TEXT,
    confidence  REAL DEFAULT 1.0,

    archived_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Relational Mesh
CREATE TABLE IF NOT EXISTS shard_bonds (
    from_id     TEXT NOT NULL,
    to_id       TEXT NOT NULL,
    weight      REAL NOT NULL,  -- Resonance score (0.0 - 1.0)
    PRIMARY KEY (from_id, to_id),
    FOREIGN KEY (from_id) REFERENCES shards(id) ON DELETE CASCADE,
    FOREIGN KEY (to_id) REFERENCES shards(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_shards_janitor ON shards(category, last_used);
CREATE INDEX IF NOT EXISTS idx_shards_category_core ON shards(category) WHERE category = 'core';

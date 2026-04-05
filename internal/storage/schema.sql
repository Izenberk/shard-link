CREATE TABLE IF NOT EXISTS shards (
    id          TEXT PRIMARY KEY,
    category    TEXT NOT NULL,      -- 'core', 'session', 'memory'
    content     TEXT NOT NULL,
    vector      BLOB NOT NULL,      -- 1536-D float32 vector (6144 bytes)
    metadata    BLOB,               -- SQLite JSONB
    last_used   DATETIME DEFAULT CURRENT_TIMESTAMP,
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS shard_bonds (
    from_id     TEXT NOT NULL,
    to_id       TEXT NOT NULL,
    weight      REAL NOT NULL,  -- Resonance score (0.0 - 1.0)
    PRIMARY KEY (from_id, to_id),
    FOREIGN KEY (from_id) REFERENCES shards(id) ON DELETE CASCADE,
    FOREIGN KEY (to_id) REFERENCES shards(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_shards_janitor ON shards(category, last_used);
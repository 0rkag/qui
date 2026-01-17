-- +migrate Up
-- Instance path mappings for canonical path translation
-- Each instance defines how its paths map to QUI server's canonical paths

CREATE TABLE instance_path_mappings (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    instance_id INTEGER NOT NULL,
    instance_path TEXT NOT NULL,      -- Path as seen by the qBittorrent instance
    canonical_path TEXT NOT NULL,     -- Path as seen by QUI server (canonical)
    enabled INTEGER NOT NULL DEFAULT 1,
    description TEXT,                 -- Optional user note
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (instance_id) REFERENCES instances(id) ON DELETE CASCADE,
    UNIQUE(instance_id, instance_path)  -- No duplicate source paths per instance
);

CREATE INDEX idx_instance_path_mappings_instance ON instance_path_mappings(instance_id);
CREATE INDEX idx_instance_path_mappings_enabled ON instance_path_mappings(instance_id, enabled);


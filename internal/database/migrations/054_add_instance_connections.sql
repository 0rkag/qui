-- Instance connection configurations for remote access
-- Supports multiple protocols: SSH, FTP, etc.

CREATE TABLE instance_connections (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    instance_id INTEGER NOT NULL,

    -- Protocol type: "ssh", "ftp", "sftp", etc.
    protocol TEXT NOT NULL DEFAULT 'ssh',

    -- Connection details
    host TEXT NOT NULL,
    port INTEGER NOT NULL DEFAULT 22,
    username TEXT NOT NULL,

    -- Authentication (protocol-dependent)
    -- For SSH: path to private key file on QUI server
    private_key_path TEXT,

    -- Settings
    enabled INTEGER NOT NULL DEFAULT 1,

    -- Timestamps
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,

    FOREIGN KEY (instance_id) REFERENCES instances(id) ON DELETE CASCADE,
    UNIQUE(instance_id, protocol)  -- One config per protocol per instance
);

CREATE INDEX idx_instance_connections_instance ON instance_connections(instance_id);
CREATE INDEX idx_instance_connections_enabled ON instance_connections(instance_id, enabled);

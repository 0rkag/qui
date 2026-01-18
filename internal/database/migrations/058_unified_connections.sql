-- Unified connection configurations for remote access to instances
-- Supports SSH (with multiple transfer methods) and FTP (explicit, implicit, plain)

-- Drop old tables (no migration needed - no production DBs exist)
DROP TABLE IF EXISTS instance_ftp_connections;
DROP TABLE IF EXISTS instance_connections;

-- Create unified connections table
CREATE TABLE instance_connections (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    instance_id INTEGER NOT NULL,

    -- Connection type: ssh_auto, ssh_rsync, ssh_sftp, ssh_scp, ftp_explicit, ftp_implicit, ftp_plain
    type TEXT NOT NULL,

    -- Connection details
    host TEXT NOT NULL,
    port INTEGER NOT NULL,
    username TEXT NOT NULL,

    -- Authentication
    password_encrypted TEXT,      -- AES-GCM encrypted (for SSH password auth or FTP)
    private_key_path TEXT,        -- For SSH: path to private key file on QUI server

    -- Settings
    enabled INTEGER NOT NULL DEFAULT 1,

    -- Detected capabilities (cached from connection test, SSH types only)
    rsync_available INTEGER DEFAULT NULL,
    rsync_version TEXT DEFAULT NULL,
    sftp_available INTEGER DEFAULT NULL,
    hardlinks_supported INTEGER DEFAULT NULL,
    reflinks_supported INTEGER DEFAULT NULL,
    capabilities_checked_at DATETIME DEFAULT NULL,

    -- Timestamps
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,

    FOREIGN KEY (instance_id) REFERENCES instances(id) ON DELETE CASCADE,
    UNIQUE(instance_id, type)  -- One config per type per instance
);

CREATE INDEX idx_instance_connections_instance ON instance_connections(instance_id);
CREATE INDEX idx_instance_connections_enabled ON instance_connections(instance_id, enabled);
CREATE INDEX idx_instance_connections_type ON instance_connections(instance_id, type);

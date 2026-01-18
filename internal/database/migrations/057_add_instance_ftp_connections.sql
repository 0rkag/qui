-- FTP/FTPS connection configurations for remote access to instances
-- Supports both plain FTP and FTPS (explicit TLS)

CREATE TABLE instance_ftp_connections (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    instance_id INTEGER NOT NULL,

    -- Connection details
    host TEXT NOT NULL,
    port INTEGER NOT NULL DEFAULT 21,
    username TEXT NOT NULL,
    password_encrypted TEXT NOT NULL,  -- AES-GCM encrypted

    -- TLS settings
    use_tls INTEGER NOT NULL DEFAULT 1,        -- Use FTPS (explicit TLS)
    tls_skip_verify INTEGER NOT NULL DEFAULT 0, -- Skip certificate verification

    -- Transfer settings
    passive_mode INTEGER NOT NULL DEFAULT 1,   -- Use passive mode (recommended)
    base_path TEXT,                            -- Base path on FTP server

    -- Detected capabilities (cached from connection test)
    tls_enabled INTEGER DEFAULT NULL,
    passive_mode_works INTEGER DEFAULT NULL,
    fxp_supported INTEGER DEFAULT NULL,
    capabilities_checked_at DATETIME DEFAULT NULL,

    -- Settings
    enabled INTEGER NOT NULL DEFAULT 1,

    -- Timestamps
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,

    FOREIGN KEY (instance_id) REFERENCES instances(id) ON DELETE CASCADE,
    UNIQUE(instance_id)  -- One FTP config per instance
);

CREATE INDEX idx_instance_ftp_connections_instance ON instance_ftp_connections(instance_id);
CREATE INDEX idx_instance_ftp_connections_enabled ON instance_ftp_connections(instance_id, enabled);

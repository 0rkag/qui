-- Add capability detection results and transfer method preference to instance_connections

-- Detected capabilities (cached from connection test)
ALTER TABLE instance_connections ADD COLUMN rsync_available INTEGER DEFAULT NULL;
ALTER TABLE instance_connections ADD COLUMN rsync_version TEXT DEFAULT NULL;
ALTER TABLE instance_connections ADD COLUMN sftp_available INTEGER DEFAULT NULL;
ALTER TABLE instance_connections ADD COLUMN hardlinks_supported INTEGER DEFAULT NULL;
ALTER TABLE instance_connections ADD COLUMN reflinks_supported INTEGER DEFAULT NULL;
ALTER TABLE instance_connections ADD COLUMN capabilities_checked_at DATETIME DEFAULT NULL;

-- User preference for transfer method: "auto", "rsync", "sftp"
-- "auto" (default) = use rsync if available, fallback to sftp
ALTER TABLE instance_connections ADD COLUMN transfer_method TEXT DEFAULT 'auto';

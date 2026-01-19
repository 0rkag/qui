-- Copyright (c) 2025, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- Migration: Add transfers functionality with remote instance connections
-- This migration adds:
--   1. transfers table for tracking torrent transfers between instances
--   2. instance_path_mappings table for canonical path translation
--   3. instance_connections table for SSH/FTP remote access configuration

-- =============================================================================
-- TRANSFERS TABLE
-- =============================================================================
-- Tracks torrent transfers between qBittorrent instances with full state machine

CREATE TABLE IF NOT EXISTS transfers (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,

    -- Source and target instances
    source_instance_id   INTEGER NOT NULL,
    target_instance_id   INTEGER NOT NULL,
    torrent_hash         TEXT NOT NULL,
    torrent_name         TEXT NOT NULL,

    -- State machine: pending -> preparing -> links_creating -> links_created ->
    --                [verifying] -> adding_torrent -> torrent_added ->
    --                [deleting_source] -> completed
    state                TEXT NOT NULL DEFAULT 'pending',

    -- Persisted configuration for recovery
    source_save_path     TEXT,
    target_save_path     TEXT,
    link_mode            TEXT,  -- 'hardlink', 'reflink', 'direct'

    -- Transfer options
    delete_from_source   INTEGER NOT NULL DEFAULT 1,
    preserve_category    INTEGER NOT NULL DEFAULT 1,
    preserve_tags        INTEGER NOT NULL DEFAULT 1,
    target_category      TEXT,
    target_tags          TEXT,  -- JSON array
    path_mappings        TEXT,  -- JSON object

    -- Progress tracking
    files_total          INTEGER NOT NULL DEFAULT 0,
    files_linked         INTEGER NOT NULL DEFAULT 0,
    bytes_total          INTEGER NOT NULL DEFAULT 0,
    bytes_transferred    INTEGER NOT NULL DEFAULT 0,

    -- Transfer behavior options
    force                INTEGER NOT NULL DEFAULT 0,  -- Allow overwriting existing files
    verify_transfer      INTEGER NOT NULL DEFAULT 0,  -- Enable post-transfer checksum verification

    -- Action enums for better user control
    -- file_exists_action: 'abort' (fail), 'skip' (skip identical), 'overwrite' (force)
    -- source_action: 'keep' (keep seeding), 'pause' (pause torrent), 'delete' (remove + files)
    file_exists_action   TEXT NOT NULL DEFAULT 'abort',
    source_action        TEXT NOT NULL DEFAULT 'keep',

    -- Error info
    error                TEXT,

    -- Timestamps
    created_at           DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at           DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at         DATETIME,

    FOREIGN KEY (source_instance_id) REFERENCES instances(id) ON DELETE CASCADE,
    FOREIGN KEY (target_instance_id) REFERENCES instances(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_transfers_state ON transfers(state);
CREATE INDEX IF NOT EXISTS idx_transfers_source ON transfers(source_instance_id, state);
CREATE INDEX IF NOT EXISTS idx_transfers_target ON transfers(target_instance_id, state);
CREATE INDEX IF NOT EXISTS idx_transfers_hash ON transfers(torrent_hash);

CREATE TRIGGER IF NOT EXISTS trg_transfers_updated
AFTER UPDATE ON transfers
BEGIN
    UPDATE transfers
    SET updated_at = CURRENT_TIMESTAMP
    WHERE id = NEW.id;
END;

-- =============================================================================
-- INSTANCE PATH MAPPINGS TABLE
-- =============================================================================
-- Each instance defines how its paths map to QUI server's canonical paths

CREATE TABLE IF NOT EXISTS instance_path_mappings (
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

CREATE INDEX IF NOT EXISTS idx_instance_path_mappings_instance ON instance_path_mappings(instance_id);
CREATE INDEX IF NOT EXISTS idx_instance_path_mappings_enabled ON instance_path_mappings(instance_id, enabled);

-- =============================================================================
-- INSTANCE CONNECTIONS TABLE
-- =============================================================================
-- Unified connection configurations for remote access to instances
-- Supports SSH (with multiple transfer methods) and FTP (explicit, implicit, plain)

CREATE TABLE IF NOT EXISTS instance_connections (
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

    -- SSH host key verification (TOFU - Trust On First Use)
    host_key_fingerprint TEXT DEFAULT NULL,
    host_key_algorithm TEXT DEFAULT NULL,
    host_key_verified_at DATETIME DEFAULT NULL,

    -- FTP TLS settings (0 = verify certificates, 1 = skip verification)
    tls_skip_verify INTEGER NOT NULL DEFAULT 0,

    -- Timestamps
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,

    FOREIGN KEY (instance_id) REFERENCES instances(id) ON DELETE CASCADE,
    UNIQUE(instance_id, type)  -- One config per type per instance
);

CREATE INDEX IF NOT EXISTS idx_instance_connections_instance ON instance_connections(instance_id);
CREATE INDEX IF NOT EXISTS idx_instance_connections_enabled ON instance_connections(instance_id, enabled);
CREATE INDEX IF NOT EXISTS idx_instance_connections_type ON instance_connections(instance_id, type);

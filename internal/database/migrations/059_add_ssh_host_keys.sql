-- Copyright (c) 2025, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- Migration: Add SSH host key verification (TOFU) and FTP TLS skip verify fields
-- This enables Trust-On-First-Use host key verification for SSH connections
-- and configurable TLS certificate verification for FTP connections.

-- Add SSH host key fields for TOFU (Trust On First Use) verification
ALTER TABLE instance_connections ADD COLUMN host_key_fingerprint TEXT DEFAULT NULL;
ALTER TABLE instance_connections ADD COLUMN host_key_algorithm TEXT DEFAULT NULL;
ALTER TABLE instance_connections ADD COLUMN host_key_verified_at DATETIME DEFAULT NULL;

-- Add FTP TLS skip verify flag (0 = verify certificates, 1 = skip verification)
ALTER TABLE instance_connections ADD COLUMN tls_skip_verify INTEGER NOT NULL DEFAULT 0;

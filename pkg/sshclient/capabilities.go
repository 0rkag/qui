// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sshclient

import (
	"context"
	"strings"
)

// Capabilities represents detected features available on a remote host.
type Capabilities struct {
	// RsyncAvailable indicates if rsync binary is present on the remote host.
	RsyncAvailable bool `json:"rsyncAvailable"`

	// RsyncVersion contains the rsync version string if available.
	RsyncVersion string `json:"rsyncVersion,omitempty"`

	// SFTPAvailable indicates if SFTP subsystem is available (always true if SSH works).
	SFTPAvailable bool `json:"sftpAvailable"`

	// HardlinksSupported indicates if the filesystem supports hardlinks.
	HardlinksSupported bool `json:"hardlinksSupported"`

	// ReflinksSupported indicates if the filesystem supports reflinks (CoW copies).
	ReflinksSupported bool `json:"reflinksSupported"`
}

// CheckCapabilities detects available features on the remote host.
// This should be called during connection testing to cache the results.
func (c *Client) CheckCapabilities(ctx context.Context) (*Capabilities, error) {
	caps := &Capabilities{
		// SFTP is always available if SSH connection works
		SFTPAvailable: true,
	}

	// Check rsync availability
	result, err := c.Exec(ctx, "which rsync && rsync --version | head -1")
	if err == nil && result.ExitCode == 0 {
		caps.RsyncAvailable = true
		// Extract version from output like "rsync  version 3.2.7  protocol version 31"
		lines := strings.Split(strings.TrimSpace(result.Stdout), "\n")
		if len(lines) >= 2 {
			caps.RsyncVersion = strings.TrimSpace(lines[1])
		}
	}

	// Check hardlink support by trying to create one in /tmp
	// We create a test file, hardlink it, and clean up
	hardlinkTest := `
		_test_dir=$(mktemp -d) && \
		echo "test" > "$_test_dir/src" && \
		ln "$_test_dir/src" "$_test_dir/dst" 2>/dev/null && \
		rm -rf "$_test_dir" && \
		echo "ok"
	`
	result, err = c.Exec(ctx, hardlinkTest)
	if err == nil && result.ExitCode == 0 && strings.Contains(result.Stdout, "ok") {
		caps.HardlinksSupported = true
	}

	// Check reflink support (requires filesystem support like btrfs, xfs, apfs)
	// Try both Linux (--reflink=always) and macOS (cp -c) variants
	reflinkTest := `
		_test_dir=$(mktemp -d) && \
		echo "test" > "$_test_dir/src" && \
		(cp --reflink=always "$_test_dir/src" "$_test_dir/dst" 2>/dev/null || cp -c "$_test_dir/src" "$_test_dir/dst" 2>/dev/null) && \
		rm -rf "$_test_dir" && \
		echo "ok"
	`
	result, err = c.Exec(ctx, reflinkTest)
	if err == nil && result.ExitCode == 0 && strings.Contains(result.Stdout, "ok") {
		caps.ReflinksSupported = true
	}

	return caps, nil
}

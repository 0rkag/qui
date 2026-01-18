// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sshclient

import (
	"context"
	"fmt"
	"strings"

	"github.com/autobrr/qui/pkg/checksum"
)

// FileChecksum calculates the checksum of a remote file via SSH command.
// This uses the system's checksum utilities (md5sum, sha256sum).
func (c *Client) FileChecksum(ctx context.Context, path string, alg checksum.Algorithm) (string, error) {
	if err := ValidatePath(path); err != nil {
		return "", err
	}

	var cmd string
	switch alg {
	case checksum.AlgorithmMD5:
		cmd = fmt.Sprintf("md5sum %s | cut -d' ' -f1", ShellQuote(path))
	case checksum.AlgorithmSHA256:
		cmd = fmt.Sprintf("sha256sum %s | cut -d' ' -f1", ShellQuote(path))
	default:
		return "", checksum.ErrUnsupportedAlgorithm
	}

	result, err := c.Exec(ctx, cmd)
	if err != nil {
		return "", fmt.Errorf("execute checksum command: %w", err)
	}
	if result.ExitCode != 0 {
		return "", fmt.Errorf("checksum command failed (exit %d): %s", result.ExitCode, result.Stderr)
	}

	return strings.TrimSpace(result.Stdout), nil
}

// FileSize returns the size of a remote file in bytes.
func (c *Client) FileSize(ctx context.Context, path string) (int64, error) {
	if err := ValidatePath(path); err != nil {
		return 0, err
	}

	cmd := fmt.Sprintf("stat -c %%s %s 2>/dev/null || stat -f %%z %s", ShellQuote(path), ShellQuote(path))
	result, err := c.Exec(ctx, cmd)
	if err != nil {
		return 0, fmt.Errorf("execute stat command: %w", err)
	}
	if result.ExitCode != 0 {
		return 0, fmt.Errorf("stat command failed: %s", result.Stderr)
	}

	var size int64
	if _, err := fmt.Sscanf(strings.TrimSpace(result.Stdout), "%d", &size); err != nil {
		return 0, fmt.Errorf("parse file size: %w", err)
	}

	return size, nil
}

// SFTPFileChecksum calculates the checksum of a remote file via SFTP.
// This streams the file content through the SFTP connection and computes the hash locally.
func (s *SFTPClient) FileChecksum(ctx context.Context, path string, alg checksum.Algorithm) (string, error) {
	f, err := s.client.Open(path)
	if err != nil {
		return "", fmt.Errorf("open remote file: %w", err)
	}
	defer f.Close()

	return checksum.ReaderChecksum(f, alg)
}

// SFTPFileSize returns the size of a remote file via SFTP.
func (s *SFTPClient) FileSize(path string) (int64, error) {
	info, err := s.client.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

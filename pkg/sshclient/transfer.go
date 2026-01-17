// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sshclient

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// TransferOptions configures file transfer behavior.
type TransferOptions struct {
	// PreservePermissions preserves file permissions and timestamps.
	PreservePermissions bool
	// Delete removes files at destination that don't exist at source.
	Delete bool
	// DryRun simulates the transfer without making changes.
	DryRun bool
}

// TransferResult holds the result of a file transfer.
type TransferResult struct {
	// FilesTransferred is the number of files transferred.
	FilesTransferred int
	// BytesTransferred is the total bytes transferred.
	BytesTransferred int64
	// Output is the raw rsync output.
	Output string
}

// RsyncPush transfers files from local to remote using rsync.
func RsyncPush(ctx context.Context, cfg *Config, localPath, remotePath string, opts *TransferOptions) (*TransferResult, error) {
	if opts == nil {
		opts = &TransferOptions{PreservePermissions: true}
	}

	args := buildRsyncArgs(opts)
	args = append(args, "-e", buildSSHCommand(cfg))
	args = append(args, localPath)
	args = append(args, fmt.Sprintf("%s@%s:%s", cfg.Username, cfg.Host, remotePath))

	return runRsync(ctx, args)
}

// RsyncPull transfers files from remote to local using rsync.
func RsyncPull(ctx context.Context, cfg *Config, remotePath, localPath string, opts *TransferOptions) (*TransferResult, error) {
	if opts == nil {
		opts = &TransferOptions{PreservePermissions: true}
	}

	args := buildRsyncArgs(opts)
	args = append(args, "-e", buildSSHCommand(cfg))
	args = append(args, fmt.Sprintf("%s@%s:%s", cfg.Username, cfg.Host, remotePath))
	args = append(args, localPath)

	return runRsync(ctx, args)
}

// RsyncRemoteToRemote transfers files between two remote hosts.
// Requires the source host to have SSH access to the destination host.
func RsyncRemoteToRemote(ctx context.Context, srcCfg, dstCfg *Config, srcPath, dstPath string, opts *TransferOptions) (*TransferResult, error) {
	if opts == nil {
		opts = &TransferOptions{PreservePermissions: true}
	}

	// Build rsync command to run on source host
	rsyncArgs := buildRsyncArgs(opts)
	rsyncArgs = append(rsyncArgs, "-e", fmt.Sprintf("ssh -p %d -o StrictHostKeyChecking=no", dstCfg.Port))
	rsyncArgs = append(rsyncArgs, srcPath)
	rsyncArgs = append(rsyncArgs, fmt.Sprintf("%s@%s:%s", dstCfg.Username, dstCfg.Host, dstPath))

	// Connect to source and run rsync there
	client, err := New(srcCfg)
	if err != nil {
		return nil, fmt.Errorf("connect to source: %w", err)
	}
	defer client.Close()

	cmd := "rsync " + strings.Join(rsyncArgs, " ")
	result, err := client.Exec(ctx, cmd)
	if err != nil {
		return nil, fmt.Errorf("rsync: %w", err)
	}

	if result.ExitCode != 0 {
		return nil, fmt.Errorf("rsync failed (exit %d): %s", result.ExitCode, strings.TrimSpace(result.Stderr))
	}

	return &TransferResult{
		Output: result.Stdout,
	}, nil
}

// ScpPush copies a single file from local to remote using scp.
func ScpPush(ctx context.Context, cfg *Config, localPath, remotePath string) error {
	args := []string{
		"-P", fmt.Sprintf("%d", cfg.Port),
		"-o", "StrictHostKeyChecking=no",
		"-i", cfg.PrivateKeyPath,
		localPath,
		fmt.Sprintf("%s@%s:%s", cfg.Username, cfg.Host, remotePath),
	}

	cmd := exec.CommandContext(ctx, "scp", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("scp: %w: %s", err, stderr.String())
	}
	return nil
}

// ScpPull copies a single file from remote to local using scp.
func ScpPull(ctx context.Context, cfg *Config, remotePath, localPath string) error {
	args := []string{
		"-P", fmt.Sprintf("%d", cfg.Port),
		"-o", "StrictHostKeyChecking=no",
		"-i", cfg.PrivateKeyPath,
		fmt.Sprintf("%s@%s:%s", cfg.Username, cfg.Host, remotePath),
		localPath,
	}

	cmd := exec.CommandContext(ctx, "scp", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("scp: %w: %s", err, stderr.String())
	}
	return nil
}

func buildRsyncArgs(opts *TransferOptions) []string {
	args := []string{"-a", "-v", "--progress"}

	if !opts.PreservePermissions {
		args = []string{"-r", "-v", "--progress"}
	}
	if opts.Delete {
		args = append(args, "--delete")
	}
	if opts.DryRun {
		args = append(args, "--dry-run")
	}

	return args
}

func buildSSHCommand(cfg *Config) string {
	return fmt.Sprintf("ssh -p %d -o StrictHostKeyChecking=no -i %s",
		cfg.Port, cfg.PrivateKeyPath)
}

func runRsync(ctx context.Context, args []string) (*TransferResult, error) {
	cmd := exec.CommandContext(ctx, "rsync", args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("rsync failed (exit %d): %s", exitErr.ExitCode(), stderr.String())
		}
		return nil, fmt.Errorf("rsync: %w", err)
	}

	return &TransferResult{
		Output: stdout.String(),
	}, nil
}

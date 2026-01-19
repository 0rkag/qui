// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sshclient

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"
)

// SCPTransferOptions configures SCP transfer behavior.
type SCPTransferOptions struct {
	// PreservePermissions preserves file permissions during transfer (-p flag).
	PreservePermissions bool

	// FileExistsMode determines what to do when target file exists.
	FileExistsMode FileExistsMode

	// Recursive enables recursive directory transfer (-r flag).
	Recursive bool

	// OnProgress is called periodically with transfer progress.
	// Note: SCP has limited progress reporting compared to rsync/SFTP.
	OnProgress func(bytesTransferred, bytesTotal int64)
}

// SCPClient wraps SCP operations for file transfer.
type SCPClient struct {
	cfg       *Config
	sshClient *Client
}

// NewSCPClient creates a new SCP client from SSH configuration.
func NewSCPClient(cfg *Config) (*SCPClient, error) {
	if err := validateConfig(cfg); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	return &SCPClient{cfg: cfg}, nil
}

// NewSCPClientWithSSH creates a new SCP client with an existing SSH client for remote operations.
func NewSCPClientWithSSH(cfg *Config, sshClient *Client) (*SCPClient, error) {
	if err := validateConfig(cfg); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	return &SCPClient{cfg: cfg, sshClient: sshClient}, nil
}

// Upload transfers a local file or directory to the remote host using SCP.
func (s *SCPClient) Upload(ctx context.Context, localPath, remotePath string, opts SCPTransferOptions) error {
	if err := ValidatePath(localPath); err != nil {
		return fmt.Errorf("invalid local path: %w", err)
	}
	if err := ValidatePath(remotePath); err != nil {
		return fmt.Errorf("invalid remote path: %w", err)
	}

	localInfo, err := os.Stat(localPath)
	if err != nil {
		return fmt.Errorf("stat local path: %w", err)
	}

	// Handle file exists mode
	if opts.FileExistsMode != FileExistsModeOverwrite {
		exists, err := s.remoteExists(ctx, remotePath)
		if err != nil {
			return fmt.Errorf("check remote exists: %w", err)
		}
		if exists {
			switch opts.FileExistsMode {
			case FileExistsModeAbort:
				return ErrFileExists
			case FileExistsModeSkip:
				// For skip mode, we need to verify the file is identical
				if localInfo.IsDir() {
					// Can't easily verify directory identity, treat as mismatch
					return ErrFileExistsMismatch
				}
				remoteSize, err := s.remoteFileSize(ctx, remotePath)
				if err != nil {
					return fmt.Errorf("get remote file size: %w", err)
				}
				if remoteSize == localInfo.Size() {
					return nil // Skip identical file
				}
				return ErrFileExistsMismatch
			}
		}
	}

	// Ensure parent directory exists on remote
	remoteDir := filepath.Dir(remotePath)
	if err := s.ensureRemoteDir(ctx, remoteDir); err != nil {
		return fmt.Errorf("create remote directory: %w", err)
	}

	// Build SCP command
	args, knownHostsFile, err := s.buildSCPArgs(ctx, localPath, remotePath, true, localInfo.IsDir() || opts.Recursive, opts.PreservePermissions)
	if err != nil {
		return err
	}
	if knownHostsFile != "" {
		defer os.Remove(knownHostsFile)
	}

	cmd := exec.CommandContext(ctx, "scp", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("scp upload failed: %w: %s", err, stderr.String())
	}

	return nil
}

// Download transfers a remote file or directory to the local host using SCP.
func (s *SCPClient) Download(ctx context.Context, remotePath, localPath string, opts SCPTransferOptions) error {
	if err := ValidatePath(remotePath); err != nil {
		return fmt.Errorf("invalid remote path: %w", err)
	}
	if err := ValidatePath(localPath); err != nil {
		return fmt.Errorf("invalid local path: %w", err)
	}

	// Check if remote is a directory
	isDir, err := s.remoteIsDir(ctx, remotePath)
	if err != nil {
		return fmt.Errorf("check remote path: %w", err)
	}

	// Handle file exists mode
	if opts.FileExistsMode != FileExistsModeOverwrite {
		if localInfo, err := os.Stat(localPath); err == nil {
			switch opts.FileExistsMode {
			case FileExistsModeAbort:
				return ErrFileExists
			case FileExistsModeSkip:
				if isDir {
					return ErrFileExistsMismatch
				}
				remoteSize, sizeErr := s.remoteFileSize(ctx, remotePath)
				if sizeErr != nil {
					return fmt.Errorf("get remote file size: %w", sizeErr)
				}
				if remoteSize == localInfo.Size() {
					return nil // Skip identical file
				}
				return ErrFileExistsMismatch
			}
		}
	}

	// Ensure parent directory exists locally
	localDir := filepath.Dir(localPath)
	if err := os.MkdirAll(localDir, 0755); err != nil {
		return fmt.Errorf("create local directory: %w", err)
	}

	// Build SCP command
	args, knownHostsFile, err := s.buildSCPArgs(ctx, remotePath, localPath, false, isDir || opts.Recursive, opts.PreservePermissions)
	if err != nil {
		return err
	}
	if knownHostsFile != "" {
		defer os.Remove(knownHostsFile)
	}

	cmd := exec.CommandContext(ctx, "scp", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("scp download failed: %w: %s", err, stderr.String())
	}

	return nil
}

// UploadTree recursively uploads a local directory to the remote host.
func (s *SCPClient) UploadTree(ctx context.Context, localDir, remoteDir string, opts SCPTransferOptions) error {
	opts.Recursive = true
	return s.Upload(ctx, localDir, remoteDir, opts)
}

// DownloadTree recursively downloads a remote directory to the local host.
func (s *SCPClient) DownloadTree(ctx context.Context, remoteDir, localDir string, opts SCPTransferOptions) error {
	opts.Recursive = true
	return s.Download(ctx, remoteDir, localDir, opts)
}

// buildSCPArgs constructs the SCP command arguments with proper host key verification.
func (s *SCPClient) buildSCPArgs(ctx context.Context, src, dst string, isUpload, recursive, preservePerms bool) ([]string, string, error) {
	// Capture host key and create temporary known_hosts file
	knownHostsEntry, err := captureHostKey(ctx, s.cfg)
	if err != nil {
		return nil, "", fmt.Errorf("verify host key: %w", err)
	}

	knownHostsFile, err := createTempKnownHosts(knownHostsEntry)
	if err != nil {
		return nil, "", fmt.Errorf("create known_hosts: %w", err)
	}

	args := []string{
		"-P", fmt.Sprintf("%d", s.cfg.Port),
		"-o", "StrictHostKeyChecking=yes",
		"-o", fmt.Sprintf("UserKnownHostsFile=%s", knownHostsFile),
	}

	if s.cfg.PrivateKeyPath != "" {
		args = append(args, "-i", s.cfg.PrivateKeyPath)
	}

	if recursive {
		args = append(args, "-r")
	}

	if preservePerms {
		args = append(args, "-p")
	}

	if isUpload {
		args = append(args, src)
		args = append(args, fmt.Sprintf("%s@%s:%s", s.cfg.Username, s.cfg.Host, dst))
	} else {
		args = append(args, fmt.Sprintf("%s@%s:%s", s.cfg.Username, s.cfg.Host, src))
		args = append(args, dst)
	}

	return args, knownHostsFile, nil
}

// ensureRemoteDir creates a directory on the remote host via SSH.
func (s *SCPClient) ensureRemoteDir(ctx context.Context, dir string) error {
	client, err := s.getSSHClient()
	if err != nil {
		return err
	}

	return client.MkdirAll(ctx, dir)
}

// remoteExists checks if a path exists on the remote host.
func (s *SCPClient) remoteExists(ctx context.Context, path string) (bool, error) {
	client, err := s.getSSHClient()
	if err != nil {
		return false, err
	}

	return client.Exists(ctx, path)
}

// remoteIsDir checks if a remote path is a directory.
func (s *SCPClient) remoteIsDir(ctx context.Context, path string) (bool, error) {
	client, err := s.getSSHClient()
	if err != nil {
		return false, err
	}

	return client.IsDir(ctx, path)
}

// remoteFileSize gets the size of a remote file.
func (s *SCPClient) remoteFileSize(ctx context.Context, path string) (int64, error) {
	client, err := s.getSSHClient()
	if err != nil {
		return 0, err
	}

	result, err := client.Exec(ctx, fmt.Sprintf("stat -c %%s %s 2>/dev/null || stat -f %%z %s", ShellQuote(path), ShellQuote(path)))
	if err != nil {
		return 0, err
	}
	if result.ExitCode != 0 {
		return 0, fmt.Errorf("stat failed: %s", result.Stderr)
	}

	var size int64
	if _, err := fmt.Sscanf(strings.TrimSpace(result.Stdout), "%d", &size); err != nil {
		return 0, fmt.Errorf("parse size: %w", err)
	}

	return size, nil
}

// getSSHClient returns or creates an SSH client for remote operations.
func (s *SCPClient) getSSHClient() (*Client, error) {
	if s.sshClient != nil {
		return s.sshClient, nil
	}

	client, _, err := New(s.cfg)
	if err != nil {
		return nil, err
	}
	s.sshClient = client
	return client, nil
}

// Close closes the underlying SSH client if owned.
func (s *SCPClient) Close() error {
	if s.sshClient != nil {
		return s.sshClient.Close()
	}
	return nil
}

// SCPRemoteToRemoteOptions configures remote-to-remote SCP transfers.
type SCPRemoteToRemoteOptions struct {
	SCPTransferOptions

	// UseRelay forces data to flow through QUI even if direct transfer might be possible.
	UseRelay bool
}

// SCPRelayTransfer transfers files between two remote hosts by relaying through local.
// This downloads from source to a temp location, then uploads to destination.
func SCPRelayTransfer(ctx context.Context, srcCfg, dstCfg *Config, srcPath, dstPath string, opts SCPRemoteToRemoteOptions) error {
	if err := ValidatePath(srcPath); err != nil {
		return fmt.Errorf("invalid source path: %w", err)
	}
	if err := ValidatePath(dstPath); err != nil {
		return fmt.Errorf("invalid destination path: %w", err)
	}

	srcClient, err := NewSCPClient(srcCfg)
	if err != nil {
		return fmt.Errorf("create source SCP client: %w", err)
	}
	defer srcClient.Close()

	dstClient, err := NewSCPClient(dstCfg)
	if err != nil {
		return fmt.Errorf("create destination SCP client: %w", err)
	}
	defer dstClient.Close()

	// Check if source is a directory
	isDir, err := srcClient.remoteIsDir(ctx, srcPath)
	if err != nil {
		return fmt.Errorf("check source path: %w", err)
	}

	// Create temp directory for relay
	tempDir, err := os.MkdirTemp("", "scp-relay-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	baseName := filepath.Base(srcPath)
	tempPath := filepath.Join(tempDir, baseName)

	// Download from source
	downloadOpts := SCPTransferOptions{
		PreservePermissions: opts.PreservePermissions,
		Recursive:           isDir,
		FileExistsMode:      FileExistsModeOverwrite,
	}
	if err := srcClient.Download(ctx, srcPath, tempPath, downloadOpts); err != nil {
		return fmt.Errorf("download from source: %w", err)
	}

	// Upload to destination
	uploadOpts := SCPTransferOptions{
		PreservePermissions: opts.PreservePermissions,
		Recursive:           isDir,
		FileExistsMode:      opts.FileExistsMode,
	}
	if err := dstClient.Upload(ctx, tempPath, dstPath, uploadOpts); err != nil {
		return fmt.Errorf("upload to destination: %w", err)
	}

	return nil
}

// SCPPushWithOptions transfers files from local to remote using SCP with full options.
func SCPPushWithOptions(ctx context.Context, cfg *Config, localPath, remotePath string, opts SCPTransferOptions) error {
	client, err := NewSCPClient(cfg)
	if err != nil {
		return err
	}
	defer client.Close()

	return client.Upload(ctx, localPath, remotePath, opts)
}

// SCPPullWithOptions transfers files from remote to local using SCP with full options.
func SCPPullWithOptions(ctx context.Context, cfg *Config, remotePath, localPath string, opts SCPTransferOptions) error {
	client, err := NewSCPClient(cfg)
	if err != nil {
		return err
	}
	defer client.Close()

	return client.Download(ctx, remotePath, localPath, opts)
}

// scpTransferProgress tracks progress for SCP directory transfers.
type scpTransferProgress struct {
	totalBytes       int64
	transferredBytes int64
	onProgress       func(transferred, total int64)
}

// calculateTotalSize calculates the total size of a local directory.
func calculateTotalSize(path string) (int64, error) {
	var total int64
	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total, err
}

// SCPPushTree recursively copies a local directory to a remote host.
// This is an alias for Upload with recursive=true.
func (s *SCPClient) SCPPushTree(ctx context.Context, localDir, remoteDir string, opts SCPTransferOptions) error {
	return s.UploadTree(ctx, localDir, remoteDir, opts)
}

// SCPPullTree recursively copies a remote directory to the local host.
// This is an alias for Download with recursive=true.
func (s *SCPClient) SCPPullTree(ctx context.Context, remoteDir, localDir string, opts SCPTransferOptions) error {
	return s.DownloadTree(ctx, remoteDir, localDir, opts)
}

// scpRemoveDir removes a file or directory on the remote host.
// Used for cleanup on failure.
func (s *SCPClient) scpRemoveDir(ctx context.Context, path string) error {
	client, err := s.getSSHClient()
	if err != nil {
		return err
	}

	return client.Remove(ctx, path)
}

// fileExistsModeToSCPForce converts FileExistsMode to a force boolean for backwards compatibility.
func fileExistsModeToSCPForce(mode FileExistsMode) bool {
	return mode == FileExistsModeOverwrite
}

// Log helper for SCP operations.
func logSCPOperation(operation, src, dst string, err error) {
	if err != nil {
		log.Warn().Err(err).Str("op", operation).Str("src", src).Str("dst", dst).Msg("SCP operation failed")
	} else {
		log.Debug().Str("op", operation).Str("src", src).Str("dst", dst).Msg("SCP operation completed")
	}
}

// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sshclient

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/pkg/sftp"
	"github.com/rs/zerolog/log"
)

// SFTPClient wraps an SFTP connection for file transfer operations.
type SFTPClient struct {
	client     *sftp.Client
	sshClient  *Client
	ownSSH     bool // true if we created the SSH client and should close it
}

// NewSFTPClient creates a new SFTP client from an existing SSH client.
func (c *Client) NewSFTPClient() (*SFTPClient, error) {
	sftpClient, err := sftp.NewClient(c.client)
	if err != nil {
		return nil, fmt.Errorf("create sftp client: %w", err)
	}

	return &SFTPClient{
		client:    sftpClient,
		sshClient: c,
		ownSSH:    false,
	}, nil
}

// NewSFTPClientFromConfig creates a new SFTP client from SSH configuration.
// The caller is responsible for closing the client when done.
//
// Host key verification behavior:
//   - If cfg.ExpectedHostKey is set, the host key will be verified against it
//   - If cfg.SkipHostKeyVerification is true, verification is skipped (insecure)
//   - Otherwise, new keys are accepted (TOFU behavior)
func NewSFTPClientFromConfig(cfg *Config) (*SFTPClient, error) {
	sshClient, _, err := New(cfg)
	if err != nil {
		return nil, err
	}

	sftpClient, err := sftp.NewClient(sshClient.client)
	if err != nil {
		sshClient.Close()
		return nil, fmt.Errorf("create sftp client: %w", err)
	}

	return &SFTPClient{
		client:    sftpClient,
		sshClient: sshClient,
		ownSSH:    true,
	}, nil
}

// Close closes the SFTP client and optionally the underlying SSH connection.
func (s *SFTPClient) Close() error {
	if err := s.client.Close(); err != nil {
		return err
	}
	if s.ownSSH && s.sshClient != nil {
		return s.sshClient.Close()
	}
	return nil
}

// Client returns the underlying sftp.Client for advanced operations.
func (s *SFTPClient) Client() *sftp.Client {
	return s.client
}

// Stat returns file info for a remote path.
func (s *SFTPClient) Stat(path string) (os.FileInfo, error) {
	return s.client.Stat(path)
}

// Exists checks if a remote path exists.
func (s *SFTPClient) Exists(path string) (bool, error) {
	_, err := s.client.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// IsDir checks if a remote path is a directory.
func (s *SFTPClient) IsDir(path string) (bool, error) {
	info, err := s.client.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return info.IsDir(), nil
}

// MkdirAll creates a directory and all parent directories on the remote host.
func (s *SFTPClient) MkdirAll(path string) error {
	return s.client.MkdirAll(path)
}

// Remove removes a file on the remote host.
func (s *SFTPClient) Remove(path string) error {
	return s.client.Remove(path)
}

// RemoveAll removes a file or directory recursively on the remote host.
func (s *SFTPClient) RemoveAll(path string) error {
	info, err := s.client.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	if !info.IsDir() {
		return s.client.Remove(path)
	}

	// Walk the directory and remove all contents
	walker := s.client.Walk(path)
	var files []string
	var dirs []string

	for walker.Step() {
		if err := walker.Err(); err != nil {
			return fmt.Errorf("walk %s: %w", walker.Path(), err)
		}
		if walker.Stat().IsDir() {
			dirs = append(dirs, walker.Path())
		} else {
			files = append(files, walker.Path())
		}
	}

	// Remove files first
	for _, f := range files {
		if err := s.client.Remove(f); err != nil {
			return fmt.Errorf("remove file %s: %w", f, err)
		}
	}

	// Remove directories in reverse order (deepest first)
	for i := len(dirs) - 1; i >= 0; i-- {
		if err := s.client.RemoveDirectory(dirs[i]); err != nil {
			return fmt.Errorf("remove dir %s: %w", dirs[i], err)
		}
	}

	return nil
}

// ReadDir returns the contents of a remote directory.
func (s *SFTPClient) ReadDir(path string) ([]os.FileInfo, error) {
	return s.client.ReadDir(path)
}

// Open opens a remote file for reading.
func (s *SFTPClient) Open(path string) (*sftp.File, error) {
	return s.client.Open(path)
}

// Create creates a remote file for writing.
func (s *SFTPClient) Create(path string) (*sftp.File, error) {
	return s.client.Create(path)
}

// OpenFile opens a remote file with the specified flags and permissions.
func (s *SFTPClient) OpenFile(path string, flags int, mode os.FileMode) (*sftp.File, error) {
	return s.client.OpenFile(path, flags)
}

// Chmod changes the permissions of a remote file.
func (s *SFTPClient) Chmod(path string, mode os.FileMode) error {
	return s.client.Chmod(path, mode)
}

// Rename renames a remote file or directory.
func (s *SFTPClient) Rename(oldpath, newpath string) error {
	return s.client.Rename(oldpath, newpath)
}

// Symlink creates a symbolic link.
func (s *SFTPClient) Symlink(oldname, newname string) error {
	return s.client.Symlink(oldname, newname)
}

// Readlink returns the target of a symbolic link.
func (s *SFTPClient) Readlink(path string) (string, error) {
	return s.client.ReadLink(path)
}

// CopyLocalToRemote copies a local file to the remote host.
func (s *SFTPClient) CopyLocalToRemote(localPath, remotePath string) error {
	localFile, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("open local file: %w", err)
	}
	defer localFile.Close()

	// Get local file info for permissions
	localInfo, err := localFile.Stat()
	if err != nil {
		return fmt.Errorf("stat local file: %w", err)
	}

	// Create parent directory if needed
	remoteDir := filepath.Dir(remotePath)
	if err := s.MkdirAll(remoteDir); err != nil {
		return fmt.Errorf("create remote directory: %w", err)
	}

	remoteFile, err := s.client.Create(remotePath)
	if err != nil {
		return fmt.Errorf("create remote file: %w", err)
	}
	defer remoteFile.Close()

	if _, err := io.Copy(remoteFile, localFile); err != nil {
		return fmt.Errorf("copy data: %w", err)
	}

	// Preserve permissions (non-fatal if it fails, but log the error)
	if err := s.client.Chmod(remotePath, localInfo.Mode()); err != nil {
		log.Warn().Err(err).Str("path", remotePath).Msg("failed to preserve permissions on remote file")
	}

	return nil
}

// CopyRemoteToLocal copies a remote file to the local host.
func (s *SFTPClient) CopyRemoteToLocal(remotePath, localPath string) error {
	remoteFile, err := s.client.Open(remotePath)
	if err != nil {
		return fmt.Errorf("open remote file: %w", err)
	}
	defer remoteFile.Close()

	// Get remote file info for permissions
	remoteInfo, err := remoteFile.Stat()
	if err != nil {
		return fmt.Errorf("stat remote file: %w", err)
	}

	// Create parent directory if needed
	localDir := filepath.Dir(localPath)
	if err := os.MkdirAll(localDir, 0755); err != nil {
		return fmt.Errorf("create local directory: %w", err)
	}

	localFile, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("create local file: %w", err)
	}
	defer localFile.Close()

	if _, err := io.Copy(localFile, remoteFile); err != nil {
		return fmt.Errorf("copy data: %w", err)
	}

	// Preserve permissions (non-fatal if it fails, but log the error)
	if err := os.Chmod(localPath, remoteInfo.Mode()); err != nil {
		log.Warn().Err(err).Str("path", localPath).Msg("failed to preserve permissions on local file")
	}

	return nil
}

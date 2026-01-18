// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sshclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ErrFileExists is returned when a file already exists and Force is not set.
var ErrFileExists = errors.New("file already exists")

// SFTPTransferOptions configures SFTP transfer behavior.
type SFTPTransferOptions struct {
	// PreservePermissions preserves file permissions during transfer.
	PreservePermissions bool

	// Force allows overwriting existing files. If false and the target exists,
	// the transfer will fail with ErrFileExists.
	Force bool

	// BufferSize is the size of the transfer buffer (default 32KB).
	BufferSize int

	// OnProgress is called periodically with transfer progress.
	// bytesTransferred is cumulative, bytesTotal is the total for the operation.
	OnProgress func(bytesTransferred, bytesTotal int64)
}

const defaultBufferSize = 32 * 1024 // 32KB

// progressWriter wraps an io.Writer to track bytes written.
type progressWriter struct {
	writer      io.Writer
	transferred int64
	total       int64
	onProgress  func(transferred, total int64)
}

func (pw *progressWriter) Write(p []byte) (n int, err error) {
	n, err = pw.writer.Write(p)
	pw.transferred += int64(n)
	if pw.onProgress != nil {
		pw.onProgress(pw.transferred, pw.total)
	}
	return
}

// progressReader wraps an io.Reader to track bytes read.
type progressReader struct {
	reader      io.Reader
	transferred int64
	total       int64
	onProgress  func(transferred, total int64)
}

func (pr *progressReader) Read(p []byte) (n int, err error) {
	n, err = pr.reader.Read(p)
	pr.transferred += int64(n)
	if pr.onProgress != nil {
		pr.onProgress(pr.transferred, pr.total)
	}
	return
}

// Upload transfers a local file to the remote host.
func (s *SFTPClient) Upload(ctx context.Context, localPath, remotePath string, opts SFTPTransferOptions) error {
	if err := ValidatePath(localPath); err != nil {
		return fmt.Errorf("invalid local path: %w", err)
	}
	if err := ValidatePath(remotePath); err != nil {
		return fmt.Errorf("invalid remote path: %w", err)
	}

	localFile, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("open local file: %w", err)
	}
	defer localFile.Close()

	localInfo, err := localFile.Stat()
	if err != nil {
		return fmt.Errorf("stat local file: %w", err)
	}

	// Create parent directory
	remoteDir := filepath.Dir(remotePath)
	if err := s.MkdirAll(remoteDir); err != nil {
		return fmt.Errorf("create remote directory: %w", err)
	}

	// Check if file exists and Force is not set
	if !opts.Force {
		if _, err := s.client.Stat(remotePath); err == nil {
			return ErrFileExists
		}
	}

	remoteFile, err := s.client.Create(remotePath)
	if err != nil {
		return fmt.Errorf("create remote file: %w", err)
	}
	defer remoteFile.Close()

	bufSize := opts.BufferSize
	if bufSize <= 0 {
		bufSize = defaultBufferSize
	}

	var reader io.Reader = localFile
	if opts.OnProgress != nil {
		reader = &progressReader{
			reader:     localFile,
			total:      localInfo.Size(),
			onProgress: opts.OnProgress,
		}
	}

	// Check for context cancellation periodically during copy
	done := make(chan error, 1)
	go func() {
		buf := make([]byte, bufSize)
		_, err := io.CopyBuffer(remoteFile, reader, buf)
		done <- err
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		if err != nil {
			return fmt.Errorf("copy data: %w", err)
		}
	}

	// Preserve permissions
	if opts.PreservePermissions {
		if err := s.client.Chmod(remotePath, localInfo.Mode()); err != nil {
			// Non-fatal, log and continue
		}
	}

	return nil
}

// Download transfers a remote file to the local host.
func (s *SFTPClient) Download(ctx context.Context, remotePath, localPath string, opts SFTPTransferOptions) error {
	if err := ValidatePath(remotePath); err != nil {
		return fmt.Errorf("invalid remote path: %w", err)
	}
	if err := ValidatePath(localPath); err != nil {
		return fmt.Errorf("invalid local path: %w", err)
	}

	remoteFile, err := s.client.Open(remotePath)
	if err != nil {
		return fmt.Errorf("open remote file: %w", err)
	}
	defer remoteFile.Close()

	remoteInfo, err := remoteFile.Stat()
	if err != nil {
		return fmt.Errorf("stat remote file: %w", err)
	}

	// Create parent directory
	localDir := filepath.Dir(localPath)
	if err := os.MkdirAll(localDir, 0755); err != nil {
		return fmt.Errorf("create local directory: %w", err)
	}

	// Check if file exists and Force is not set
	if !opts.Force {
		if _, err := os.Stat(localPath); err == nil {
			return ErrFileExists
		}
	}

	localFile, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("create local file: %w", err)
	}
	defer localFile.Close()

	bufSize := opts.BufferSize
	if bufSize <= 0 {
		bufSize = defaultBufferSize
	}

	var reader io.Reader = remoteFile
	if opts.OnProgress != nil {
		reader = &progressReader{
			reader:     remoteFile,
			total:      remoteInfo.Size(),
			onProgress: opts.OnProgress,
		}
	}

	done := make(chan error, 1)
	go func() {
		buf := make([]byte, bufSize)
		_, err := io.CopyBuffer(localFile, reader, buf)
		done <- err
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		if err != nil {
			return fmt.Errorf("copy data: %w", err)
		}
	}

	// Preserve permissions
	if opts.PreservePermissions {
		if err := os.Chmod(localPath, remoteInfo.Mode()); err != nil {
			// Non-fatal
		}
	}

	return nil
}

// UploadTree recursively uploads a local directory to the remote host.
func (s *SFTPClient) UploadTree(ctx context.Context, localDir, remoteDir string, opts SFTPTransferOptions) error {
	if err := ValidatePath(localDir); err != nil {
		return fmt.Errorf("invalid local path: %w", err)
	}
	if err := ValidatePath(remoteDir); err != nil {
		return fmt.Errorf("invalid remote path: %w", err)
	}

	// Calculate total size for progress tracking
	var totalBytes int64
	var files []string

	err := filepath.Walk(localDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			totalBytes += info.Size()
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("walk local directory: %w", err)
	}

	var transferred int64
	progressCallback := func(fileTransferred, fileTotal int64) {
		if opts.OnProgress != nil {
			opts.OnProgress(transferred+fileTransferred, totalBytes)
		}
	}

	// Upload each file
	for _, localPath := range files {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		relPath, err := filepath.Rel(localDir, localPath)
		if err != nil {
			return fmt.Errorf("get relative path: %w", err)
		}
		remotePath := filepath.Join(remoteDir, relPath)
		// Ensure forward slashes for remote paths
		remotePath = strings.ReplaceAll(remotePath, "\\", "/")

		localInfo, err := os.Stat(localPath)
		if err != nil {
			return fmt.Errorf("stat %s: %w", localPath, err)
		}

		fileOpts := SFTPTransferOptions{
			PreservePermissions: opts.PreservePermissions,
			Force:               opts.Force,
			BufferSize:          opts.BufferSize,
			OnProgress:          progressCallback,
		}

		if err := s.Upload(ctx, localPath, remotePath, fileOpts); err != nil {
			return fmt.Errorf("upload %s: %w", localPath, err)
		}

		transferred += localInfo.Size()
	}

	return nil
}

// DownloadTree recursively downloads a remote directory to the local host.
func (s *SFTPClient) DownloadTree(ctx context.Context, remoteDir, localDir string, opts SFTPTransferOptions) error {
	if err := ValidatePath(remoteDir); err != nil {
		return fmt.Errorf("invalid remote path: %w", err)
	}
	if err := ValidatePath(localDir); err != nil {
		return fmt.Errorf("invalid local path: %w", err)
	}

	// Calculate total size and collect files
	var totalBytes int64
	type fileEntry struct {
		remotePath string
		size       int64
	}
	var files []fileEntry

	walker := s.client.Walk(remoteDir)
	for walker.Step() {
		if walker.Err() != nil {
			continue
		}
		if !walker.Stat().IsDir() {
			totalBytes += walker.Stat().Size()
			files = append(files, fileEntry{
				remotePath: walker.Path(),
				size:       walker.Stat().Size(),
			})
		}
	}

	var transferred int64
	progressCallback := func(fileTransferred, fileTotal int64) {
		if opts.OnProgress != nil {
			opts.OnProgress(transferred+fileTransferred, totalBytes)
		}
	}

	// Download each file
	for _, f := range files {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		relPath, err := filepath.Rel(remoteDir, f.remotePath)
		if err != nil {
			return fmt.Errorf("get relative path: %w", err)
		}
		localPath := filepath.Join(localDir, relPath)

		fileOpts := SFTPTransferOptions{
			PreservePermissions: opts.PreservePermissions,
			Force:               opts.Force,
			BufferSize:          opts.BufferSize,
			OnProgress:          progressCallback,
		}

		if err := s.Download(ctx, f.remotePath, localPath, fileOpts); err != nil {
			return fmt.Errorf("download %s: %w", f.remotePath, err)
		}

		transferred += f.size
	}

	return nil
}

// SFTPRemoteToRemoteOptions configures remote-to-remote SFTP transfers.
type SFTPRemoteToRemoteOptions struct {
	SFTPTransferOptions

	// UseRelay forces data to flow through QUI even if direct transfer might be possible.
	UseRelay bool
}

// SFTPRelayTransfer transfers files between two remote hosts by relaying through local.
// This is a fallback when direct transfer isn't possible.
func SFTPRelayTransfer(ctx context.Context, src, dst *SFTPClient, srcPath, dstPath string, opts SFTPRemoteToRemoteOptions) error {
	if err := ValidatePath(srcPath); err != nil {
		return fmt.Errorf("invalid source path: %w", err)
	}
	if err := ValidatePath(dstPath); err != nil {
		return fmt.Errorf("invalid destination path: %w", err)
	}

	srcInfo, err := src.Stat(srcPath)
	if err != nil {
		return fmt.Errorf("stat source: %w", err)
	}

	if srcInfo.IsDir() {
		return sftpRelayDir(ctx, src, dst, srcPath, dstPath, opts)
	}

	return sftpRelayFile(ctx, src, dst, srcPath, dstPath, srcInfo.Size(), opts)
}

// sftpRelayFile transfers a single file between remote hosts.
func sftpRelayFile(ctx context.Context, src, dst *SFTPClient, srcPath, dstPath string, size int64, opts SFTPRemoteToRemoteOptions) error {
	srcFile, err := src.client.Open(srcPath)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer srcFile.Close()

	// Create parent directory on destination
	dstDir := filepath.Dir(dstPath)
	if err := dst.MkdirAll(dstDir); err != nil {
		return fmt.Errorf("create dest directory: %w", err)
	}

	// Check if file exists and Force is not set
	if !opts.Force {
		if _, err := dst.client.Stat(dstPath); err == nil {
			return ErrFileExists
		}
	}

	dstFile, err := dst.client.Create(dstPath)
	if err != nil {
		return fmt.Errorf("create dest file: %w", err)
	}
	defer dstFile.Close()

	bufSize := opts.BufferSize
	if bufSize <= 0 {
		bufSize = defaultBufferSize
	}

	var reader io.Reader = srcFile
	if opts.OnProgress != nil {
		reader = &progressReader{
			reader:     srcFile,
			total:      size,
			onProgress: opts.OnProgress,
		}
	}

	done := make(chan error, 1)
	go func() {
		buf := make([]byte, bufSize)
		_, err := io.CopyBuffer(dstFile, reader, buf)
		done <- err
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		if err != nil {
			return fmt.Errorf("copy data: %w", err)
		}
	}

	// Preserve permissions
	if opts.PreservePermissions {
		srcInfo, _ := srcFile.Stat()
		if srcInfo != nil {
			_ = dst.client.Chmod(dstPath, srcInfo.Mode())
		}
	}

	return nil
}

// sftpRelayDir transfers a directory between remote hosts.
func sftpRelayDir(ctx context.Context, src, dst *SFTPClient, srcDir, dstDir string, opts SFTPRemoteToRemoteOptions) error {
	// Calculate total size
	var totalBytes int64
	type fileEntry struct {
		path string
		size int64
	}
	var files []fileEntry

	walker := src.client.Walk(srcDir)
	for walker.Step() {
		if walker.Err() != nil {
			continue
		}
		if !walker.Stat().IsDir() {
			totalBytes += walker.Stat().Size()
			files = append(files, fileEntry{
				path: walker.Path(),
				size: walker.Stat().Size(),
			})
		}
	}

	var transferred int64
	progressCallback := func(fileTransferred, fileTotal int64) {
		if opts.OnProgress != nil {
			opts.OnProgress(transferred+fileTransferred, totalBytes)
		}
	}

	for _, f := range files {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		relPath, err := filepath.Rel(srcDir, f.path)
		if err != nil {
			return fmt.Errorf("get relative path: %w", err)
		}
		dstPath := filepath.Join(dstDir, relPath)
		dstPath = strings.ReplaceAll(dstPath, "\\", "/")

		fileOpts := SFTPRemoteToRemoteOptions{
			SFTPTransferOptions: SFTPTransferOptions{
				PreservePermissions: opts.PreservePermissions,
				Force:               opts.Force,
				BufferSize:          opts.BufferSize,
				OnProgress:          progressCallback,
			},
		}

		if err := sftpRelayFile(ctx, src, dst, f.path, dstPath, f.size, fileOpts); err != nil {
			return fmt.Errorf("relay %s: %w", f.path, err)
		}

		transferred += f.size
	}

	return nil
}

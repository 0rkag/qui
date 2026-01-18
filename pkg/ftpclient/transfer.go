// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package ftpclient

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"

	"github.com/jlaffaye/ftp"
)

// TransferOptions configures FTP transfer behavior.
type TransferOptions struct {
	// PreservePermissions attempts to preserve file permissions (limited on FTP).
	PreservePermissions bool

	// OnProgress is called periodically with transfer progress.
	OnProgress func(bytesTransferred, bytesTotal int64)
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

// Upload transfers a local file to the remote host.
func (c *Client) Upload(ctx context.Context, localPath, remotePath string, opts TransferOptions) error {
	if err := validatePath(remotePath); err != nil {
		return err
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
	remoteDir := path.Dir(remotePath)
	if err := c.MkdirAll(remoteDir); err != nil {
		return fmt.Errorf("create remote directory: %w", err)
	}

	var reader io.Reader = localFile
	if opts.OnProgress != nil {
		reader = &progressReader{
			reader:     localFile,
			total:      localInfo.Size(),
			onProgress: opts.OnProgress,
		}
	}

	// Check for context cancellation
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if err := c.conn.Stor(remotePath, reader); err != nil {
		// Clean up partial file on failure
		_ = c.conn.Delete(remotePath)
		return fmt.Errorf("store file: %w", err)
	}

	return nil
}

// Download transfers a remote file to the local host.
func (c *Client) Download(ctx context.Context, remotePath, localPath string, opts TransferOptions) error {
	if err := validatePath(remotePath); err != nil {
		return err
	}

	// Get remote file size for progress tracking
	size, err := c.FileSize(remotePath)
	if err != nil {
		// Try to continue without size (progress will be inaccurate)
		size = 0
	}

	// Create parent directory locally
	localDir := filepath.Dir(localPath)
	if err := os.MkdirAll(localDir, 0755); err != nil {
		return fmt.Errorf("create local directory: %w", err)
	}

	// Check for context cancellation
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	resp, err := c.conn.Retr(remotePath)
	if err != nil {
		return fmt.Errorf("retrieve file: %w", err)
	}
	defer resp.Close()

	localFile, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("create local file: %w", err)
	}

	// Use a flag to track if transfer completed successfully
	success := false
	defer func() {
		localFile.Close()
		if !success {
			// Clean up partial file on failure
			_ = os.Remove(localPath)
		}
	}()

	var writer io.Writer = localFile
	if opts.OnProgress != nil {
		writer = &progressWriter{
			writer:     localFile,
			total:      size,
			onProgress: opts.OnProgress,
		}
	}

	if _, err := io.Copy(writer, resp); err != nil {
		return fmt.Errorf("copy data: %w", err)
	}

	success = true
	return nil
}

// UploadTree recursively uploads a local directory to the remote host.
func (c *Client) UploadTree(ctx context.Context, localDir, remoteDir string, opts TransferOptions) error {
	if err := validatePath(remoteDir); err != nil {
		return err
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
		remotePath := path.Join(remoteDir, relPath)

		localInfo, err := os.Stat(localPath)
		if err != nil {
			return fmt.Errorf("stat %s: %w", localPath, err)
		}

		fileOpts := TransferOptions{
			PreservePermissions: opts.PreservePermissions,
			OnProgress:          progressCallback,
		}

		if err := c.Upload(ctx, localPath, remotePath, fileOpts); err != nil {
			return fmt.Errorf("upload %s: %w", localPath, err)
		}

		transferred += localInfo.Size()
	}

	return nil
}

// DownloadTree recursively downloads a remote directory to the local host.
func (c *Client) DownloadTree(ctx context.Context, remoteDir, localDir string, opts TransferOptions) error {
	if err := validatePath(remoteDir); err != nil {
		return err
	}

	// Calculate total size and collect files
	var totalBytes int64
	type fileEntry struct {
		remotePath string
		size       int64
	}
	var files []fileEntry

	err := c.Walk(remoteDir, func(entryPath string, entry *ftp.Entry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type == ftp.EntryTypeFile {
			totalBytes += int64(entry.Size)
			files = append(files, fileEntry{
				remotePath: entryPath,
				size:       int64(entry.Size),
			})
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("walk remote directory: %w", err)
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

		fileOpts := TransferOptions{
			PreservePermissions: opts.PreservePermissions,
			OnProgress:          progressCallback,
		}

		if err := c.Download(ctx, f.remotePath, localPath, fileOpts); err != nil {
			return fmt.Errorf("download %s: %w", f.remotePath, err)
		}

		transferred += f.size
	}

	return nil
}

// RelayTransfer transfers files between two FTP servers via local relay.
// This downloads from source and uploads to destination through QUI.
func RelayTransfer(ctx context.Context, src, dst *Client, srcPath, dstPath string, opts TransferOptions) error {
	if err := validatePath(srcPath); err != nil {
		return fmt.Errorf("source path: %w", err)
	}
	if err := validatePath(dstPath); err != nil {
		return fmt.Errorf("dest path: %w", err)
	}

	// Check if source is a directory
	entry, err := src.Stat(srcPath)
	if err != nil {
		return fmt.Errorf("stat source: %w", err)
	}

	if entry.Type == ftp.EntryTypeFolder {
		return relayDir(ctx, src, dst, srcPath, dstPath, opts)
	}

	return relayFile(ctx, src, dst, srcPath, dstPath, int64(entry.Size), opts)
}

// relayFile transfers a single file between FTP servers.
func relayFile(ctx context.Context, src, dst *Client, srcPath, dstPath string, size int64, opts TransferOptions) error {
	// Create destination directory
	dstDir := path.Dir(dstPath)
	if err := dst.MkdirAll(dstDir); err != nil {
		return fmt.Errorf("create dest directory: %w", err)
	}

	// Open source file
	resp, err := src.conn.Retr(srcPath)
	if err != nil {
		return fmt.Errorf("retrieve source: %w", err)
	}
	defer resp.Close()

	var reader io.Reader = resp
	if opts.OnProgress != nil {
		reader = &progressReader{
			reader:     resp,
			total:      size,
			onProgress: opts.OnProgress,
		}
	}

	// Check for context cancellation
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Store to destination
	if err := dst.conn.Stor(dstPath, reader); err != nil {
		return fmt.Errorf("store to dest: %w", err)
	}

	return nil
}

// relayDir transfers a directory between FTP servers.
func relayDir(ctx context.Context, src, dst *Client, srcDir, dstDir string, opts TransferOptions) error {
	// Calculate total size
	var totalBytes int64
	type fileEntry struct {
		path string
		size int64
	}
	var files []fileEntry

	err := src.Walk(srcDir, func(entryPath string, entry *ftp.Entry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type == ftp.EntryTypeFile {
			totalBytes += int64(entry.Size)
			files = append(files, fileEntry{
				path: entryPath,
				size: int64(entry.Size),
			})
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("walk source: %w", err)
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
		targetPath := path.Join(dstDir, relPath)

		fileOpts := TransferOptions{
			PreservePermissions: opts.PreservePermissions,
			OnProgress:          progressCallback,
		}

		if err := relayFile(ctx, src, dst, f.path, targetPath, f.size, fileOpts); err != nil {
			return fmt.Errorf("relay %s: %w", f.path, err)
		}

		transferred += f.size
	}

	return nil
}

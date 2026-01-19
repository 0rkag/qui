// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package ftpclient

import (
	"bytes"
	"context"
	"io"
	"testing"
)

func TestTransferOptions_EffectiveFileExistsMode(t *testing.T) {
	tests := []struct {
		name     string
		opts     TransferOptions
		expected FileExistsMode
	}{
		{
			name:     "Force=true overrides mode",
			opts:     TransferOptions{Force: true, FileExistsMode: FileExistsModeAbort},
			expected: FileExistsModeOverwrite,
		},
		{
			name:     "Force=true overrides skip",
			opts:     TransferOptions{Force: true, FileExistsMode: FileExistsModeSkip},
			expected: FileExistsModeOverwrite,
		},
		{
			name:     "Force=false uses mode - abort",
			opts:     TransferOptions{Force: false, FileExistsMode: FileExistsModeAbort},
			expected: FileExistsModeAbort,
		},
		{
			name:     "Force=false uses mode - skip",
			opts:     TransferOptions{Force: false, FileExistsMode: FileExistsModeSkip},
			expected: FileExistsModeSkip,
		},
		{
			name:     "Force=false uses mode - overwrite",
			opts:     TransferOptions{Force: false, FileExistsMode: FileExistsModeOverwrite},
			expected: FileExistsModeOverwrite,
		},
		{
			name:     "default mode is abort",
			opts:     TransferOptions{},
			expected: FileExistsModeAbort,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.opts.effectiveFileExistsMode()
			if got != tt.expected {
				t.Errorf("effectiveFileExistsMode() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestFileExistsModeConstants(t *testing.T) {
	// Verify constants are distinct
	modes := []FileExistsMode{
		FileExistsModeAbort,
		FileExistsModeSkip,
		FileExistsModeOverwrite,
	}

	seen := make(map[FileExistsMode]bool)
	for _, mode := range modes {
		if seen[mode] {
			t.Errorf("Duplicate FileExistsMode value: %v", mode)
		}
		seen[mode] = true
	}

	// Verify abort is 0 (default)
	if FileExistsModeAbort != 0 {
		t.Errorf("FileExistsModeAbort = %v, want 0 (default)", FileExistsModeAbort)
	}
}

func TestProgressReader_Tracking(t *testing.T) {
	data := []byte("Hello, World! This is test data for progress tracking.")
	reader := bytes.NewReader(data)

	var lastTransferred, lastTotal int64
	callCount := 0

	pr := &progressReader{
		reader: reader,
		total:  int64(len(data)),
		onProgress: func(transferred, total int64) {
			lastTransferred = transferred
			lastTotal = total
			callCount++
		},
	}

	// Read all data in small chunks
	buf := make([]byte, 10)
	var totalRead int64
	for {
		n, err := pr.Read(buf)
		totalRead += int64(n)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Read() error = %v", err)
		}
	}

	// Verify all bytes were read
	if totalRead != int64(len(data)) {
		t.Errorf("Total read = %d, want %d", totalRead, len(data))
	}

	// Verify progress was tracked
	if lastTransferred != int64(len(data)) {
		t.Errorf("lastTransferred = %d, want %d", lastTransferred, len(data))
	}
	if lastTotal != int64(len(data)) {
		t.Errorf("lastTotal = %d, want %d", lastTotal, len(data))
	}

	// Verify callback was called multiple times
	if callCount == 0 {
		t.Error("Progress callback was never called")
	}
}

func TestProgressReader_NilCallback(t *testing.T) {
	data := []byte("test data")
	reader := bytes.NewReader(data)

	pr := &progressReader{
		reader:     reader,
		total:      int64(len(data)),
		onProgress: nil, // No callback
	}

	// Should not panic with nil callback
	buf := make([]byte, len(data))
	n, err := pr.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("Read() error = %v", err)
	}
	if n != len(data) {
		t.Errorf("Read() n = %d, want %d", n, len(data))
	}
}

func TestProgressWriter_Callback(t *testing.T) {
	var buf bytes.Buffer
	data := []byte("Hello, World! This is test data for progress tracking.")

	var lastTransferred, lastTotal int64
	callCount := 0

	pw := &progressWriter{
		writer: &buf,
		total:  int64(len(data)),
		onProgress: func(transferred, total int64) {
			lastTransferred = transferred
			lastTotal = total
			callCount++
		},
	}

	// Write data in chunks
	chunkSize := 10
	for i := 0; i < len(data); i += chunkSize {
		end := i + chunkSize
		if end > len(data) {
			end = len(data)
		}
		n, err := pw.Write(data[i:end])
		if err != nil {
			t.Fatalf("Write() error = %v", err)
		}
		if n != end-i {
			t.Errorf("Write() n = %d, want %d", n, end-i)
		}
	}

	// Verify all bytes were written
	if buf.Len() != len(data) {
		t.Errorf("Buffer length = %d, want %d", buf.Len(), len(data))
	}

	// Verify progress was tracked
	if lastTransferred != int64(len(data)) {
		t.Errorf("lastTransferred = %d, want %d", lastTransferred, len(data))
	}
	if lastTotal != int64(len(data)) {
		t.Errorf("lastTotal = %d, want %d", lastTotal, len(data))
	}

	// Verify callback was called
	if callCount == 0 {
		t.Error("Progress callback was never called")
	}
}

func TestProgressWriter_NilCallback(t *testing.T) {
	var buf bytes.Buffer
	data := []byte("test data")

	pw := &progressWriter{
		writer:     &buf,
		total:      int64(len(data)),
		onProgress: nil, // No callback
	}

	// Should not panic with nil callback
	n, err := pw.Write(data)
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if n != len(data) {
		t.Errorf("Write() n = %d, want %d", n, len(data))
	}
}

func TestUpload_PathValidation(t *testing.T) {
	client := &Client{conn: nil}
	ctx := context.Background()

	tests := []struct {
		name       string
		localPath  string
		remotePath string
	}{
		{"traversal in remote", "/local/file", "../../../etc/passwd"},
		{"null byte in remote", "/local/file", "/path/\x00/file"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := client.Upload(ctx, tt.localPath, tt.remotePath, TransferOptions{})
			if err == nil {
				t.Errorf("Upload() with invalid remote path expected error, got nil")
			}
		})
	}
}

func TestDownload_PathValidation(t *testing.T) {
	client := &Client{conn: nil}
	ctx := context.Background()

	tests := []struct {
		name       string
		remotePath string
		localPath  string
	}{
		{"traversal in remote", "../../../etc/passwd", "/local/file"},
		{"null byte in remote", "/path/\x00/file", "/local/file"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := client.Download(ctx, tt.remotePath, tt.localPath, TransferOptions{})
			if err == nil {
				t.Errorf("Download() with invalid remote path expected error, got nil")
			}
		})
	}
}

func TestUploadTree_PathValidation(t *testing.T) {
	client := &Client{conn: nil}
	ctx := context.Background()

	tests := []struct {
		name      string
		localDir  string
		remoteDir string
	}{
		{"traversal in remote", "/local/dir", "../../../etc"},
		{"null byte in remote", "/local/dir", "/path/\x00/dir"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := client.UploadTree(ctx, tt.localDir, tt.remoteDir, TransferOptions{})
			if err == nil {
				t.Errorf("UploadTree() with invalid remote path expected error, got nil")
			}
		})
	}
}

func TestDownloadTree_PathValidation(t *testing.T) {
	client := &Client{conn: nil}
	ctx := context.Background()

	tests := []struct {
		name      string
		remoteDir string
		localDir  string
	}{
		{"traversal in remote", "../../../etc", "/local/dir"},
		{"null byte in remote", "/path/\x00/dir", "/local/dir"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := client.DownloadTree(ctx, tt.remoteDir, tt.localDir, TransferOptions{})
			if err == nil {
				t.Errorf("DownloadTree() with invalid remote path expected error, got nil")
			}
		})
	}
}

func TestRelayTransfer_PathValidation(t *testing.T) {
	src := &Client{conn: nil}
	dst := &Client{conn: nil}
	ctx := context.Background()

	tests := []struct {
		name    string
		srcPath string
		dstPath string
	}{
		{"traversal in source", "../../../etc/passwd", "/dst/file"},
		{"traversal in dest", "/src/file", "../../../etc/passwd"},
		{"null byte in source", "/path/\x00/file", "/dst/file"},
		{"null byte in dest", "/src/file", "/path/\x00/file"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := RelayTransfer(ctx, src, dst, tt.srcPath, tt.dstPath, TransferOptions{})
			if err == nil {
				t.Errorf("RelayTransfer() with invalid path expected error, got nil")
			}
		})
	}
}

func TestErrFileExists(t *testing.T) {
	if ErrFileExists == nil {
		t.Error("ErrFileExists is nil")
	}
	if ErrFileExists.Error() == "" {
		t.Error("ErrFileExists.Error() is empty")
	}
}

func TestErrFileExistsMismatch(t *testing.T) {
	if ErrFileExistsMismatch == nil {
		t.Error("ErrFileExistsMismatch is nil")
	}
	if ErrFileExistsMismatch.Error() == "" {
		t.Error("ErrFileExistsMismatch.Error() is empty")
	}
}

func TestTransferOptions_Defaults(t *testing.T) {
	opts := TransferOptions{}

	// Verify defaults
	if opts.PreservePermissions {
		t.Error("Default PreservePermissions should be false")
	}
	if opts.Force {
		t.Error("Default Force should be false")
	}
	if opts.FileExistsMode != FileExistsModeAbort {
		t.Errorf("Default FileExistsMode = %v, want FileExistsModeAbort", opts.FileExistsMode)
	}
	if opts.OnProgress != nil {
		t.Error("Default OnProgress should be nil")
	}
}

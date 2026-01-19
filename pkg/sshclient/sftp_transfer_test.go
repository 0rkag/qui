// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sshclient

import (
	"bytes"
	"io"
	"testing"
)

func TestSFTPTransferOptions_effectiveFileExistsMode(t *testing.T) {
	tests := []struct {
		name     string
		opts     SFTPTransferOptions
		expected FileExistsMode
	}{
		{
			name:     "Force=true overrides mode to Overwrite",
			opts:     SFTPTransferOptions{Force: true, FileExistsMode: FileExistsModeAbort},
			expected: FileExistsModeOverwrite,
		},
		{
			name:     "Force=true overrides Skip to Overwrite",
			opts:     SFTPTransferOptions{Force: true, FileExistsMode: FileExistsModeSkip},
			expected: FileExistsModeOverwrite,
		},
		{
			name:     "Force=false uses mode - Abort",
			opts:     SFTPTransferOptions{Force: false, FileExistsMode: FileExistsModeAbort},
			expected: FileExistsModeAbort,
		},
		{
			name:     "Force=false uses mode - Skip",
			opts:     SFTPTransferOptions{Force: false, FileExistsMode: FileExistsModeSkip},
			expected: FileExistsModeSkip,
		},
		{
			name:     "Force=false uses mode - Overwrite",
			opts:     SFTPTransferOptions{Force: false, FileExistsMode: FileExistsModeOverwrite},
			expected: FileExistsModeOverwrite,
		},
		{
			name:     "default is Abort",
			opts:     SFTPTransferOptions{},
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
	// Verify constants are distinct and in expected order
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

	// Abort should be 0 (default zero value)
	if FileExistsModeAbort != 0 {
		t.Errorf("FileExistsModeAbort = %v, want 0 (default)", FileExistsModeAbort)
	}
}

func TestProgressReader_Tracking(t *testing.T) {
	data := []byte("Hello, World! This is test data for SFTP progress tracking.")
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

	// Verify progress was tracked correctly
	if lastTransferred != int64(len(data)) {
		t.Errorf("lastTransferred = %d, want %d", lastTransferred, len(data))
	}
	if lastTotal != int64(len(data)) {
		t.Errorf("lastTotal = %d, want %d", lastTotal, len(data))
	}

	// Verify callback was called multiple times for chunked reading
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

	// Verify transferred is still tracked internally
	if pr.transferred != int64(len(data)) {
		t.Errorf("pr.transferred = %d, want %d", pr.transferred, len(data))
	}
}

func TestProgressWriter_Callback(t *testing.T) {
	var buf bytes.Buffer
	data := []byte("Hello, World! This is test data for SFTP progress tracking.")

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

	// Verify transferred is still tracked internally
	if pw.transferred != int64(len(data)) {
		t.Errorf("pw.transferred = %d, want %d", pw.transferred, len(data))
	}
}

func TestSFTPTransferOptions_Defaults(t *testing.T) {
	opts := SFTPTransferOptions{}

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
	if opts.BufferSize != 0 {
		t.Errorf("Default BufferSize = %d, want 0 (to use default)", opts.BufferSize)
	}
	if opts.OnProgress != nil {
		t.Error("Default OnProgress should be nil")
	}
}

func TestDefaultBufferSize(t *testing.T) {
	if defaultBufferSize != 32*1024 {
		t.Errorf("defaultBufferSize = %d, want 32768", defaultBufferSize)
	}
}

func TestErrFileExists(t *testing.T) {
	if ErrFileExists == nil {
		t.Error("ErrFileExists is nil")
	}
	if ErrFileExists.Error() == "" {
		t.Error("ErrFileExists.Error() is empty")
	}
	if ErrFileExists.Error() != "file already exists" {
		t.Errorf("ErrFileExists.Error() = %q, want \"file already exists\"", ErrFileExists.Error())
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

func TestValidatePath_SFTP(t *testing.T) {
	// These tests verify path validation for SFTP operations
	// SFTP requires absolute paths for security
	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"valid absolute path", "/path/to/file.txt", false},
		{"relative path rejected", "path/to/file.txt", true}, // SSH requires absolute paths
		{"path traversal", "../../../etc/passwd", true},
		{"null byte injection", "/path/to/file\x00.txt", true},
		{"empty path", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePath(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePath(%q) error = %v, wantErr %v", tt.path, err, tt.wantErr)
			}
		})
	}
}

func TestProgressReader_AccumulatesCorrectly(t *testing.T) {
	data := make([]byte, 100)
	for i := range data {
		data[i] = byte(i)
	}
	reader := bytes.NewReader(data)

	progressHistory := make([]int64, 0)

	pr := &progressReader{
		reader: reader,
		total:  int64(len(data)),
		onProgress: func(transferred, total int64) {
			progressHistory = append(progressHistory, transferred)
		},
	}

	// Read in chunks of 10
	buf := make([]byte, 10)
	for {
		_, err := pr.Read(buf)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Read() error = %v", err)
		}
	}

	// Verify progress is monotonically increasing
	for i := 1; i < len(progressHistory); i++ {
		if progressHistory[i] < progressHistory[i-1] {
			t.Errorf("Progress decreased: %d -> %d", progressHistory[i-1], progressHistory[i])
		}
	}

	// Verify final progress equals total
	if len(progressHistory) > 0 {
		final := progressHistory[len(progressHistory)-1]
		if final != int64(len(data)) {
			t.Errorf("Final progress = %d, want %d", final, len(data))
		}
	}
}

func TestProgressWriter_AccumulatesCorrectly(t *testing.T) {
	var buf bytes.Buffer
	data := make([]byte, 100)
	for i := range data {
		data[i] = byte(i)
	}

	progressHistory := make([]int64, 0)

	pw := &progressWriter{
		writer: &buf,
		total:  int64(len(data)),
		onProgress: func(transferred, total int64) {
			progressHistory = append(progressHistory, transferred)
		},
	}

	// Write in chunks of 10
	for i := 0; i < len(data); i += 10 {
		end := i + 10
		if end > len(data) {
			end = len(data)
		}
		_, err := pw.Write(data[i:end])
		if err != nil {
			t.Fatalf("Write() error = %v", err)
		}
	}

	// Verify progress is monotonically increasing
	for i := 1; i < len(progressHistory); i++ {
		if progressHistory[i] < progressHistory[i-1] {
			t.Errorf("Progress decreased: %d -> %d", progressHistory[i-1], progressHistory[i])
		}
	}

	// Verify final progress equals total
	if len(progressHistory) > 0 {
		final := progressHistory[len(progressHistory)-1]
		if final != int64(len(data)) {
			t.Errorf("Final progress = %d, want %d", final, len(data))
		}
	}
}

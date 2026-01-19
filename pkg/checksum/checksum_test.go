// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package checksum

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Known test vectors for verification
const (
	// Empty file hashes
	emptyMD5    = "d41d8cd98f00b204e9800998ecf8427e"
	emptySHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

	// "Hello, World!" hashes
	helloWorldMD5    = "65a8e27d8879283831b664bd8b7f0ad4"
	helloWorldSHA256 = "dffd6021bb2bd5b0af676290809ec3a53191dd81c7f70a4b28688a362182986f"
)

func TestReaderChecksum_MD5(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:  "empty input",
			input: "",
			want:  emptyMD5,
		},
		{
			name:  "hello world",
			input: "Hello, World!",
			want:  helloWorldMD5,
		},
		{
			name:  "simple text",
			input: "test",
			want:  "098f6bcd4621d373cade4e832627b4f6",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := strings.NewReader(tt.input)
			got, err := ReaderChecksum(reader, AlgorithmMD5)
			if (err != nil) != tt.wantErr {
				t.Errorf("ReaderChecksum() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ReaderChecksum() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestReaderChecksum_SHA256(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:  "empty input",
			input: "",
			want:  emptySHA256,
		},
		{
			name:  "hello world",
			input: "Hello, World!",
			want:  helloWorldSHA256,
		},
		{
			name:  "simple text",
			input: "test",
			want:  "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := strings.NewReader(tt.input)
			got, err := ReaderChecksum(reader, AlgorithmSHA256)
			if (err != nil) != tt.wantErr {
				t.Errorf("ReaderChecksum() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ReaderChecksum() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestReaderChecksum_EmptyReader(t *testing.T) {
	reader := bytes.NewReader([]byte{})

	md5Sum, err := ReaderChecksum(reader, AlgorithmMD5)
	if err != nil {
		t.Fatalf("ReaderChecksum(MD5) error = %v", err)
	}
	if md5Sum != emptyMD5 {
		t.Errorf("ReaderChecksum(MD5) = %v, want %v", md5Sum, emptyMD5)
	}

	// Reset reader for SHA256
	reader = bytes.NewReader([]byte{})
	sha256Sum, err := ReaderChecksum(reader, AlgorithmSHA256)
	if err != nil {
		t.Fatalf("ReaderChecksum(SHA256) error = %v", err)
	}
	if sha256Sum != emptySHA256 {
		t.Errorf("ReaderChecksum(SHA256) = %v, want %v", sha256Sum, emptySHA256)
	}
}

func TestReaderChecksum_Unsupported(t *testing.T) {
	reader := strings.NewReader("test")
	_, err := ReaderChecksum(reader, Algorithm("sha512"))
	if err != ErrUnsupportedAlgorithm {
		t.Errorf("ReaderChecksum() error = %v, want ErrUnsupportedAlgorithm", err)
	}
}

func TestFileChecksum_ValidFile(t *testing.T) {
	// Create temp file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")

	content := []byte("Hello, World!")
	if err := os.WriteFile(testFile, content, 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	tests := []struct {
		name string
		alg  Algorithm
		want string
	}{
		{
			name: "md5",
			alg:  AlgorithmMD5,
			want: helloWorldMD5,
		},
		{
			name: "sha256",
			alg:  AlgorithmSHA256,
			want: helloWorldSHA256,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FileChecksum(testFile, tt.alg)
			if err != nil {
				t.Fatalf("FileChecksum() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("FileChecksum() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFileChecksum_NotFound(t *testing.T) {
	_, err := FileChecksum("/nonexistent/path/file.txt", AlgorithmMD5)
	if err == nil {
		t.Error("FileChecksum() expected error for non-existent file, got nil")
	}
}

func TestFileChecksum_LargeFile(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "large.bin")

	// Create a 2MB file with predictable content
	size := 2 * 1024 * 1024
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i % 256)
	}

	if err := os.WriteFile(testFile, data, 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// Calculate expected checksum from data
	expectedSum, err := ReaderChecksum(bytes.NewReader(data), AlgorithmSHA256)
	if err != nil {
		t.Fatalf("failed to calculate expected checksum: %v", err)
	}

	// Verify FileChecksum matches
	got, err := FileChecksum(testFile, AlgorithmSHA256)
	if err != nil {
		t.Fatalf("FileChecksum() error = %v", err)
	}
	if got != expectedSum {
		t.Errorf("FileChecksum() = %v, want %v", got, expectedSum)
	}
}

func TestCompareFiles_Identical(t *testing.T) {
	tmpDir := t.TempDir()

	content := []byte("identical content")
	file1 := filepath.Join(tmpDir, "file1.txt")
	file2 := filepath.Join(tmpDir, "file2.txt")

	if err := os.WriteFile(file1, content, 0644); err != nil {
		t.Fatalf("failed to write file1: %v", err)
	}
	if err := os.WriteFile(file2, content, 0644); err != nil {
		t.Fatalf("failed to write file2: %v", err)
	}

	for _, alg := range []Algorithm{AlgorithmMD5, AlgorithmSHA256} {
		t.Run(string(alg), func(t *testing.T) {
			equal, err := CompareFiles(file1, file2, alg)
			if err != nil {
				t.Fatalf("CompareFiles() error = %v", err)
			}
			if !equal {
				t.Error("CompareFiles() = false, want true for identical files")
			}
		})
	}
}

func TestCompareFiles_Different(t *testing.T) {
	tmpDir := t.TempDir()

	file1 := filepath.Join(tmpDir, "file1.txt")
	file2 := filepath.Join(tmpDir, "file2.txt")

	if err := os.WriteFile(file1, []byte("content one"), 0644); err != nil {
		t.Fatalf("failed to write file1: %v", err)
	}
	if err := os.WriteFile(file2, []byte("content two"), 0644); err != nil {
		t.Fatalf("failed to write file2: %v", err)
	}

	for _, alg := range []Algorithm{AlgorithmMD5, AlgorithmSHA256} {
		t.Run(string(alg), func(t *testing.T) {
			equal, err := CompareFiles(file1, file2, alg)
			if err != nil {
				t.Fatalf("CompareFiles() error = %v", err)
			}
			if equal {
				t.Error("CompareFiles() = true, want false for different files")
			}
		})
	}
}

func TestCompareFiles_MissingFile(t *testing.T) {
	tmpDir := t.TempDir()
	file1 := filepath.Join(tmpDir, "exists.txt")

	if err := os.WriteFile(file1, []byte("content"), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	tests := []struct {
		name  string
		path1 string
		path2 string
	}{
		{
			name:  "first file missing",
			path1: "/nonexistent",
			path2: file1,
		},
		{
			name:  "second file missing",
			path1: file1,
			path2: "/nonexistent",
		},
		{
			name:  "both files missing",
			path1: "/nonexistent1",
			path2: "/nonexistent2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := CompareFiles(tt.path1, tt.path2, AlgorithmSHA256)
			if err == nil {
				t.Error("CompareFiles() expected error for missing file, got nil")
			}
		})
	}
}

func TestFileSizeMatch_Same(t *testing.T) {
	tmpDir := t.TempDir()

	content := []byte("same size content")
	file1 := filepath.Join(tmpDir, "file1.txt")
	file2 := filepath.Join(tmpDir, "file2.txt")

	// Same content = same size
	if err := os.WriteFile(file1, content, 0644); err != nil {
		t.Fatalf("failed to write file1: %v", err)
	}
	if err := os.WriteFile(file2, content, 0644); err != nil {
		t.Fatalf("failed to write file2: %v", err)
	}

	match, err := FileSizeMatch(file1, file2)
	if err != nil {
		t.Fatalf("FileSizeMatch() error = %v", err)
	}
	if !match {
		t.Error("FileSizeMatch() = false, want true for same size files")
	}
}

func TestFileSizeMatch_Different(t *testing.T) {
	tmpDir := t.TempDir()

	file1 := filepath.Join(tmpDir, "file1.txt")
	file2 := filepath.Join(tmpDir, "file2.txt")

	if err := os.WriteFile(file1, []byte("short"), 0644); err != nil {
		t.Fatalf("failed to write file1: %v", err)
	}
	if err := os.WriteFile(file2, []byte("much longer content here"), 0644); err != nil {
		t.Fatalf("failed to write file2: %v", err)
	}

	match, err := FileSizeMatch(file1, file2)
	if err != nil {
		t.Fatalf("FileSizeMatch() error = %v", err)
	}
	if match {
		t.Error("FileSizeMatch() = true, want false for different size files")
	}
}

func TestFileSizeMatch_MissingFile(t *testing.T) {
	tmpDir := t.TempDir()
	file1 := filepath.Join(tmpDir, "exists.txt")

	if err := os.WriteFile(file1, []byte("content"), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	_, err := FileSizeMatch(file1, "/nonexistent")
	if err == nil {
		t.Error("FileSizeMatch() expected error for missing file, got nil")
	}

	_, err = FileSizeMatch("/nonexistent", file1)
	if err == nil {
		t.Error("FileSizeMatch() expected error for missing file, got nil")
	}
}

func TestNewHash(t *testing.T) {
	tests := []struct {
		name    string
		alg     Algorithm
		wantErr bool
	}{
		{name: "md5", alg: AlgorithmMD5, wantErr: false},
		{name: "sha256", alg: AlgorithmSHA256, wantErr: false},
		{name: "unsupported", alg: Algorithm("sha512"), wantErr: true},
		{name: "empty", alg: Algorithm(""), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, err := newHash(tt.alg)
			if (err != nil) != tt.wantErr {
				t.Errorf("newHash() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && h == nil {
				t.Error("newHash() returned nil hash for valid algorithm")
			}
		})
	}
}

func TestAlgorithmConstants(t *testing.T) {
	// Ensure algorithm constants are defined correctly
	if AlgorithmMD5 != "md5" {
		t.Errorf("AlgorithmMD5 = %q, want \"md5\"", AlgorithmMD5)
	}
	if AlgorithmSHA256 != "sha256" {
		t.Errorf("AlgorithmSHA256 = %q, want \"sha256\"", AlgorithmSHA256)
	}
}

// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build ftp_integration

package ftpclient

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/jlaffaye/ftp"
)

// These tests require a running FTP server.
// Run with: go test -tags=ftp_integration -v ./pkg/ftpclient/...
//
// Environment variables:
//   FTP_TEST_HOST - FTP server hostname (default: localhost)
//   FTP_TEST_PORT - FTP server port (default: 21)
//   FTP_TEST_USER - FTP username (default: testuser)
//   FTP_TEST_PASS - FTP password (default: testpass)
//   FTP_TEST_TLS  - TLS mode: none, explicit, implicit (default: none)
//
// Docker quick start:
//   docker run -d --name ftp-test \
//     -p 21:21 -p 21100-21110:21100-21110 \
//     -e FTP_USER=testuser -e FTP_PASS=testpass \
//     fauria/vsftpd

func getTestConfig(t *testing.T) *Config {
	t.Helper()

	host := os.Getenv("FTP_TEST_HOST")
	if host == "" {
		host = "localhost"
	}

	port := 21
	if p := os.Getenv("FTP_TEST_PORT"); p != "" {
		parsed, err := strconv.Atoi(p)
		if err != nil {
			t.Fatalf("Invalid FTP_TEST_PORT %q: %v", p, err)
		}
		port = parsed
	}

	user := os.Getenv("FTP_TEST_USER")
	if user == "" {
		user = "testuser"
	}

	pass := os.Getenv("FTP_TEST_PASS")
	if pass == "" {
		pass = "testpass"
	}

	tlsMode := os.Getenv("FTP_TEST_TLS")
	if tlsMode == "" {
		tlsMode = TLSModeNone
	}

	return &Config{
		Host:          host,
		Port:          port,
		Username:      user,
		Password:      pass,
		TLSMode:       tlsMode,
		SkipTLSVerify: true, // For testing with self-signed certs
		Timeout:       30 * time.Second,
	}
}

func TestIntegration_Connect(t *testing.T) {
	cfg := getTestConfig(t)

	client, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer client.Close()

	if !client.IsConnected() {
		t.Error("IsConnected() = false after successful New()")
	}
}

func TestIntegration_UploadDownload(t *testing.T) {
	cfg := getTestConfig(t)

	client, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	// Create temp local file
	tmpDir := t.TempDir()
	localFile := filepath.Join(tmpDir, "test.txt")
	content := []byte("Hello, FTP World! " + time.Now().String())
	if err := os.WriteFile(localFile, content, 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// Remote path - use unique name to avoid conflicts
	remotePath := "/test_upload_" + time.Now().Format("20060102_150405") + ".txt"

	// Upload
	err = client.Upload(ctx, localFile, remotePath, TransferOptions{
		FileExistsMode: FileExistsModeOverwrite,
	})
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}

	// Verify file exists
	exists, err := client.Exists(remotePath)
	if err != nil {
		t.Fatalf("Exists() error = %v", err)
	}
	if !exists {
		t.Error("Uploaded file does not exist on server")
	}

	// Download to different local file
	downloadFile := filepath.Join(tmpDir, "downloaded.txt")
	err = client.Download(ctx, remotePath, downloadFile, TransferOptions{})
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}

	// Verify content matches
	downloaded, err := os.ReadFile(downloadFile)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !bytes.Equal(downloaded, content) {
		t.Errorf("Downloaded content = %q, want %q", downloaded, content)
	}

	// Cleanup - use t.Cleanup to ensure it runs even if test fails
	t.Cleanup(func() {
		if err := client.Remove(remotePath); err != nil {
			// Log but don't fail - file might already be removed or test might have failed before creating it
			t.Logf("Cleanup: Remove(%s) error: %v", remotePath, err)
		}
	})
}

func TestIntegration_DirectoryOperations(t *testing.T) {
	cfg := getTestConfig(t)

	client, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer client.Close()

	// Create nested directory
	testDir := "/test_dir_" + time.Now().Format("20060102_150405")
	nestedDir := testDir + "/nested/deep"

	err = client.MkdirAll(nestedDir)
	if err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	// Verify directory exists
	isDir, err := client.IsDir(nestedDir)
	if err != nil {
		t.Fatalf("IsDir() error = %v", err)
	}
	if !isDir {
		t.Error("Created directory not detected as directory")
	}

	// List directory
	entries, err := client.List(testDir)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(entries) == 0 {
		t.Error("List() returned no entries for non-empty directory")
	}

	// Cleanup - use t.Cleanup to ensure it runs even if test fails
	t.Cleanup(func() {
		if err := client.RemoveAll(testDir); err != nil {
			t.Logf("Cleanup: RemoveAll(%s) error: %v", testDir, err)
		}
	})
}

func TestIntegration_FileExistsAbort(t *testing.T) {
	cfg := getTestConfig(t)

	client, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	// Create local file
	tmpDir := t.TempDir()
	localFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(localFile, []byte("test"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	remotePath := "/test_abort_" + time.Now().Format("20060102_150405") + ".txt"

	// First upload should succeed
	err = client.Upload(ctx, localFile, remotePath, TransferOptions{
		FileExistsMode: FileExistsModeAbort,
	})
	if err != nil {
		t.Fatalf("First Upload() error = %v", err)
	}

	// Second upload should fail with ErrFileExists
	err = client.Upload(ctx, localFile, remotePath, TransferOptions{
		FileExistsMode: FileExistsModeAbort,
	})
	if err != ErrFileExists {
		t.Errorf("Second Upload() error = %v, want ErrFileExists", err)
	}

	// Cleanup - use t.Cleanup to ensure it runs even if test fails
	t.Cleanup(func() {
		if err := client.Remove(remotePath); err != nil {
			t.Logf("Cleanup: Remove(%s) error: %v", remotePath, err)
		}
	})
}

func TestIntegration_FileExistsSkip(t *testing.T) {
	cfg := getTestConfig(t)

	client, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	// Create local file
	tmpDir := t.TempDir()
	localFile := filepath.Join(tmpDir, "test.txt")
	content := []byte("test content")
	if err := os.WriteFile(localFile, content, 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	remotePath := "/test_skip_" + time.Now().Format("20060102_150405") + ".txt"

	// First upload
	err = client.Upload(ctx, localFile, remotePath, TransferOptions{
		FileExistsMode: FileExistsModeOverwrite,
	})
	if err != nil {
		t.Fatalf("First Upload() error = %v", err)
	}

	// Second upload with same content should skip (no error)
	err = client.Upload(ctx, localFile, remotePath, TransferOptions{
		FileExistsMode: FileExistsModeSkip,
	})
	if err != nil {
		t.Errorf("Second Upload() with same content error = %v, want nil (skip)", err)
	}

	// Upload with different content should fail
	differentFile := filepath.Join(tmpDir, "different.txt")
	if err := os.WriteFile(differentFile, []byte("different content that is longer"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	err = client.Upload(ctx, differentFile, remotePath, TransferOptions{
		FileExistsMode: FileExistsModeSkip,
	})
	if err != ErrFileExistsMismatch {
		t.Errorf("Upload() with different content error = %v, want ErrFileExistsMismatch", err)
	}

	// Cleanup - use t.Cleanup to ensure it runs even if test fails
	t.Cleanup(func() {
		if err := client.Remove(remotePath); err != nil {
			t.Logf("Cleanup: Remove(%s) error: %v", remotePath, err)
		}
	})
}

func TestIntegration_FileExistsOverwrite(t *testing.T) {
	cfg := getTestConfig(t)

	client, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	tmpDir := t.TempDir()
	localFile := filepath.Join(tmpDir, "test.txt")

	remotePath := "/test_overwrite_" + time.Now().Format("20060102_150405") + ".txt"

	// First upload
	if err := os.WriteFile(localFile, []byte("original"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	err = client.Upload(ctx, localFile, remotePath, TransferOptions{
		FileExistsMode: FileExistsModeOverwrite,
	})
	if err != nil {
		t.Fatalf("First Upload() error = %v", err)
	}

	// Second upload should overwrite
	newContent := []byte("new content")
	if err := os.WriteFile(localFile, newContent, 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	err = client.Upload(ctx, localFile, remotePath, TransferOptions{
		FileExistsMode: FileExistsModeOverwrite,
	})
	if err != nil {
		t.Errorf("Second Upload() with overwrite error = %v, want nil", err)
	}

	// Verify new content
	downloadFile := filepath.Join(tmpDir, "downloaded.txt")
	err = client.Download(ctx, remotePath, downloadFile, TransferOptions{
		FileExistsMode: FileExistsModeOverwrite,
	})
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}

	downloaded, err := os.ReadFile(downloadFile)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !bytes.Equal(downloaded, newContent) {
		t.Errorf("Downloaded content = %q, want %q", downloaded, newContent)
	}

	// Cleanup - use t.Cleanup to ensure it runs even if test fails
	t.Cleanup(func() {
		if err := client.Remove(remotePath); err != nil {
			t.Logf("Cleanup: Remove(%s) error: %v", remotePath, err)
		}
	})
}

func TestIntegration_LargeFile(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping large file test in short mode")
	}

	cfg := getTestConfig(t)

	client, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	// Create 10MB file
	tmpDir := t.TempDir()
	localFile := filepath.Join(tmpDir, "large.bin")

	size := 10 * 1024 * 1024 // 10MB
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i % 256)
	}

	if err := os.WriteFile(localFile, data, 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	remotePath := "/test_large_" + time.Now().Format("20060102_150405") + ".bin"

	// Track progress
	var lastProgress int64
	progressCalled := false

	// Upload with progress tracking
	err = client.Upload(ctx, localFile, remotePath, TransferOptions{
		FileExistsMode: FileExistsModeOverwrite,
		OnProgress: func(transferred, total int64) {
			progressCalled = true
			lastProgress = transferred
		},
	})
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}

	if !progressCalled {
		t.Error("Progress callback was not called")
	}
	if lastProgress != int64(size) {
		t.Errorf("Final progress = %d, want %d", lastProgress, size)
	}

	// Verify file size
	remoteSize, err := client.FileSize(remotePath)
	if err != nil {
		t.Fatalf("FileSize() error = %v", err)
	}
	if remoteSize != int64(size) {
		t.Errorf("Remote file size = %d, want %d", remoteSize, size)
	}

	// Download and verify
	downloadFile := filepath.Join(tmpDir, "downloaded.bin")
	err = client.Download(ctx, remotePath, downloadFile, TransferOptions{
		FileExistsMode: FileExistsModeOverwrite,
	})
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}

	downloaded, err := os.ReadFile(downloadFile)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !bytes.Equal(downloaded, data) {
		t.Error("Downloaded content does not match original")
	}

	// Cleanup - use t.Cleanup to ensure it runs even if test fails
	t.Cleanup(func() {
		if err := client.Remove(remotePath); err != nil {
			t.Logf("Cleanup: Remove(%s) error: %v", remotePath, err)
		}
	})
}

func TestIntegration_Capabilities(t *testing.T) {
	cfg := getTestConfig(t)

	client, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer client.Close()

	caps, err := client.CheckCapabilities()
	if err != nil {
		t.Fatalf("CheckCapabilities() error = %v", err)
	}

	t.Logf("Capabilities: TLS=%v, PassiveMode=%v, FXP=%v",
		caps.TLSEnabled, caps.PassiveModeWorks, caps.FXPSupported)

	// TLS should match config
	expectedTLS := cfg.TLSMode != TLSModeNone
	if caps.TLSEnabled != expectedTLS {
		t.Errorf("TLSEnabled = %v, want %v", caps.TLSEnabled, expectedTLS)
	}
}

func TestIntegration_Walk(t *testing.T) {
	cfg := getTestConfig(t)

	client, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	// Create test directory structure
	testDir := "/test_walk_" + time.Now().Format("20060102_150405")
	if err := client.MkdirAll(testDir + "/subdir"); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	// Create test files
	tmpDir := t.TempDir()
	localFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(localFile, []byte("test"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if err := client.Upload(ctx, localFile, testDir+"/file1.txt", TransferOptions{FileExistsMode: FileExistsModeOverwrite}); err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	if err := client.Upload(ctx, localFile, testDir+"/subdir/file2.txt", TransferOptions{FileExistsMode: FileExistsModeOverwrite}); err != nil {
		t.Fatalf("Upload() error = %v", err)
	}

	// Walk and collect entries
	var paths []string
	err = client.Walk(testDir, func(path string, entry *ftp.Entry, err error) error {
		if err != nil {
			return err
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		t.Fatalf("Walk() error = %v", err)
	}

	// Should have found at least the subdir and two files
	if len(paths) < 3 {
		t.Errorf("Walk() found %d paths, want at least 3", len(paths))
	}

	// Cleanup - use t.Cleanup to ensure it runs even if test fails
	t.Cleanup(func() {
		if err := client.RemoveAll(testDir); err != nil {
			t.Logf("Cleanup: RemoveAll(%s) error: %v", testDir, err)
		}
	})
}

func TestIntegration_FileSize(t *testing.T) {
	cfg := getTestConfig(t)

	client, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	// Create test file with known size
	tmpDir := t.TempDir()
	localFile := filepath.Join(tmpDir, "sized.txt")
	content := []byte("this is exactly 29 bytes!!!")
	expectedSize := int64(len(content))

	if err := os.WriteFile(localFile, content, 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	remotePath := "/test_size_" + time.Now().Format("20060102_150405") + ".txt"

	// Upload
	err = client.Upload(ctx, localFile, remotePath, TransferOptions{
		FileExistsMode: FileExistsModeOverwrite,
	})
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}

	t.Cleanup(func() {
		if err := client.Remove(remotePath); err != nil {
			t.Logf("Cleanup: Remove(%s) error: %v", remotePath, err)
		}
	})

	// Get file size
	size, err := client.FileSize(remotePath)
	if err != nil {
		t.Fatalf("FileSize() error = %v", err)
	}

	if size != expectedSize {
		t.Errorf("FileSize() = %d, want %d", size, expectedSize)
	}
}

func TestIntegration_Rename(t *testing.T) {
	cfg := getTestConfig(t)

	client, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	// Create test file
	tmpDir := t.TempDir()
	localFile := filepath.Join(tmpDir, "rename_test.txt")
	if err := os.WriteFile(localFile, []byte("rename me"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	timestamp := time.Now().Format("20060102_150405")
	oldPath := "/test_rename_old_" + timestamp + ".txt"
	newPath := "/test_rename_new_" + timestamp + ".txt"

	// Upload with old name
	err = client.Upload(ctx, localFile, oldPath, TransferOptions{
		FileExistsMode: FileExistsModeOverwrite,
	})
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}

	t.Cleanup(func() {
		// Clean up both possible paths
		client.Remove(oldPath)
		client.Remove(newPath)
	})

	// Rename
	err = client.Rename(oldPath, newPath)
	if err != nil {
		t.Fatalf("Rename() error = %v", err)
	}

	// Verify old path no longer exists
	oldExists, err := client.Exists(oldPath)
	if err != nil {
		t.Fatalf("Exists(old) error = %v", err)
	}
	if oldExists {
		t.Error("Old path should not exist after rename")
	}

	// Verify new path exists
	newExists, err := client.Exists(newPath)
	if err != nil {
		t.Fatalf("Exists(new) error = %v", err)
	}
	if !newExists {
		t.Error("New path should exist after rename")
	}
}

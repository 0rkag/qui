// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build ssh_integration

package sshclient

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// SSH Integration Tests
//
// These tests require a real SSH server to connect to.
// Run with: go test -tags=ssh_integration ./pkg/sshclient/...
//
// Configure the SSH server via environment variables:
//   SSH_TEST_HOST     - SSH server hostname (default: localhost)
//   SSH_TEST_PORT     - SSH server port (default: 22)
//   SSH_TEST_USER     - SSH username (default: current user)
//   SSH_TEST_KEY      - Path to SSH private key (default: ~/.ssh/id_rsa)
//   SSH_TEST_WORKDIR  - Remote working directory for tests (default: /tmp/sshclient_test)
//
// Example with Docker:
//   docker run -d -p 2222:22 --name ssh-test linuxserver/openssh-server
//   SSH_TEST_HOST=localhost SSH_TEST_PORT=2222 SSH_TEST_USER=... go test -tags=ssh_integration ./pkg/sshclient/...

func getTestConfig(t *testing.T) *Config {
	t.Helper()

	host := os.Getenv("SSH_TEST_HOST")
	if host == "" {
		host = "localhost"
	}

	port := 22
	if p := os.Getenv("SSH_TEST_PORT"); p != "" {
		var err error
		_, err = time.ParseDuration(p + "s") // Hacky way to parse int
		if err != nil {
			t.Fatalf("invalid SSH_TEST_PORT: %s", p)
		}
	}

	user := os.Getenv("SSH_TEST_USER")
	if user == "" {
		user = os.Getenv("USER")
	}

	keyPath := os.Getenv("SSH_TEST_KEY")
	if keyPath == "" {
		home, _ := os.UserHomeDir()
		keyPath = filepath.Join(home, ".ssh", "id_rsa")
	}

	// Check key exists
	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		t.Skipf("SSH key not found at %s, skipping integration test", keyPath)
	}

	return &Config{
		Host:           host,
		Port:           port,
		Username:       user,
		PrivateKeyPath: keyPath,
		Timeout:        10 * time.Second,
	}
}

func getTestWorkdir() string {
	dir := os.Getenv("SSH_TEST_WORKDIR")
	if dir == "" {
		dir = "/tmp/sshclient_test"
	}
	return dir
}

func TestIntegration_Connect(t *testing.T) {
	cfg := getTestConfig(t)

	client, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer client.Close()

	if !client.IsAlive() {
		t.Error("client should be alive after connect")
	}
}

func TestIntegration_Exec(t *testing.T) {
	cfg := getTestConfig(t)

	client, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	// Test simple command
	result, err := client.Exec(ctx, "echo hello")
	if err != nil {
		t.Fatalf("Exec failed: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.ExitCode)
	}
	if result.Stdout != "hello\n" {
		t.Errorf("expected stdout 'hello\\n', got %q", result.Stdout)
	}

	// Test command with non-zero exit
	result, err = client.Exec(ctx, "exit 42")
	if err != nil {
		t.Fatalf("Exec failed: %v", err)
	}
	if result.ExitCode != 42 {
		t.Errorf("expected exit code 42, got %d", result.ExitCode)
	}
}

func TestIntegration_ExecSimple(t *testing.T) {
	cfg := getTestConfig(t)

	client, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	// Success case
	stdout, err := client.ExecSimple(ctx, "echo success")
	if err != nil {
		t.Fatalf("ExecSimple failed: %v", err)
	}
	if stdout != "success\n" {
		t.Errorf("expected 'success\\n', got %q", stdout)
	}

	// Failure case
	_, err = client.ExecSimple(ctx, "exit 1")
	if err == nil {
		t.Error("expected error for non-zero exit")
	}
}

func TestIntegration_ExecWithContext(t *testing.T) {
	cfg := getTestConfig(t)

	client, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer client.Close()

	// Test context cancellation
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err = client.Exec(ctx, "sleep 10")
	if err == nil {
		t.Error("expected error due to context timeout")
	}
	if err != context.DeadlineExceeded {
		t.Errorf("expected DeadlineExceeded, got %v", err)
	}
}

func TestIntegration_MkdirAll(t *testing.T) {
	cfg := getTestConfig(t)

	client, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	workdir := getTestWorkdir()

	// Clean up first
	_ = client.Remove(ctx, workdir)

	// Create nested directory
	testDir := filepath.Join(workdir, "a", "b", "c")
	err = client.MkdirAll(ctx, testDir)
	if err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}

	// Verify it exists
	isDir, err := client.IsDir(ctx, testDir)
	if err != nil {
		t.Fatalf("IsDir failed: %v", err)
	}
	if !isDir {
		t.Error("expected directory to exist")
	}

	// Cleanup
	_ = client.Remove(ctx, workdir)
}

func TestIntegration_Exists(t *testing.T) {
	cfg := getTestConfig(t)

	client, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	// /tmp should exist
	exists, err := client.Exists(ctx, "/tmp")
	if err != nil {
		t.Fatalf("Exists failed: %v", err)
	}
	if !exists {
		t.Error("/tmp should exist")
	}

	// Random path should not exist
	exists, err = client.Exists(ctx, "/nonexistent_path_12345")
	if err != nil {
		t.Fatalf("Exists failed: %v", err)
	}
	if exists {
		t.Error("nonexistent path should not exist")
	}
}

func TestIntegration_IsDir(t *testing.T) {
	cfg := getTestConfig(t)

	client, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	// /tmp is a directory
	isDir, err := client.IsDir(ctx, "/tmp")
	if err != nil {
		t.Fatalf("IsDir failed: %v", err)
	}
	if !isDir {
		t.Error("/tmp should be a directory")
	}

	// /etc/passwd is not a directory (if it exists)
	isDir, err = client.IsDir(ctx, "/etc/passwd")
	if err != nil {
		t.Fatalf("IsDir failed: %v", err)
	}
	if isDir {
		t.Error("/etc/passwd should not be a directory")
	}
}

func TestIntegration_Hardlink(t *testing.T) {
	cfg := getTestConfig(t)

	client, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	workdir := getTestWorkdir()

	// Setup
	_ = client.Remove(ctx, workdir)
	err = client.MkdirAll(ctx, workdir)
	if err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}

	// Create a test file
	srcFile := filepath.Join(workdir, "source.txt")
	_, err = client.ExecSimple(ctx, "echo 'test content' > "+ShellQuote(srcFile))
	if err != nil {
		t.Fatalf("failed to create source file: %v", err)
	}

	// Create hardlink
	dstFile := filepath.Join(workdir, "hardlink.txt")
	err = client.Hardlink(ctx, srcFile, dstFile)
	if err != nil {
		t.Fatalf("Hardlink failed: %v", err)
	}

	// Verify link exists
	exists, err := client.Exists(ctx, dstFile)
	if err != nil {
		t.Fatalf("Exists failed: %v", err)
	}
	if !exists {
		t.Error("hardlink should exist")
	}

	// Verify same inode (hardlink)
	stdout, err := client.ExecSimple(ctx, "stat -c %i "+ShellQuote(srcFile)+" "+ShellQuote(dstFile))
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}
	// Both files should have same inode
	lines := filepath.SplitList(stdout)
	if len(lines) >= 2 && lines[0] != lines[1] {
		t.Error("files should have same inode (be hardlinked)")
	}

	// Cleanup
	_ = client.Remove(ctx, workdir)
}

func TestIntegration_Copy(t *testing.T) {
	cfg := getTestConfig(t)

	client, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	workdir := getTestWorkdir()

	// Setup
	_ = client.Remove(ctx, workdir)
	err = client.MkdirAll(ctx, workdir)
	if err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}

	// Create a test file
	srcFile := filepath.Join(workdir, "source.txt")
	_, err = client.ExecSimple(ctx, "echo 'copy test' > "+ShellQuote(srcFile))
	if err != nil {
		t.Fatalf("failed to create source file: %v", err)
	}

	// Copy file
	dstFile := filepath.Join(workdir, "copied.txt")
	err = client.Copy(ctx, srcFile, dstFile)
	if err != nil {
		t.Fatalf("Copy failed: %v", err)
	}

	// Verify copy exists
	exists, err := client.Exists(ctx, dstFile)
	if err != nil {
		t.Fatalf("Exists failed: %v", err)
	}
	if !exists {
		t.Error("copied file should exist")
	}

	// Cleanup
	_ = client.Remove(ctx, workdir)
}

func TestIntegration_Remove(t *testing.T) {
	cfg := getTestConfig(t)

	client, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	workdir := getTestWorkdir()

	// Create directory with files
	testDir := filepath.Join(workdir, "to_remove")
	err = client.MkdirAll(ctx, testDir)
	if err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}

	_, err = client.ExecSimple(ctx, "touch "+ShellQuote(filepath.Join(testDir, "file.txt")))
	if err != nil {
		t.Fatalf("touch failed: %v", err)
	}

	// Remove
	err = client.Remove(ctx, testDir)
	if err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	// Verify gone
	exists, err := client.Exists(ctx, testDir)
	if err != nil {
		t.Fatalf("Exists failed: %v", err)
	}
	if exists {
		t.Error("directory should be removed")
	}
}

func TestIntegration_IsAlive(t *testing.T) {
	cfg := getTestConfig(t)

	client, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	if !client.IsAlive() {
		t.Error("client should be alive")
	}

	// Close and check again
	client.Close()

	if client.IsAlive() {
		t.Error("client should not be alive after close")
	}
}

func TestIntegration_Pool(t *testing.T) {
	cfg := getTestConfig(t)

	pool := NewPool(time.Minute)
	defer pool.Close()

	// Get client
	client1, err := pool.Get(cfg)
	if err != nil {
		t.Fatalf("pool.Get failed: %v", err)
	}

	// Should be in pool
	stats := pool.Stats()
	if stats.ActiveConnections != 1 {
		t.Errorf("expected 1 active connection, got %d", stats.ActiveConnections)
	}

	// Get again should return same client
	client2, err := pool.Get(cfg)
	if err != nil {
		t.Fatalf("pool.Get failed: %v", err)
	}
	if client1 != client2 {
		t.Error("expected same client instance from pool")
	}

	// Verify it works
	ctx := context.Background()
	_, err = client2.ExecSimple(ctx, "echo test")
	if err != nil {
		t.Errorf("exec on pooled client failed: %v", err)
	}
}

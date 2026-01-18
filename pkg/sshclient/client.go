// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package sshclient provides SSH connection and command execution utilities.
package sshclient

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"golang.org/x/crypto/ssh"
)

// Config holds SSH connection configuration.
type Config struct {
	Host           string
	Port           int
	Username       string
	Password       string // Password authentication (used if PrivateKeyPath is empty)
	PrivateKeyPath string // Private key authentication (takes precedence over password)
	Timeout        time.Duration

	// Host key verification (TOFU - Trust On First Use)
	ExpectedHostKey         *HostKeyInfo                             // If set, verify the host key matches
	OnNewHostKey            func(info *HostKeyInfo) error            // Callback when a new host key is encountered
	SkipHostKeyVerification bool                                     // If true, skip all host key verification (insecure)
}

// HostKeyInfo contains information about an SSH host key.
type HostKeyInfo struct {
	Fingerprint string // SHA256 fingerprint (e.g., "SHA256:...")
	Algorithm   string // Key algorithm (e.g., "ssh-ed25519", "ssh-rsa")
}

// HostKeyError is returned when the host key does not match the expected fingerprint.
type HostKeyError struct {
	Expected string // Expected fingerprint
	Actual   string // Actual fingerprint
	Hostname string // Hostname being connected to
}

// Error implements the error interface.
func (e *HostKeyError) Error() string {
	return fmt.Sprintf("host key mismatch for %s: expected %s, got %s", e.Hostname, e.Expected, e.Actual)
}

// Client wraps an SSH connection with command execution capabilities.
type Client struct {
	config *Config
	client *ssh.Client
}

// New creates a new SSH client and establishes a connection.
// Authentication priority: private key (if provided) > password.
// Returns the client, host key info (for TOFU), and any error.
func New(cfg *Config) (*Client, *HostKeyInfo, error) {
	if cfg == nil {
		return nil, nil, fmt.Errorf("config cannot be nil")
	}
	if cfg.Port == 0 {
		cfg.Port = 22
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}

	// Build authentication methods
	var authMethods []ssh.AuthMethod

	// Private key takes precedence if provided
	if cfg.PrivateKeyPath != "" {
		signer, err := loadPrivateKey(cfg.PrivateKeyPath)
		if err != nil {
			return nil, nil, fmt.Errorf("load private key: %w", err)
		}
		authMethods = append(authMethods, ssh.PublicKeys(signer))
	}

	// Password authentication as fallback or primary
	if cfg.Password != "" {
		authMethods = append(authMethods, ssh.Password(cfg.Password))
	}

	if len(authMethods) == 0 {
		return nil, nil, fmt.Errorf("no authentication method provided (need password or private key)")
	}

	// Build host key callback with TOFU support
	var capturedHostKey *HostKeyInfo
	hostKeyCallback := buildHostKeyCallback(cfg, &capturedHostKey)

	sshConfig := &ssh.ClientConfig{
		User:            cfg.Username,
		Auth:            authMethods,
		HostKeyCallback: hostKeyCallback,
		Timeout:         cfg.Timeout,
	}

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	client, err := ssh.Dial("tcp", addr, sshConfig)
	if err != nil {
		// Don't include address in error to avoid leaking sensitive info to end users
		// The underlying error may still contain connection details for logging
		return nil, capturedHostKey, fmt.Errorf("ssh connection failed: %w", err)
	}

	return &Client{
		config: cfg,
		client: client,
	}, capturedHostKey, nil
}

// NewInsecure creates a new SSH client without host key verification.
// This is a convenience wrapper that sets SkipHostKeyVerification=true.
// Use this only when backward compatibility is needed or for testing.
func NewInsecure(cfg *Config) (*Client, error) {
	cfg.SkipHostKeyVerification = true
	client, _, err := New(cfg)
	return client, err
}

// buildHostKeyCallback creates a host key callback based on the config.
func buildHostKeyCallback(cfg *Config, capturedKey **HostKeyInfo) ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		// Capture the host key info for TOFU
		fingerprint := ssh.FingerprintSHA256(key)
		algorithm := key.Type()
		keyInfo := &HostKeyInfo{
			Fingerprint: fingerprint,
			Algorithm:   algorithm,
		}
		*capturedKey = keyInfo

		// If skipping verification, accept any key
		if cfg.SkipHostKeyVerification {
			return nil
		}

		// If we have an expected key, verify it matches
		if cfg.ExpectedHostKey != nil {
			if cfg.ExpectedHostKey.Fingerprint != fingerprint {
				return &HostKeyError{
					Expected: cfg.ExpectedHostKey.Fingerprint,
					Actual:   fingerprint,
					Hostname: hostname,
				}
			}
			return nil // Key matches
		}

		// No expected key - this is a new key (TOFU scenario)
		// Call the callback if provided to let the caller decide
		if cfg.OnNewHostKey != nil {
			return cfg.OnNewHostKey(keyInfo)
		}

		// Default: accept new keys (backward compatible behavior)
		return nil
	}
}

// Close closes the SSH connection.
func (c *Client) Close() error {
	if c.client != nil {
		return c.client.Close()
	}
	return nil
}

// ExecResult holds the result of a command execution.
type ExecResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// Exec executes a command on the remote host.
//
// On context cancellation, the function attempts graceful cleanup:
// 1. Signals SIGKILL to the remote process
// 2. Closes the SSH session to unblock the goroutine
// 3. Waits up to the client's configured timeout for the goroutine to exit
//
// If the goroutine doesn't exit within the timeout, it will be orphaned.
// This is a limitation of the Go SSH library - there's no way to forcefully
// interrupt a blocked session.Run() call.
func (c *Client) Exec(ctx context.Context, cmd string) (*ExecResult, error) {
	log.Info().Str("cmd", cmd).Msg("exec command")
	session, err := c.client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	// Run command in goroutine to allow context cancellation
	done := make(chan error, 1)
	go func() {
		done <- session.Run(cmd)
	}()

	select {
	case <-ctx.Done():
		// Attempt graceful cleanup
		c.cleanupSession(session, done)
		return nil, ctx.Err()

	case err := <-done:
		session.Close()
		result := &ExecResult{
			Stdout:   stdout.String(),
			Stderr:   stderr.String(),
			ExitCode: 0,
		}
		if err != nil {
			if exitErr, ok := err.(*ssh.ExitError); ok {
				result.ExitCode = exitErr.ExitStatus()
			} else {
				return nil, fmt.Errorf("exec command: %w", err)
			}
		}
		return result, nil
	}
}

// cleanupSession attempts to gracefully terminate a session and wait for the
// goroutine to exit. Uses the client's configured timeout for the wait.
func (c *Client) cleanupSession(session *ssh.Session, done <-chan error) {
	// Signal the remote process to terminate
	_ = session.Signal(ssh.SIGKILL)

	// Close the session - this should unblock session.Run()
	_ = session.Close()

	// Use client's timeout for cleanup wait, with a minimum of 5 seconds
	cleanupTimeout := c.config.Timeout
	if cleanupTimeout < 5*time.Second {
		cleanupTimeout = 5 * time.Second
	}

	// Wait for the goroutine to acknowledge the close
	select {
	case <-done:
		// Goroutine exited cleanly
	case <-time.After(cleanupTimeout):
		// Goroutine didn't exit - this is a known limitation of the SSH library.
		// The goroutine will eventually exit when the underlying TCP connection
		// times out or is closed by the OS.
	}
}

// ExecSimple executes a command and returns stdout, failing on non-zero exit.
func (c *Client) ExecSimple(ctx context.Context, cmd string) (string, error) {
	result, err := c.Exec(ctx, cmd)
	if err != nil {
		return "", err
	}
	if result.ExitCode != 0 {
		return "", fmt.Errorf("command failed (exit %d): %s", result.ExitCode, strings.TrimSpace(result.Stderr))
	}
	return result.Stdout, nil
}

// MkdirAll creates a directory and all parent directories on the remote host.
// If the directory already exists, this is a no-op.
func (c *Client) MkdirAll(ctx context.Context, path string) error {
	if err := ValidatePath(path); err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}

	// Check if directory already exists
	exists, err := c.IsDir(ctx, path)
	if err == nil && exists {
		return nil // Directory exists, nothing to do
	}

	// Try to create the directory
	result, err := c.Exec(ctx, fmt.Sprintf("mkdir -p %s", ShellQuote(path)))
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("failed to create directory %s: %s (check that the parent directory exists and the SSH user has write permission)",
			path, strings.TrimSpace(result.Stderr))
	}
	return nil
}

// Exists checks if a path exists on the remote host.
func (c *Client) Exists(ctx context.Context, path string) (bool, error) {
	if err := ValidatePath(path); err != nil {
		return false, fmt.Errorf("invalid path: %w", err)
	}
	result, err := c.Exec(ctx, fmt.Sprintf("test -e %s", ShellQuote(path)))
	if err != nil {
		return false, err
	}
	return result.ExitCode == 0, nil
}

// IsDir checks if a path is a directory on the remote host.
func (c *Client) IsDir(ctx context.Context, path string) (bool, error) {
	if err := ValidatePath(path); err != nil {
		return false, fmt.Errorf("invalid path: %w", err)
	}
	result, err := c.Exec(ctx, fmt.Sprintf("test -d %s", ShellQuote(path)))
	if err != nil {
		return false, err
	}
	return result.ExitCode == 0, nil
}

// Hardlink creates a hardlink from src to dst on the remote host.
func (c *Client) Hardlink(ctx context.Context, src, dst string) error {
	if err := ValidatePath(src); err != nil {
		return fmt.Errorf("invalid source path: %w", err)
	}
	if err := ValidatePath(dst); err != nil {
		return fmt.Errorf("invalid destination path: %w", err)
	}
	_, err := c.ExecSimple(ctx, fmt.Sprintf("ln %s %s", ShellQuote(src), ShellQuote(dst)))
	return err
}

// Reflink creates a reflink (copy-on-write) from src to dst on the remote host.
// If force is false and dst already exists, returns ErrFileExists.
func (c *Client) Reflink(ctx context.Context, src, dst string, force bool) error {
	if err := ValidatePath(src); err != nil {
		return fmt.Errorf("invalid source path: %w", err)
	}
	if err := ValidatePath(dst); err != nil {
		return fmt.Errorf("invalid destination path: %w", err)
	}

	// Check if destination exists and force is not set
	if !force {
		exists, err := c.Exists(ctx, dst)
		if err != nil {
			return fmt.Errorf("check destination exists: %w", err)
		}
		if exists {
			return ErrFileExists
		}
	}

	// Try cp --reflink=always first (Linux), fall back to cp -c (macOS)
	result, err := c.Exec(ctx, fmt.Sprintf("cp --reflink=always %s %s 2>/dev/null || cp -c %s %s", ShellQuote(src), ShellQuote(dst), ShellQuote(src), ShellQuote(dst)))
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("reflink failed: %s", strings.TrimSpace(result.Stderr))
	}
	return nil
}

// Copy copies a file from src to dst on the remote host.
// If force is false and dst already exists, returns ErrFileExists.
func (c *Client) Copy(ctx context.Context, src, dst string, force bool) error {
	if err := ValidatePath(src); err != nil {
		return fmt.Errorf("invalid source path: %w", err)
	}
	if err := ValidatePath(dst); err != nil {
		return fmt.Errorf("invalid destination path: %w", err)
	}

	// Check if destination exists and force is not set
	if !force {
		exists, err := c.Exists(ctx, dst)
		if err != nil {
			return fmt.Errorf("check destination exists: %w", err)
		}
		if exists {
			return ErrFileExists
		}
	}

	_, err := c.ExecSimple(ctx, fmt.Sprintf("cp %s %s", ShellQuote(src), ShellQuote(dst)))
	return err
}

// Remove removes a file or directory on the remote host.
// WARNING: This is a destructive operation. The path is validated to be absolute
// and free of traversal elements, but callers should ensure the path is within
// expected directories.
func (c *Client) Remove(ctx context.Context, path string) error {
	if err := ValidatePath(path); err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}
	_, err := c.ExecSimple(ctx, fmt.Sprintf("rm -rf %s", ShellQuote(path)))
	return err
}

// Config returns the client's configuration.
func (c *Client) Config() *Config {
	return c.config
}

// IsAlive checks if the SSH connection is still alive by sending a keepalive request.
func (c *Client) IsAlive() bool {
	if c.client == nil {
		return false
	}
	// SendRequest with keepalive@openssh.com is a common way to check connection health
	// The wantReply=true ensures we wait for a response
	_, _, err := c.client.SendRequest("keepalive@openssh.com", true, nil)
	return err == nil
}

// loadPrivateKey loads and parses a private key file.
func loadPrivateKey(path string) (ssh.Signer, error) {
	key, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read key file: %w", err)
	}

	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("parse key: %w", err)
	}

	return signer, nil
}

// ShellQuote quotes a string for safe use in shell commands.
// Uses single quotes and escapes embedded single quotes.
func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

// ShellSession represents an interactive shell session with PTY.
type ShellSession struct {
	Session *ssh.Session
	Stdin   io.WriteCloser
	Stdout  io.Reader
	Stderr  io.Reader
}

// Shell creates an interactive shell session with a pseudo-terminal.
// The caller is responsible for closing the session when done.
func (c *Client) Shell(cols, rows uint32) (*ShellSession, error) {
	session, err := c.client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}

	// Request pseudo-terminal
	modes := ssh.TerminalModes{
		ssh.ECHO:          1,     // Enable echo
		ssh.TTY_OP_ISPEED: 14400, // Input speed
		ssh.TTY_OP_OSPEED: 14400, // Output speed
	}

	if err := session.RequestPty("xterm-256color", int(rows), int(cols), modes); err != nil {
		session.Close()
		return nil, fmt.Errorf("request pty: %w", err)
	}

	stdin, err := session.StdinPipe()
	if err != nil {
		session.Close()
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}

	stdout, err := session.StdoutPipe()
	if err != nil {
		session.Close()
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	stderr, err := session.StderrPipe()
	if err != nil {
		session.Close()
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}

	if err := session.Shell(); err != nil {
		session.Close()
		return nil, fmt.Errorf("start shell: %w", err)
	}

	return &ShellSession{
		Session: session,
		Stdin:   stdin,
		Stdout:  stdout,
		Stderr:  stderr,
	}, nil
}

// Resize changes the terminal size for an active shell session.
func (s *ShellSession) Resize(cols, rows uint32) error {
	return s.Session.WindowChange(int(rows), int(cols))
}

// Close closes the shell session.
func (s *ShellSession) Close() error {
	return s.Session.Close()
}

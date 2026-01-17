// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package sshclient provides SSH connection and command execution utilities.
package sshclient

import (
	"bytes"
	"context"
	"fmt"
	"io"
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
	PrivateKeyPath string
	Timeout        time.Duration
}

// Client wraps an SSH connection with command execution capabilities.
type Client struct {
	config *Config
	client *ssh.Client
}

// New creates a new SSH client and establishes a connection.
func New(cfg *Config) (*Client, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}
	if cfg.Port == 0 {
		cfg.Port = 22
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}

	signer, err := loadPrivateKey(cfg.PrivateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("load private key: %w", err)
	}

	sshConfig := &ssh.ClientConfig{
		User: cfg.Username,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // TODO: Add proper host key verification
		Timeout:         cfg.Timeout,
	}

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	client, err := ssh.Dial("tcp", addr, sshConfig)
	if err != nil {
		// Don't include address in error to avoid leaking sensitive info to end users
		// The underlying error may still contain connection details for logging
		return nil, fmt.Errorf("ssh connection failed: %w", err)
	}

	return &Client{
		config: cfg,
		client: client,
	}, nil
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
func (c *Client) MkdirAll(ctx context.Context, path string) error {
	if err := ValidatePath(path); err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}
	_, err := c.ExecSimple(ctx, fmt.Sprintf("mkdir -p %s", shellQuote(path)))
	return err
}

// Exists checks if a path exists on the remote host.
func (c *Client) Exists(ctx context.Context, path string) (bool, error) {
	if err := ValidatePath(path); err != nil {
		return false, fmt.Errorf("invalid path: %w", err)
	}
	result, err := c.Exec(ctx, fmt.Sprintf("test -e %s", shellQuote(path)))
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
	result, err := c.Exec(ctx, fmt.Sprintf("test -d %s", shellQuote(path)))
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
	_, err := c.ExecSimple(ctx, fmt.Sprintf("ln %s %s", shellQuote(src), shellQuote(dst)))
	return err
}

// Reflink creates a reflink (copy-on-write) from src to dst on the remote host.
func (c *Client) Reflink(ctx context.Context, src, dst string) error {
	if err := ValidatePath(src); err != nil {
		return fmt.Errorf("invalid source path: %w", err)
	}
	if err := ValidatePath(dst); err != nil {
		return fmt.Errorf("invalid destination path: %w", err)
	}
	// Try cp --reflink=always first (Linux), fall back to cp -c (macOS)
	result, err := c.Exec(ctx, fmt.Sprintf("cp --reflink=always %s %s 2>/dev/null || cp -c %s %s", shellQuote(src), shellQuote(dst), shellQuote(src), shellQuote(dst)))
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("reflink failed: %s", strings.TrimSpace(result.Stderr))
	}
	return nil
}

// Copy copies a file from src to dst on the remote host.
func (c *Client) Copy(ctx context.Context, src, dst string) error {
	if err := ValidatePath(src); err != nil {
		return fmt.Errorf("invalid source path: %w", err)
	}
	if err := ValidatePath(dst); err != nil {
		return fmt.Errorf("invalid destination path: %w", err)
	}
	_, err := c.ExecSimple(ctx, fmt.Sprintf("cp %s %s", shellQuote(src), shellQuote(dst)))
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
	_, err := c.ExecSimple(ctx, fmt.Sprintf("rm -rf %s", shellQuote(path)))
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
// Exported for use by transfer.go.
func ShellQuote(s string) string {
	// Use single quotes and escape any single quotes in the string
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

// shellQuote is an internal alias for backwards compatibility.
func shellQuote(s string) string {
	return ShellQuote(s)
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

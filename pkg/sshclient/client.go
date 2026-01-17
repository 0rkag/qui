// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package sshclient provides SSH connection and command execution utilities.
package sshclient

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

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
		return nil, fmt.Errorf("ssh dial %s: %w", addr, err)
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
func (c *Client) Exec(ctx context.Context, cmd string) (*ExecResult, error) {
	session, err := c.client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	defer session.Close()

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	// Handle context cancellation
	done := make(chan error, 1)
	go func() {
		done <- session.Run(cmd)
	}()

	select {
	case <-ctx.Done():
		_ = session.Signal(ssh.SIGKILL)
		return nil, ctx.Err()
	case err := <-done:
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
	_, err := c.ExecSimple(ctx, fmt.Sprintf("mkdir -p %s", shellQuote(path)))
	return err
}

// Exists checks if a path exists on the remote host.
func (c *Client) Exists(ctx context.Context, path string) (bool, error) {
	result, err := c.Exec(ctx, fmt.Sprintf("test -e %s", shellQuote(path)))
	if err != nil {
		return false, err
	}
	return result.ExitCode == 0, nil
}

// IsDir checks if a path is a directory on the remote host.
func (c *Client) IsDir(ctx context.Context, path string) (bool, error) {
	result, err := c.Exec(ctx, fmt.Sprintf("test -d %s", shellQuote(path)))
	if err != nil {
		return false, err
	}
	return result.ExitCode == 0, nil
}

// Hardlink creates a hardlink from src to dst on the remote host.
func (c *Client) Hardlink(ctx context.Context, src, dst string) error {
	_, err := c.ExecSimple(ctx, fmt.Sprintf("ln %s %s", shellQuote(src), shellQuote(dst)))
	return err
}

// Reflink creates a reflink (copy-on-write) from src to dst on the remote host.
func (c *Client) Reflink(ctx context.Context, src, dst string) error {
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
	_, err := c.ExecSimple(ctx, fmt.Sprintf("cp %s %s", shellQuote(src), shellQuote(dst)))
	return err
}

// Remove removes a file or directory on the remote host.
func (c *Client) Remove(ctx context.Context, path string) error {
	_, err := c.ExecSimple(ctx, fmt.Sprintf("rm -rf %s", shellQuote(path)))
	return err
}

// Config returns the client's configuration.
func (c *Client) Config() *Config {
	return c.config
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

// shellQuote quotes a string for safe use in shell commands.
func shellQuote(s string) string {
	// Use single quotes and escape any single quotes in the string
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

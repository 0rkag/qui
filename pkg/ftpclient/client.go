// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package ftpclient provides FTP/FTPS connection and file transfer utilities.
package ftpclient

import (
	"crypto/tls"
	"fmt"
	"time"

	"github.com/jlaffaye/ftp"
)

// TLS mode for FTP connections
const (
	TLSModeNone     = "none"     // Plain FTP (no encryption)
	TLSModeExplicit = "explicit" // FTP with AUTH TLS (explicit TLS on port 21)
	TLSModeImplicit = "implicit" // FTPS (implicit TLS from start on port 990)
)

// Config holds FTP connection configuration.
type Config struct {
	Host     string
	Port     int
	Username string
	Password string

	// TLS mode: "none", "explicit", or "implicit"
	TLSMode string // Default: "explicit"

	// TLS certificate verification
	SkipTLSVerify bool // If true, skip TLS certificate verification (insecure, use for self-signed certs)

	// Connection settings
	Timeout time.Duration // Connection timeout (default 30s)
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Port:    21,
		Timeout: 30 * time.Second,
		TLSMode: TLSModeExplicit, // Default to explicit TLS
	}
}

// Client wraps an FTP connection with file operation capabilities.
type Client struct {
	config *Config
	conn   *ftp.ServerConn
}

// New creates a new FTP client and establishes a connection.
func New(cfg *Config) (*Client, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}
	if cfg.Host == "" {
		return nil, fmt.Errorf("host is required")
	}
	if cfg.Port == 0 {
		// Set default port based on TLS mode
		switch cfg.TLSMode {
		case TLSModeImplicit:
			cfg.Port = 990
		default:
			cfg.Port = 21
		}
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.TLSMode == "" {
		cfg.TLSMode = TLSModeExplicit // Default to explicit TLS
	}

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)

	// Build dial options
	var opts []ftp.DialOption
	opts = append(opts, ftp.DialWithTimeout(cfg.Timeout))

	// TLS configuration - use SkipTLSVerify setting from config
	tlsConfig := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: cfg.SkipTLSVerify,
		ServerName:         cfg.Host,
	}

	switch cfg.TLSMode {
	case TLSModeExplicit:
		opts = append(opts, ftp.DialWithExplicitTLS(tlsConfig))
	case TLSModeImplicit:
		opts = append(opts, ftp.DialWithTLS(tlsConfig))
	case TLSModeNone:
		// No TLS
	default:
		return nil, fmt.Errorf("invalid TLS mode: %s", cfg.TLSMode)
	}

	// Library uses passive mode by default for transfers

	// Connect
	conn, err := ftp.Dial(addr, opts...)
	if err != nil {
		return nil, fmt.Errorf("ftp dial failed: %w", err)
	}

	// Login
	if err := conn.Login(cfg.Username, cfg.Password); err != nil {
		conn.Quit()
		return nil, fmt.Errorf("ftp login failed: %w", err)
	}

	// Clear password from config after successful login
	cfg.Password = ""

	return &Client{
		config: cfg,
		conn:   conn,
	}, nil
}

// Close closes the FTP connection gracefully.
func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Quit()
	}
	return nil
}

// Config returns the client's configuration (with password masked).
func (c *Client) Config() *Config {
	return c.config
}

// IsConnected checks if the connection is still alive.
func (c *Client) IsConnected() bool {
	if c.conn == nil {
		return false
	}
	// Try a no-op command to test connection
	return c.conn.NoOp() == nil
}

// Capabilities holds detected FTP server capabilities.
type Capabilities struct {
	// TLSEnabled indicates if the connection is encrypted.
	TLSEnabled bool `json:"tlsEnabled"`

	// PassiveModeWorks indicates if passive mode transfers work.
	PassiveModeWorks bool `json:"passiveModeWorks"`

	// FXPSupported indicates if FXP (server-to-server) might work.
	// Note: This is a hint only; actual FXP may still fail.
	FXPSupported bool `json:"fxpSupported"`

	// ServerType contains the server software identification if available.
	ServerType string `json:"serverType,omitempty"`

	// Features lists supported FTP extensions (FEAT response).
	Features []string `json:"features,omitempty"`
}

// CheckCapabilities detects FTP server capabilities.
func (c *Client) CheckCapabilities() (*Capabilities, error) {
	caps := &Capabilities{
		TLSEnabled: c.config.TLSMode != TLSModeNone,
	}

	// Test passive mode with a simple LIST
	_, err := c.conn.List(".")
	caps.PassiveModeWorks = err == nil

	// FXP support is hard to detect without trying
	// Most servers that allow it don't advertise it
	// We'll mark it as potentially supported if PASV works
	caps.FXPSupported = caps.PassiveModeWorks

	return caps, nil
}

// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/autobrr/qui/internal/dbinterface"
)

// Validation patterns for connection configuration
var (
	// validConnUsername allows alphanumeric, underscore, hyphen, and dot
	validConnUsername = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9_.-]*$`)
	// validConnHostname allows alphanumeric, hyphen, dot, and brackets for IPv6
	validConnHostname = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.\-:\[\]]*$`)
)

// Connection protocol types
const (
	ProtocolSSH  = "ssh"
	ProtocolSFTP = "sftp"
	ProtocolFTP  = "ftp"
)

// Validation limits
const (
	MaxHostnameLength     = 253
	MaxUsernameLength     = 64
	MaxPrivateKeyPathLen  = 4096
	MaxPortNumber         = 65535
	MaxConnectionsPerList = 100 // Reasonable limit for connections per instance
)

var (
	ErrConnectionNotFound      = errors.New("connection not found")
	ErrDuplicateConnection     = errors.New("connection already exists for this instance and protocol")
	ErrUnsupportedProtocol     = errors.New("unsupported protocol")
	ErrInvalidConnectionConfig = errors.New("invalid connection configuration")
)

// InstanceConnection represents a connection configuration for remote access to an instance.
type InstanceConnection struct {
	ID             int64     `json:"id"`
	InstanceID     int       `json:"instanceId"`
	Protocol       string    `json:"protocol"`       // "ssh", "sftp", "ftp"
	Host           string    `json:"host"`
	Port           int       `json:"port"`
	Username       string    `json:"username"`
	PrivateKeyPath string    `json:"privateKeyPath,omitempty"` // For SSH: path to key on QUI server
	Enabled        bool      `json:"enabled"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

// Validate checks that the connection has valid data.
func (c *InstanceConnection) Validate() error {
	if c.InstanceID <= 0 {
		return errors.New("instance ID is required")
	}
	if strings.TrimSpace(c.Protocol) == "" {
		return errors.New("protocol is required")
	}
	if c.Protocol != ProtocolSSH && c.Protocol != ProtocolSFTP && c.Protocol != ProtocolFTP {
		return ErrUnsupportedProtocol
	}

	// Validate host format
	host := strings.TrimSpace(c.Host)
	if host == "" {
		return errors.New("host is required")
	}
	if len(host) > MaxHostnameLength {
		return errors.New("host too long (max 253 chars)")
	}
	if !validConnHostname.MatchString(host) {
		return errors.New("host contains invalid characters")
	}
	c.Host = host

	if c.Port <= 0 || c.Port > MaxPortNumber {
		return errors.New("port must be between 1 and 65535")
	}

	// Validate username format
	username := strings.TrimSpace(c.Username)
	if username == "" {
		return errors.New("username is required")
	}
	if len(username) > MaxUsernameLength {
		return errors.New("username too long (max 64 chars)")
	}
	if !validConnUsername.MatchString(username) {
		return errors.New("username contains invalid characters")
	}
	c.Username = username

	// Validate private key path if provided
	if c.PrivateKeyPath != "" {
		if err := validatePrivateKeyPath(c.PrivateKeyPath); err != nil {
			return err
		}
	}

	return nil
}

// validatePrivateKeyPath checks that a private key path is safe.
func validatePrivateKeyPath(path string) error {
	// Clean the path to resolve any . or ..
	cleaned := filepath.Clean(path)

	// Check for path traversal attempts
	if strings.Contains(cleaned, "..") {
		return errors.New("private key path contains path traversal")
	}

	// Must be an absolute path
	if !filepath.IsAbs(cleaned) {
		return errors.New("private key path must be absolute")
	}

	// Check path length
	if len(cleaned) > MaxPrivateKeyPathLen {
		return errors.New("private key path too long")
	}

	return nil
}

// DefaultPort returns the default port for a given protocol.
func DefaultPort(protocol string) int {
	switch protocol {
	case ProtocolSSH, ProtocolSFTP:
		return 22
	case ProtocolFTP:
		return 21
	default:
		return 22
	}
}

// InstanceConnectionStore handles database operations for instance connections.
type InstanceConnectionStore struct {
	db dbinterface.Querier
}

// NewInstanceConnectionStore creates a new InstanceConnectionStore.
func NewInstanceConnectionStore(db dbinterface.Querier) *InstanceConnectionStore {
	return &InstanceConnectionStore{db: db}
}

// Create inserts a new connection configuration.
func (s *InstanceConnectionStore) Create(ctx context.Context, c *InstanceConnection) (*InstanceConnection, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO instance_connections (
			instance_id, protocol, host, port, username, private_key_path, enabled, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.InstanceID, c.Protocol, c.Host, c.Port, c.Username, c.PrivateKeyPath, c.Enabled, now, now,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return nil, ErrDuplicateConnection
		}
		return nil, err
	}

	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}

	c.ID = id
	c.CreatedAt = now
	c.UpdatedAt = now
	return c, nil
}

// Get retrieves a connection by ID.
func (s *InstanceConnectionStore) Get(ctx context.Context, id int64) (*InstanceConnection, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, instance_id, protocol, host, port, username, private_key_path, enabled, created_at, updated_at
		FROM instance_connections
		WHERE id = ?`, id)

	return s.scanConnection(row)
}

// GetByInstanceAndProtocol retrieves a connection for a specific instance and protocol.
func (s *InstanceConnectionStore) GetByInstanceAndProtocol(ctx context.Context, instanceID int, protocol string) (*InstanceConnection, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, instance_id, protocol, host, port, username, private_key_path, enabled, created_at, updated_at
		FROM instance_connections
		WHERE instance_id = ? AND protocol = ?`, instanceID, protocol)

	return s.scanConnection(row)
}

// GetSSHByInstance retrieves the SSH connection for an instance (convenience method).
func (s *InstanceConnectionStore) GetSSHByInstance(ctx context.Context, instanceID int) (*InstanceConnection, error) {
	return s.GetByInstanceAndProtocol(ctx, instanceID, ProtocolSSH)
}

// Update modifies an existing connection.
func (s *InstanceConnectionStore) Update(ctx context.Context, c *InstanceConnection) error {
	if err := c.Validate(); err != nil {
		return err
	}

	c.UpdatedAt = time.Now().UTC()
	res, err := s.db.ExecContext(ctx, `
		UPDATE instance_connections
		SET host = ?, port = ?, username = ?, private_key_path = ?, enabled = ?, updated_at = ?
		WHERE id = ?`,
		c.Host, c.Port, c.Username, c.PrivateKeyPath, c.Enabled, c.UpdatedAt, c.ID,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return ErrDuplicateConnection
		}
		return err
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrConnectionNotFound
	}
	return nil
}

// Delete removes a connection by ID.
func (s *InstanceConnectionStore) Delete(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM instance_connections WHERE id = ?`, id)
	if err != nil {
		return err
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrConnectionNotFound
	}
	return nil
}

// ListByInstance retrieves all connections for an instance.
func (s *InstanceConnectionStore) ListByInstance(ctx context.Context, instanceID int) ([]*InstanceConnection, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, instance_id, protocol, host, port, username, private_key_path, enabled, created_at, updated_at
		FROM instance_connections
		WHERE instance_id = ?
		ORDER BY protocol
		LIMIT ?`, instanceID, MaxConnectionsPerList)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanConnections(rows)
}

// ListEnabledByInstance retrieves all enabled connections for an instance.
func (s *InstanceConnectionStore) ListEnabledByInstance(ctx context.Context, instanceID int) ([]*InstanceConnection, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, instance_id, protocol, host, port, username, private_key_path, enabled, created_at, updated_at
		FROM instance_connections
		WHERE instance_id = ? AND enabled = 1
		ORDER BY protocol
		LIMIT ?`, instanceID, MaxConnectionsPerList)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanConnections(rows)
}

// DeleteByInstance removes all connections for an instance.
func (s *InstanceConnectionStore) DeleteByInstance(ctx context.Context, instanceID int) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM instance_connections WHERE instance_id = ?`, instanceID)
	return err
}

// scanConnection scans a single row into an InstanceConnection.
func (s *InstanceConnectionStore) scanConnection(row *sql.Row) (*InstanceConnection, error) {
	var c InstanceConnection
	var privateKeyPath sql.NullString

	err := row.Scan(
		&c.ID, &c.InstanceID, &c.Protocol, &c.Host, &c.Port,
		&c.Username, &privateKeyPath, &c.Enabled, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrConnectionNotFound
		}
		return nil, err
	}

	if privateKeyPath.Valid {
		c.PrivateKeyPath = privateKeyPath.String
	}
	return &c, nil
}

// scanConnections scans multiple rows into InstanceConnection slice.
func (s *InstanceConnectionStore) scanConnections(rows *sql.Rows) ([]*InstanceConnection, error) {
	var connections []*InstanceConnection
	for rows.Next() {
		var c InstanceConnection
		var privateKeyPath sql.NullString

		err := rows.Scan(
			&c.ID, &c.InstanceID, &c.Protocol, &c.Host, &c.Port,
			&c.Username, &privateKeyPath, &c.Enabled, &c.CreatedAt, &c.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}

		if privateKeyPath.Valid {
			c.PrivateKeyPath = privateKeyPath.String
		}
		connections = append(connections, &c)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}
	return connections, nil
}

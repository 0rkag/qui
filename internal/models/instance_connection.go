// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"io"
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

// Connection type constants - these define both the protocol and transfer method
const (
	// SSH-based connection types
	ConnectionTypeSSHAuto   = "ssh_auto"   // SSH, auto-select best transfer (rsync > sftp > scp)
	ConnectionTypeSSHRsync  = "ssh_rsync"  // SSH, force rsync
	ConnectionTypeSSHSFTP   = "ssh_sftp"   // SSH, force SFTP
	ConnectionTypeSSHSCP    = "ssh_scp"    // SSH, force SCP

	// FTP-based connection types
	ConnectionTypeFTPExplicit = "ftp_explicit" // FTP with AUTH TLS (explicit TLS)
	ConnectionTypeFTPImplicit = "ftp_implicit" // FTPS (implicit TLS from start)
	ConnectionTypeFTPPlain    = "ftp_plain"    // Plain FTP (no encryption)
)

// IsSSHType returns true if the connection type is SSH-based.
func IsSSHType(connType string) bool {
	switch connType {
	case ConnectionTypeSSHAuto, ConnectionTypeSSHRsync, ConnectionTypeSSHSFTP, ConnectionTypeSSHSCP:
		return true
	}
	return false
}

// IsFTPType returns true if the connection type is FTP-based.
func IsFTPType(connType string) bool {
	switch connType {
	case ConnectionTypeFTPExplicit, ConnectionTypeFTPImplicit, ConnectionTypeFTPPlain:
		return true
	}
	return false
}

// ValidConnectionTypes returns all valid connection type values.
func ValidConnectionTypes() []string {
	return []string{
		ConnectionTypeSSHAuto,
		ConnectionTypeSSHRsync,
		ConnectionTypeSSHSFTP,
		ConnectionTypeSSHSCP,
		ConnectionTypeFTPExplicit,
		ConnectionTypeFTPImplicit,
		ConnectionTypeFTPPlain,
	}
}

// Validation limits
const (
	MaxHostnameLength     = 253
	MaxUsernameLength     = 64
	MaxPasswordLength     = 256
	MaxPrivateKeyPathLen  = 4096
	MaxPortNumber         = 65535
	MaxConnectionsPerList = 100 // Reasonable limit for connections per instance
)

var (
	ErrConnectionNotFound      = errors.New("connection not found")
	ErrDuplicateConnection     = errors.New("connection already exists for this instance and type")
	ErrUnsupportedType         = errors.New("unsupported connection type")
	ErrInvalidConnectionConfig = errors.New("invalid connection configuration")
	ErrPathMappingRequired     = errors.New("at least one path mapping is required before creating a connection")
)

// InstanceConnection represents a connection configuration for remote access to an instance.
type InstanceConnection struct {
	ID         int64     `json:"id"`
	InstanceID int       `json:"instanceId"`
	Type       string    `json:"type"` // ssh_auto, ssh_rsync, ssh_sftp, ssh_scp, ftp_explicit, ftp_implicit, ftp_plain
	Host       string    `json:"host"`
	Port       int       `json:"port"`
	Username   string    `json:"username"`
	Password   string    `json:"password,omitempty"`       // Encrypted in DB, only for input (never returned)
	PrivateKeyPath string `json:"privateKeyPath,omitempty"` // For SSH types: path to key on QUI server
	Enabled    bool      `json:"enabled"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`

	// Detected capabilities (cached from connection test)
	RsyncAvailable      *bool      `json:"rsyncAvailable,omitempty"`
	RsyncVersion        string     `json:"rsyncVersion,omitempty"`
	SFTPAvailable       *bool      `json:"sftpAvailable,omitempty"`
	HardlinksSupported  *bool      `json:"hardlinksSupported,omitempty"`
	ReflinksSupported   *bool      `json:"reflinksSupported,omitempty"`
	CapabilitiesChecked *time.Time `json:"capabilitiesCheckedAt,omitempty"`

	// Internal: encrypted password (not exported to JSON)
	passwordEncrypted string
}

// Validate checks that the connection has valid data.
func (c *InstanceConnection) Validate() error {
	if c.InstanceID <= 0 {
		return errors.New("instance ID is required")
	}

	// Validate type
	connType := strings.TrimSpace(c.Type)
	if connType == "" {
		return errors.New("type is required")
	}
	if !IsSSHType(connType) && !IsFTPType(connType) {
		return ErrUnsupportedType
	}
	c.Type = connType

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

	// Validate password length if provided
	if c.Password != "" && len(c.Password) > MaxPasswordLength {
		return errors.New("password too long (max 256 chars)")
	}

	// Validate private key path if provided (SSH types only)
	if c.PrivateKeyPath != "" {
		if !IsSSHType(c.Type) {
			return errors.New("private key path is only valid for SSH connection types")
		}
		if err := validatePrivateKeyPath(c.PrivateKeyPath); err != nil {
			return err
		}
	}

	// For SSH types: need either password or private key
	if IsSSHType(c.Type) && c.Password == "" && c.PrivateKeyPath == "" {
		return errors.New("SSH connections require either password or private key")
	}

	// For FTP types: need password
	if IsFTPType(c.Type) && c.Password == "" && c.passwordEncrypted == "" {
		return errors.New("FTP connections require a password")
	}

	return nil
}

// validatePrivateKeyPath checks that a private key path is safe.
func validatePrivateKeyPath(path string) error {
	// Check for path traversal attempts BEFORE cleaning (Clean removes ".." sequences)
	if strings.Contains(path, "..") {
		return errors.New("private key path contains path traversal")
	}

	// Clean the path to normalize it
	cleaned := filepath.Clean(path)

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

// DefaultPort returns the default port for a given connection type.
func DefaultPort(connType string) int {
	switch connType {
	case ConnectionTypeSSHAuto, ConnectionTypeSSHRsync, ConnectionTypeSSHSFTP, ConnectionTypeSSHSCP:
		return 22
	case ConnectionTypeFTPExplicit, ConnectionTypeFTPPlain:
		return 21
	case ConnectionTypeFTPImplicit:
		return 990
	default:
		return 22
	}
}

// InstanceConnectionStore handles database operations for instance connections.
type InstanceConnectionStore struct {
	db            dbinterface.Querier
	encryptionKey []byte
}

// NewInstanceConnectionStore creates a new InstanceConnectionStore.
func NewInstanceConnectionStore(db dbinterface.Querier, encryptionKey []byte) (*InstanceConnectionStore, error) {
	if len(encryptionKey) != 32 {
		return nil, errors.New("encryption key must be 32 bytes")
	}
	return &InstanceConnectionStore{db: db, encryptionKey: encryptionKey}, nil
}

// encrypt encrypts a string using AES-GCM.
func (s *InstanceConnectionStore) encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}

	block, err := aes.NewCipher(s.encryptionKey)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// decrypt decrypts an AES-GCM encrypted string.
func (s *InstanceConnectionStore) decrypt(ciphertext string) (string, error) {
	if ciphertext == "" {
		return "", nil
	}

	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(s.encryptionKey)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	if len(data) < gcm.NonceSize() {
		return "", errors.New("ciphertext too short")
	}

	nonce, ciphertextBytes := data[:gcm.NonceSize()], data[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertextBytes, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}

// GetDecryptedPassword returns the decrypted password for a connection.
func (s *InstanceConnectionStore) GetDecryptedPassword(c *InstanceConnection) (string, error) {
	return s.decrypt(c.passwordEncrypted)
}

// Create inserts a new connection configuration.
func (s *InstanceConnectionStore) Create(ctx context.Context, c *InstanceConnection) (*InstanceConnection, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}

	// Encrypt password if provided
	var encryptedPassword string
	if c.Password != "" {
		var err error
		encryptedPassword, err = s.encrypt(c.Password)
		if err != nil {
			return nil, err
		}
	}

	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO instance_connections (
			instance_id, type, host, port, username, password_encrypted, private_key_path, enabled,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.InstanceID, c.Type, c.Host, c.Port, c.Username, encryptedPassword, c.PrivateKeyPath, c.Enabled,
		now, now,
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
	c.Password = "" // Clear password from memory
	c.CreatedAt = now
	c.UpdatedAt = now
	return c, nil
}

// Get retrieves a connection by ID.
func (s *InstanceConnectionStore) Get(ctx context.Context, id int64) (*InstanceConnection, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, instance_id, type, host, port, username, password_encrypted, private_key_path, enabled,
			rsync_available, rsync_version, sftp_available, hardlinks_supported, reflinks_supported,
			capabilities_checked_at, created_at, updated_at
		FROM instance_connections
		WHERE id = ?`, id)

	return s.scanConnection(row)
}

// GetByInstanceAndType retrieves a connection for a specific instance and type.
func (s *InstanceConnectionStore) GetByInstanceAndType(ctx context.Context, instanceID int, connType string) (*InstanceConnection, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, instance_id, type, host, port, username, password_encrypted, private_key_path, enabled,
			rsync_available, rsync_version, sftp_available, hardlinks_supported, reflinks_supported,
			capabilities_checked_at, created_at, updated_at
		FROM instance_connections
		WHERE instance_id = ? AND type = ?`, instanceID, connType)

	return s.scanConnection(row)
}

// GetSSHByInstance retrieves any SSH-type connection for an instance (convenience method).
// Returns the first SSH connection found (ssh_auto, ssh_rsync, ssh_sftp, or ssh_scp).
func (s *InstanceConnectionStore) GetSSHByInstance(ctx context.Context, instanceID int) (*InstanceConnection, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, instance_id, type, host, port, username, password_encrypted, private_key_path, enabled,
			rsync_available, rsync_version, sftp_available, hardlinks_supported, reflinks_supported,
			capabilities_checked_at, created_at, updated_at
		FROM instance_connections
		WHERE instance_id = ? AND type IN (?, ?, ?, ?)
		LIMIT 1`,
		instanceID, ConnectionTypeSSHAuto, ConnectionTypeSSHRsync, ConnectionTypeSSHSFTP, ConnectionTypeSSHSCP)

	return s.scanConnection(row)
}

// GetFTPByInstance retrieves any FTP-type connection for an instance (convenience method).
func (s *InstanceConnectionStore) GetFTPByInstance(ctx context.Context, instanceID int) (*InstanceConnection, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, instance_id, type, host, port, username, password_encrypted, private_key_path, enabled,
			rsync_available, rsync_version, sftp_available, hardlinks_supported, reflinks_supported,
			capabilities_checked_at, created_at, updated_at
		FROM instance_connections
		WHERE instance_id = ? AND type IN (?, ?, ?)
		LIMIT 1`,
		instanceID, ConnectionTypeFTPExplicit, ConnectionTypeFTPImplicit, ConnectionTypeFTPPlain)

	return s.scanConnection(row)
}

// Update modifies an existing connection.
func (s *InstanceConnectionStore) Update(ctx context.Context, c *InstanceConnection) error {
	if err := c.Validate(); err != nil {
		return err
	}

	c.UpdatedAt = time.Now().UTC()

	// If password is provided, encrypt it; otherwise keep existing
	var query string
	var args []interface{}

	if c.Password != "" {
		encryptedPassword, err := s.encrypt(c.Password)
		if err != nil {
			return err
		}
		query = `
			UPDATE instance_connections
			SET host = ?, port = ?, username = ?, password_encrypted = ?, private_key_path = ?, enabled = ?, updated_at = ?
			WHERE id = ?`
		args = []interface{}{c.Host, c.Port, c.Username, encryptedPassword, c.PrivateKeyPath, c.Enabled, c.UpdatedAt, c.ID}
	} else {
		query = `
			UPDATE instance_connections
			SET host = ?, port = ?, username = ?, private_key_path = ?, enabled = ?, updated_at = ?
			WHERE id = ?`
		args = []interface{}{c.Host, c.Port, c.Username, c.PrivateKeyPath, c.Enabled, c.UpdatedAt, c.ID}
	}

	res, err := s.db.ExecContext(ctx, query, args...)
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

	c.Password = "" // Clear password from memory
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
		SELECT id, instance_id, type, host, port, username, password_encrypted, private_key_path, enabled,
			rsync_available, rsync_version, sftp_available, hardlinks_supported, reflinks_supported,
			capabilities_checked_at, created_at, updated_at
		FROM instance_connections
		WHERE instance_id = ?
		ORDER BY type
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
		SELECT id, instance_id, type, host, port, username, password_encrypted, private_key_path, enabled,
			rsync_available, rsync_version, sftp_available, hardlinks_supported, reflinks_supported,
			capabilities_checked_at, created_at, updated_at
		FROM instance_connections
		WHERE instance_id = ? AND enabled = 1
		ORDER BY type
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

// UpdateCapabilities updates the cached capabilities for a connection.
func (s *InstanceConnectionStore) UpdateCapabilities(ctx context.Context, id int64, caps *ConnectionCapabilities) error {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
		UPDATE instance_connections
		SET rsync_available = ?, rsync_version = ?, sftp_available = ?,
			hardlinks_supported = ?, reflinks_supported = ?,
			capabilities_checked_at = ?, updated_at = ?
		WHERE id = ?`,
		caps.RsyncAvailable, caps.RsyncVersion, caps.SFTPAvailable,
		caps.HardlinksSupported, caps.ReflinksSupported,
		now, now, id,
	)
	return err
}

// ConnectionCapabilities holds the detected capabilities for a connection.
type ConnectionCapabilities struct {
	RsyncAvailable     bool   `json:"rsyncAvailable"`
	RsyncVersion       string `json:"rsyncVersion,omitempty"`
	SFTPAvailable      bool   `json:"sftpAvailable"`
	HardlinksSupported bool   `json:"hardlinksSupported"`
	ReflinksSupported  bool   `json:"reflinksSupported"`
}

// connectionScanFields holds the nullable fields for scanning a connection row.
type connectionScanFields struct {
	passwordEncrypted   sql.NullString
	privateKeyPath      sql.NullString
	rsyncVersion        sql.NullString
	rsyncAvailable      sql.NullBool
	sftpAvailable       sql.NullBool
	hardlinksSupported  sql.NullBool
	reflinksSupported   sql.NullBool
	capabilitiesChecked sql.NullTime
}

// applyToConnection applies the scanned nullable fields to a connection.
func (f *connectionScanFields) applyToConnection(c *InstanceConnection) {
	if f.passwordEncrypted.Valid {
		c.passwordEncrypted = f.passwordEncrypted.String
	}
	if f.privateKeyPath.Valid {
		c.PrivateKeyPath = f.privateKeyPath.String
	}
	if f.rsyncAvailable.Valid {
		c.RsyncAvailable = &f.rsyncAvailable.Bool
	}
	if f.rsyncVersion.Valid {
		c.RsyncVersion = f.rsyncVersion.String
	}
	if f.sftpAvailable.Valid {
		c.SFTPAvailable = &f.sftpAvailable.Bool
	}
	if f.hardlinksSupported.Valid {
		c.HardlinksSupported = &f.hardlinksSupported.Bool
	}
	if f.reflinksSupported.Valid {
		c.ReflinksSupported = &f.reflinksSupported.Bool
	}
	if f.capabilitiesChecked.Valid {
		c.CapabilitiesChecked = &f.capabilitiesChecked.Time
	}
}

// scanConnection scans a single row into an InstanceConnection.
func (s *InstanceConnectionStore) scanConnection(row *sql.Row) (*InstanceConnection, error) {
	var c InstanceConnection
	var f connectionScanFields

	err := row.Scan(
		&c.ID, &c.InstanceID, &c.Type, &c.Host, &c.Port,
		&c.Username, &f.passwordEncrypted, &f.privateKeyPath, &c.Enabled,
		&f.rsyncAvailable, &f.rsyncVersion, &f.sftpAvailable, &f.hardlinksSupported, &f.reflinksSupported,
		&f.capabilitiesChecked, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrConnectionNotFound
		}
		return nil, err
	}

	f.applyToConnection(&c)
	return &c, nil
}

// scanConnections scans multiple rows into InstanceConnection slice.
func (s *InstanceConnectionStore) scanConnections(rows *sql.Rows) ([]*InstanceConnection, error) {
	var connections []*InstanceConnection
	for rows.Next() {
		var c InstanceConnection
		var f connectionScanFields

		err := rows.Scan(
			&c.ID, &c.InstanceID, &c.Type, &c.Host, &c.Port,
			&c.Username, &f.passwordEncrypted, &f.privateKeyPath, &c.Enabled,
			&f.rsyncAvailable, &f.rsyncVersion, &f.sftpAvailable, &f.hardlinksSupported, &f.reflinksSupported,
			&f.capabilitiesChecked, &c.CreatedAt, &c.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}

		f.applyToConnection(&c)
		connections = append(connections, &c)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}
	return connections, nil
}

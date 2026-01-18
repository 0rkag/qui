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
	"regexp"
	"strings"
	"time"

	"github.com/autobrr/qui/internal/dbinterface"
)

// Validation patterns for FTP connection configuration
var (
	validFTPUsername = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9_.\-@]*$`)
	validFTPHostname = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.\-:\[\]]*$`)
)

// Validation limits for FTP connections
const (
	MaxFTPHostnameLength = 253
	MaxFTPUsernameLength = 64
	MaxFTPPasswordLength = 256
	MaxFTPBasePathLength = 4096
)

var (
	ErrFTPConnectionNotFound  = errors.New("FTP connection not found")
	ErrDuplicateFTPConnection = errors.New("FTP connection already exists for this instance")
	ErrInvalidFTPConfig       = errors.New("invalid FTP connection configuration")
)

// InstanceFTPConnection represents an FTP/FTPS connection configuration for an instance.
type InstanceFTPConnection struct {
	ID         int64     `json:"id"`
	InstanceID int       `json:"instanceId"`
	Host       string    `json:"host"`
	Port       int       `json:"port"`
	Username   string    `json:"username"`
	Password   string    `json:"password,omitempty"` // Only for input, never returned
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`

	// TLS settings
	UseTLS        bool `json:"useTls"`
	TLSSkipVerify bool `json:"tlsSkipVerify"`

	// Transfer settings
	PassiveMode bool   `json:"passiveMode"`
	BasePath    string `json:"basePath,omitempty"`

	// Detected capabilities (cached from connection test)
	TLSEnabled       *bool      `json:"tlsEnabled,omitempty"`
	PassiveModeWorks *bool      `json:"passiveModeWorks,omitempty"`
	FXPSupported     *bool      `json:"fxpSupported,omitempty"`
	CapabilitiesChecked *time.Time `json:"capabilitiesCheckedAt,omitempty"`

	Enabled bool `json:"enabled"`

	// Internal: encrypted password (not exported to JSON)
	passwordEncrypted string
}

// Validate checks that the FTP connection has valid data.
func (c *InstanceFTPConnection) Validate() error {
	if c.InstanceID <= 0 {
		return errors.New("instance ID is required")
	}

	// Validate host
	host := strings.TrimSpace(c.Host)
	if host == "" {
		return errors.New("host is required")
	}
	if len(host) > MaxFTPHostnameLength {
		return errors.New("host too long (max 253 chars)")
	}
	if !validFTPHostname.MatchString(host) {
		return errors.New("host contains invalid characters")
	}
	c.Host = host

	// Validate port
	if c.Port <= 0 || c.Port > 65535 {
		return errors.New("port must be between 1 and 65535")
	}

	// Validate username
	username := strings.TrimSpace(c.Username)
	if username == "" {
		return errors.New("username is required")
	}
	if len(username) > MaxFTPUsernameLength {
		return errors.New("username too long (max 64 chars)")
	}
	if !validFTPUsername.MatchString(username) {
		return errors.New("username contains invalid characters")
	}
	c.Username = username

	// Validate base path if provided
	if c.BasePath != "" {
		if len(c.BasePath) > MaxFTPBasePathLength {
			return errors.New("base path too long")
		}
	}

	return nil
}

// FTPCapabilities holds detected FTP server capabilities.
type FTPCapabilities struct {
	TLSEnabled       bool `json:"tlsEnabled"`
	PassiveModeWorks bool `json:"passiveModeWorks"`
	FXPSupported     bool `json:"fxpSupported"`
}

// InstanceFTPConnectionStore handles database operations for FTP connections.
type InstanceFTPConnectionStore struct {
	db            dbinterface.Querier
	encryptionKey []byte
}

// NewInstanceFTPConnectionStore creates a new InstanceFTPConnectionStore.
func NewInstanceFTPConnectionStore(db dbinterface.Querier, encryptionKey []byte) (*InstanceFTPConnectionStore, error) {
	if len(encryptionKey) != 32 {
		return nil, errors.New("encryption key must be 32 bytes")
	}

	return &InstanceFTPConnectionStore{
		db:            db,
		encryptionKey: encryptionKey,
	}, nil
}

// encrypt encrypts a string using AES-GCM.
func (s *InstanceFTPConnectionStore) encrypt(plaintext string) (string, error) {
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
func (s *InstanceFTPConnectionStore) decrypt(ciphertext string) (string, error) {
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

// Create inserts a new FTP connection configuration.
func (s *InstanceFTPConnectionStore) Create(ctx context.Context, c *InstanceFTPConnection) (*InstanceFTPConnection, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}

	if c.Password == "" {
		return nil, errors.New("password is required")
	}

	// Encrypt the password
	encryptedPassword, err := s.encrypt(c.Password)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO instance_ftp_connections (
			instance_id, host, port, username, password_encrypted,
			use_tls, tls_skip_verify, passive_mode, base_path,
			enabled, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.InstanceID, c.Host, c.Port, c.Username, encryptedPassword,
		c.UseTLS, c.TLSSkipVerify, c.PassiveMode, c.BasePath,
		c.Enabled, now, now,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return nil, ErrDuplicateFTPConnection
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

// Get retrieves an FTP connection by ID.
func (s *InstanceFTPConnectionStore) Get(ctx context.Context, id int64) (*InstanceFTPConnection, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, instance_id, host, port, username, password_encrypted,
			use_tls, tls_skip_verify, passive_mode, base_path,
			tls_enabled, passive_mode_works, fxp_supported, capabilities_checked_at,
			enabled, created_at, updated_at
		FROM instance_ftp_connections
		WHERE id = ?`, id)

	return s.scanConnection(row)
}

// GetByInstance retrieves the FTP connection for an instance.
func (s *InstanceFTPConnectionStore) GetByInstance(ctx context.Context, instanceID int) (*InstanceFTPConnection, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, instance_id, host, port, username, password_encrypted,
			use_tls, tls_skip_verify, passive_mode, base_path,
			tls_enabled, passive_mode_works, fxp_supported, capabilities_checked_at,
			enabled, created_at, updated_at
		FROM instance_ftp_connections
		WHERE instance_id = ?`, instanceID)

	return s.scanConnection(row)
}

// Update modifies an existing FTP connection.
func (s *InstanceFTPConnectionStore) Update(ctx context.Context, c *InstanceFTPConnection) error {
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
			UPDATE instance_ftp_connections
			SET host = ?, port = ?, username = ?, password_encrypted = ?,
				use_tls = ?, tls_skip_verify = ?, passive_mode = ?, base_path = ?,
				enabled = ?, updated_at = ?
			WHERE id = ?`
		args = []interface{}{
			c.Host, c.Port, c.Username, encryptedPassword,
			c.UseTLS, c.TLSSkipVerify, c.PassiveMode, c.BasePath,
			c.Enabled, c.UpdatedAt, c.ID,
		}
	} else {
		query = `
			UPDATE instance_ftp_connections
			SET host = ?, port = ?, username = ?,
				use_tls = ?, tls_skip_verify = ?, passive_mode = ?, base_path = ?,
				enabled = ?, updated_at = ?
			WHERE id = ?`
		args = []interface{}{
			c.Host, c.Port, c.Username,
			c.UseTLS, c.TLSSkipVerify, c.PassiveMode, c.BasePath,
			c.Enabled, c.UpdatedAt, c.ID,
		}
	}

	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrFTPConnectionNotFound
	}

	c.Password = "" // Clear password from memory
	return nil
}

// Delete removes an FTP connection by ID.
func (s *InstanceFTPConnectionStore) Delete(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM instance_ftp_connections WHERE id = ?`, id)
	if err != nil {
		return err
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrFTPConnectionNotFound
	}
	return nil
}

// DeleteByInstance removes the FTP connection for an instance.
func (s *InstanceFTPConnectionStore) DeleteByInstance(ctx context.Context, instanceID int) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM instance_ftp_connections WHERE instance_id = ?`, instanceID)
	return err
}

// UpdateCapabilities updates the cached capabilities for an FTP connection.
func (s *InstanceFTPConnectionStore) UpdateCapabilities(ctx context.Context, id int64, caps *FTPCapabilities) error {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
		UPDATE instance_ftp_connections
		SET tls_enabled = ?, passive_mode_works = ?, fxp_supported = ?,
			capabilities_checked_at = ?, updated_at = ?
		WHERE id = ?`,
		caps.TLSEnabled, caps.PassiveModeWorks, caps.FXPSupported,
		now, now, id,
	)
	return err
}

// GetDecryptedPassword returns the decrypted password for an FTP connection.
func (s *InstanceFTPConnectionStore) GetDecryptedPassword(c *InstanceFTPConnection) (string, error) {
	return s.decrypt(c.passwordEncrypted)
}

// scanConnection scans a single row into an InstanceFTPConnection.
func (s *InstanceFTPConnectionStore) scanConnection(row *sql.Row) (*InstanceFTPConnection, error) {
	var c InstanceFTPConnection
	var basePath sql.NullString
	var tlsEnabled, passiveModeWorks, fxpSupported sql.NullBool
	var capabilitiesChecked sql.NullTime

	err := row.Scan(
		&c.ID, &c.InstanceID, &c.Host, &c.Port, &c.Username, &c.passwordEncrypted,
		&c.UseTLS, &c.TLSSkipVerify, &c.PassiveMode, &basePath,
		&tlsEnabled, &passiveModeWorks, &fxpSupported, &capabilitiesChecked,
		&c.Enabled, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrFTPConnectionNotFound
		}
		return nil, err
	}

	if basePath.Valid {
		c.BasePath = basePath.String
	}
	if tlsEnabled.Valid {
		c.TLSEnabled = &tlsEnabled.Bool
	}
	if passiveModeWorks.Valid {
		c.PassiveModeWorks = &passiveModeWorks.Bool
	}
	if fxpSupported.Valid {
		c.FXPSupported = &fxpSupported.Bool
	}
	if capabilitiesChecked.Valid {
		c.CapabilitiesChecked = &capabilitiesChecked.Time
	}

	return &c, nil
}

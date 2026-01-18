// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/ftpclient"
	"github.com/autobrr/qui/pkg/sshclient"
)

// sanitizeSSHError returns a user-friendly error message without exposing sensitive details.
func sanitizeSSHError(err error) string {
	errStr := err.Error()

	switch {
	case strings.Contains(errStr, "no such file or directory"):
		return "Private key file not found"
	case strings.Contains(errStr, "permission denied"):
		return "Authentication failed (check username and credentials)"
	case strings.Contains(errStr, "connection refused"):
		return "Connection refused (check host and port)"
	case strings.Contains(errStr, "no route to host"):
		return "Host unreachable (check network connectivity)"
	case strings.Contains(errStr, "i/o timeout"):
		return "Connection timed out"
	case strings.Contains(errStr, "host key"):
		return "Host key verification issue"
	case strings.Contains(errStr, "parse"):
		return "Invalid private key format"
	case strings.Contains(errStr, "no authentication method"):
		return "No authentication method provided"
	default:
		return "Connection failed"
	}
}

// sanitizeFTPError returns a user-friendly error message for FTP errors.
func sanitizeFTPError(err error) string {
	errStr := err.Error()

	switch {
	case strings.Contains(errStr, "connection refused"):
		return "Connection refused (check host and port)"
	case strings.Contains(errStr, "no such host"):
		return "Host not found"
	case strings.Contains(errStr, "i/o timeout"):
		return "Connection timed out"
	case strings.Contains(errStr, "530"):
		return "Login failed (check username and password)"
	case strings.Contains(errStr, "TLS"):
		return "TLS handshake failed"
	case strings.Contains(errStr, "certificate"):
		return "Certificate verification failed"
	default:
		return "Connection failed"
	}
}

// maxRequestBodySize limits request body size to prevent memory exhaustion attacks.
const maxRequestBodySize = 1 << 20 // 1 MB

// sshTestParams holds parameters for SSH connection testing.
type sshTestParams struct {
	Host           string
	Port           int
	Username       string
	Password       string
	PrivateKeyPath string
	// Host key verification (TOFU)
	ExpectedHostKey *sshclient.HostKeyInfo // If set, verify the host key matches
}

// doSSHConnectionTest performs SSH connection test and returns the result.
func doSSHConnectionTest(ctx context.Context, params sshTestParams) ConnectionTestResult {
	// Validate private key path if provided
	if params.PrivateKeyPath != "" {
		if err := sshclient.ValidatePath(params.PrivateKeyPath); err != nil {
			return ConnectionTestResult{
				Success: false,
				Message: "Invalid private key path: " + err.Error(),
			}
		}
	}

	cfg := &sshclient.Config{
		Host:           params.Host,
		Port:           params.Port,
		Username:       params.Username,
		Password:       params.Password,
		PrivateKeyPath: params.PrivateKeyPath,
		ExpectedHostKey: params.ExpectedHostKey,
	}

	client, hostKeyInfo, err := sshclient.New(cfg)
	if err != nil {
		log.Debug().Err(err).Str("host", params.Host).Msg("connections: SSH test failed")

		// Check if this is a host key mismatch error
		var hostKeyErr *sshclient.HostKeyError
		if errors.As(err, &hostKeyErr) {
			return ConnectionTestResult{
				Success:            false,
				Message:            "Host key mismatch: the server's key has changed",
				Details:            fmt.Sprintf("Expected: %s, Got: %s", hostKeyErr.Expected, hostKeyErr.Actual),
				HostKeyFingerprint: hostKeyErr.Actual,
				HostKeyMismatch:    true,
			}
		}

		return ConnectionTestResult{
			Success: false,
			Message: sanitizeSSHError(err),
		}
	}
	defer client.Close()

	// Build result with host key info for TOFU
	result := ConnectionTestResult{
		Success: true,
	}
	if hostKeyInfo != nil {
		result.HostKeyFingerprint = hostKeyInfo.Fingerprint
		result.HostKeyAlgorithm = hostKeyInfo.Algorithm
	}

	_, err = client.Exec(ctx, "echo 'Connection successful'")
	if err != nil {
		log.Debug().Err(err).Str("host", params.Host).Msg("connections: SSH command execution failed")
		result.Success = false
		result.Message = "Connection established but command execution failed"
		return result
	}

	caps, err := client.CheckCapabilities(ctx)
	if err != nil {
		log.Warn().Err(err).Str("host", params.Host).Msg("connections: failed to check SSH capabilities")
		result.Message = "Connection successful (capability detection failed)"
		return result
	}

	result.Message = "Connection successful"
	result.SSHCapabilities = caps
	return result
}

// ftpTestParams holds parameters for FTP connection testing.
type ftpTestParams struct {
	Host           string
	Port           int
	Username       string
	Password       string
	ConnectionType string // ssh_auto, ftp_explicit, etc.
	TLSSkipVerify  bool   // Skip TLS certificate verification
}

// doFTPConnectionTest performs FTP connection test and returns the result.
func doFTPConnectionTest(params ftpTestParams) ConnectionTestResult {
	if params.Password == "" {
		return ConnectionTestResult{
			Success: false,
			Message: "Password is required for FTP connections",
		}
	}

	// Determine TLS mode from connection type
	var tlsMode string
	switch params.ConnectionType {
	case models.ConnectionTypeFTPExplicit:
		tlsMode = ftpclient.TLSModeExplicit
	case models.ConnectionTypeFTPImplicit:
		tlsMode = ftpclient.TLSModeImplicit
	case models.ConnectionTypeFTPPlain:
		tlsMode = ftpclient.TLSModeNone
	default:
		tlsMode = ftpclient.TLSModeExplicit
	}

	cfg := &ftpclient.Config{
		Host:          params.Host,
		Port:          params.Port,
		Username:      params.Username,
		Password:      params.Password,
		TLSMode:       tlsMode,
		SkipTLSVerify: params.TLSSkipVerify,
	}

	client, err := ftpclient.New(cfg)
	if err != nil {
		log.Debug().Err(err).Str("host", params.Host).Msg("connections: FTP test failed")
		return ConnectionTestResult{
			Success: false,
			Message: sanitizeFTPError(err),
		}
	}
	defer client.Close()

	caps, err := client.CheckCapabilities()
	if err != nil {
		log.Warn().Err(err).Str("host", params.Host).Msg("connections: FTP capability check failed")
		return ConnectionTestResult{
			Success: true,
			Message: "Connection successful (capability detection failed)",
		}
	}

	message := "Connection successful"
	if caps.TLSEnabled {
		message += " • TLS enabled"
	}
	if caps.PassiveModeWorks {
		message += " • Passive mode working"
	}

	return ConnectionTestResult{
		Success:         true,
		Message:         message,
		FTPCapabilities: caps,
	}
}

// InstanceConnectionsHandler handles instance connection API endpoints.
type InstanceConnectionsHandler struct {
	store            *models.InstanceConnectionStore
	pathMappingStore *models.InstancePathMappingStore
}

// NewInstanceConnectionsHandler creates a new InstanceConnectionsHandler.
func NewInstanceConnectionsHandler(store *models.InstanceConnectionStore, pathMappingStore *models.InstancePathMappingStore) *InstanceConnectionsHandler {
	return &InstanceConnectionsHandler{
		store:            store,
		pathMappingStore: pathMappingStore,
	}
}

// CreateConnectionPayload is the request body for creating a connection.
type CreateConnectionPayload struct {
	Type           string `json:"type"` // ssh_auto, ssh_rsync, ssh_sftp, ssh_scp, ftp_explicit, ftp_implicit, ftp_plain
	Host           string `json:"host"`
	Port           int    `json:"port"`
	Username       string `json:"username"`
	Password       string `json:"password,omitempty"`
	PrivateKeyPath string `json:"privateKeyPath,omitempty"`
	Enabled        *bool  `json:"enabled,omitempty"`
	TLSSkipVerify  bool   `json:"tlsSkipVerify,omitempty"` // Skip TLS cert verification for FTP
}

// UpdateConnectionPayload is the request body for updating a connection.
type UpdateConnectionPayload struct {
	Host           string `json:"host"`
	Port           int    `json:"port"`
	Username       string `json:"username"`
	Password       string `json:"password,omitempty"` // Optional on update
	PrivateKeyPath string `json:"privateKeyPath,omitempty"`
	Enabled        *bool  `json:"enabled,omitempty"`
	TLSSkipVerify  *bool  `json:"tlsSkipVerify,omitempty"` // Skip TLS cert verification for FTP
}

// TestConnectionPayload is the request body for testing a connection.
type TestConnectionPayload struct {
	Type           string `json:"type"`
	Host           string `json:"host"`
	Port           int    `json:"port"`
	Username       string `json:"username"`
	Password       string `json:"password,omitempty"`
	PrivateKeyPath string `json:"privateKeyPath,omitempty"`
	TLSSkipVerify  bool   `json:"tlsSkipVerify,omitempty"` // Skip TLS cert verification for FTP
}

// ConnectionTestResult is the response for connection testing.
type ConnectionTestResult struct {
	Success         bool                     `json:"success"`
	Message         string                   `json:"message"`
	Details         string                   `json:"details,omitempty"`
	SSHCapabilities *sshclient.Capabilities  `json:"sshCapabilities,omitempty"`
	FTPCapabilities *ftpclient.Capabilities  `json:"ftpCapabilities,omitempty"`
	// SSH Host Key info (for TOFU - Trust On First Use)
	HostKeyFingerprint string `json:"hostKeyFingerprint,omitempty"`
	HostKeyAlgorithm   string `json:"hostKeyAlgorithm,omitempty"`
	HostKeyMismatch    bool   `json:"hostKeyMismatch,omitempty"` // True if key changed from stored value
}

// List handles GET /api/instances/{instanceID}/connections
func (h *InstanceConnectionsHandler) List(w http.ResponseWriter, r *http.Request) {
	instanceID, ok := parseIntParam(w, r, "instanceID", "Invalid instance ID")
	if !ok {
		return
	}

	connections, err := h.store.ListByInstance(r.Context(), instanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("connections: failed to list connections")
		RespondError(w, http.StatusInternalServerError, "Failed to list connections")
		return
	}

	if connections == nil {
		connections = []*models.InstanceConnection{}
	}

	RespondJSON(w, http.StatusOK, connections)
}

// Create handles POST /api/instances/{instanceID}/connections
func (h *InstanceConnectionsHandler) Create(w http.ResponseWriter, r *http.Request) {
	instanceID, ok := parseIntParam(w, r, "instanceID", "Invalid instance ID")
	if !ok {
		return
	}

	// Check if at least one path mapping exists
	mappings, err := h.pathMappingStore.ListByInstance(r.Context(), instanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("connections: failed to check path mappings")
		RespondError(w, http.StatusInternalServerError, "Failed to verify path mappings")
		return
	}
	if len(mappings) == 0 {
		RespondError(w, http.StatusBadRequest, "At least one path mapping must be configured before creating a connection")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)

	var payload CreateConnectionPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		log.Warn().Err(err).Msg("connections: failed to decode create payload")
		RespondError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	// Validate private key path if provided (SSH types only)
	if payload.PrivateKeyPath != "" {
		if !models.IsSSHType(payload.Type) {
			RespondError(w, http.StatusBadRequest, "Private key path is only valid for SSH connection types")
			return
		}
		if err := sshclient.ValidatePath(payload.PrivateKeyPath); err != nil {
			RespondError(w, http.StatusBadRequest, "Invalid private key path: "+err.Error())
			return
		}
	}

	enabled := true
	if payload.Enabled != nil {
		enabled = *payload.Enabled
	}

	port := payload.Port
	if port == 0 {
		port = models.DefaultPort(payload.Type)
	}

	conn := &models.InstanceConnection{
		InstanceID:     instanceID,
		Type:           payload.Type,
		Host:           payload.Host,
		Port:           port,
		Username:       payload.Username,
		Password:       payload.Password,
		PrivateKeyPath: payload.PrivateKeyPath,
		Enabled:        enabled,
		TLSSkipVerify:  payload.TLSSkipVerify,
	}

	created, err := h.store.Create(r.Context(), conn)
	if err != nil {
		if errors.Is(err, models.ErrDuplicateConnection) {
			RespondError(w, http.StatusConflict, "Connection already exists for this type")
			return
		}
		if errors.Is(err, models.ErrUnsupportedType) {
			RespondError(w, http.StatusBadRequest, "Unsupported connection type")
			return
		}
		if errors.Is(err, models.ErrInvalidConnectionConfig) {
			RespondError(w, http.StatusBadRequest, err.Error())
			return
		}
		// Check for validation errors from Validate()
		if strings.Contains(err.Error(), "require") {
			RespondError(w, http.StatusBadRequest, err.Error())
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Msg("connections: failed to create connection")
		RespondError(w, http.StatusInternalServerError, "Failed to create connection")
		return
	}

	RespondJSON(w, http.StatusCreated, created)
}

// Get handles GET /api/instances/{instanceID}/connections/{id}
func (h *InstanceConnectionsHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, ok := parseInt64Param(w, r, "id", "Invalid connection ID")
	if !ok {
		return
	}

	conn, err := h.store.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, models.ErrConnectionNotFound) {
			RespondError(w, http.StatusNotFound, "Connection not found")
			return
		}
		log.Error().Err(err).Int64("id", id).Msg("connections: failed to get connection")
		RespondError(w, http.StatusInternalServerError, "Failed to get connection")
		return
	}

	RespondJSON(w, http.StatusOK, conn)
}

// Update handles PUT /api/instances/{instanceID}/connections/{id}
func (h *InstanceConnectionsHandler) Update(w http.ResponseWriter, r *http.Request) {
	instanceID, ok := parseIntParam(w, r, "instanceID", "Invalid instance ID")
	if !ok {
		return
	}

	id, ok := parseInt64Param(w, r, "id", "Invalid connection ID")
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)

	var payload UpdateConnectionPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		log.Warn().Err(err).Msg("connections: failed to decode update payload")
		RespondError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	existing, err := h.store.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, models.ErrConnectionNotFound) {
			RespondError(w, http.StatusNotFound, "Connection not found")
			return
		}
		log.Error().Err(err).Int64("id", id).Msg("connections: failed to get existing connection")
		RespondError(w, http.StatusInternalServerError, "Failed to get connection")
		return
	}

	if existing.InstanceID != instanceID {
		RespondError(w, http.StatusNotFound, "Connection not found")
		return
	}

	// Validate private key path if provided (SSH types only)
	if payload.PrivateKeyPath != "" {
		if !models.IsSSHType(existing.Type) {
			RespondError(w, http.StatusBadRequest, "Private key path is only valid for SSH connection types")
			return
		}
		if err := sshclient.ValidatePath(payload.PrivateKeyPath); err != nil {
			RespondError(w, http.StatusBadRequest, "Invalid private key path: "+err.Error())
			return
		}
	}

	existing.Host = payload.Host
	existing.Port = payload.Port
	existing.Username = payload.Username
	existing.Password = payload.Password // May be empty (keeps existing)
	existing.PrivateKeyPath = payload.PrivateKeyPath
	if payload.Enabled != nil {
		existing.Enabled = *payload.Enabled
	}
	if payload.TLSSkipVerify != nil {
		existing.TLSSkipVerify = *payload.TLSSkipVerify
	}

	if err := h.store.Update(r.Context(), existing); err != nil {
		if errors.Is(err, models.ErrConnectionNotFound) {
			RespondError(w, http.StatusNotFound, "Connection not found")
			return
		}
		log.Error().Err(err).Int64("id", id).Msg("connections: failed to update connection")
		RespondError(w, http.StatusInternalServerError, "Failed to update connection")
		return
	}

	RespondJSON(w, http.StatusOK, existing)
}

// Delete handles DELETE /api/instances/{instanceID}/connections/{id}
func (h *InstanceConnectionsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	instanceID, ok := parseIntParam(w, r, "instanceID", "Invalid instance ID")
	if !ok {
		return
	}

	id, ok := parseInt64Param(w, r, "id", "Invalid connection ID")
	if !ok {
		return
	}

	existing, err := h.store.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, models.ErrConnectionNotFound) {
			RespondError(w, http.StatusNotFound, "Connection not found")
			return
		}
		log.Error().Err(err).Int64("id", id).Msg("connections: failed to get connection for delete")
		RespondError(w, http.StatusInternalServerError, "Failed to get connection")
		return
	}

	if existing.InstanceID != instanceID {
		RespondError(w, http.StatusNotFound, "Connection not found")
		return
	}

	if err := h.store.Delete(r.Context(), id); err != nil {
		if errors.Is(err, models.ErrConnectionNotFound) {
			RespondError(w, http.StatusNotFound, "Connection not found")
			return
		}
		log.Error().Err(err).Int64("id", id).Msg("connections: failed to delete connection")
		RespondError(w, http.StatusInternalServerError, "Failed to delete connection")
		return
	}

	RespondJSON(w, http.StatusNoContent, nil)
}

// Test handles POST /api/instances/{instanceID}/connections/test
// Tests a connection without saving it.
func (h *InstanceConnectionsHandler) Test(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)

	var payload TestConnectionPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		log.Warn().Err(err).Msg("connections: failed to decode test payload")
		RespondError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	// Validate required fields
	if payload.Host == "" {
		RespondJSON(w, http.StatusOK, ConnectionTestResult{
			Success: false,
			Message: "Host is required",
		})
		return
	}

	if payload.Username == "" {
		RespondJSON(w, http.StatusOK, ConnectionTestResult{
			Success: false,
			Message: "Username is required",
		})
		return
	}

	port := payload.Port
	if port == 0 {
		port = models.DefaultPort(payload.Type)
	}

	if models.IsSSHType(payload.Type) {
		h.testSSHConnection(w, r, payload, port)
	} else if models.IsFTPType(payload.Type) {
		h.testFTPConnection(w, r, payload, port)
	} else {
		RespondJSON(w, http.StatusOK, ConnectionTestResult{
			Success: false,
			Message: "Invalid connection type",
		})
	}
}

// testSSHConnection tests an SSH connection.
func (h *InstanceConnectionsHandler) testSSHConnection(w http.ResponseWriter, r *http.Request, payload TestConnectionPayload, port int) {
	result := doSSHConnectionTest(r.Context(), sshTestParams{
		Host:           payload.Host,
		Port:           port,
		Username:       payload.Username,
		Password:       payload.Password,
		PrivateKeyPath: payload.PrivateKeyPath,
	})
	RespondJSON(w, http.StatusOK, result)
}

// testFTPConnection tests an FTP connection.
func (h *InstanceConnectionsHandler) testFTPConnection(w http.ResponseWriter, _ *http.Request, payload TestConnectionPayload, port int) {
	result := doFTPConnectionTest(ftpTestParams{
		Host:           payload.Host,
		Port:           port,
		Username:       payload.Username,
		Password:       payload.Password,
		ConnectionType: payload.Type,
		TLSSkipVerify:  payload.TLSSkipVerify,
	})
	RespondJSON(w, http.StatusOK, result)
}

// TestExisting handles POST /api/instances/{instanceID}/connections/{id}/test
// Tests an existing saved connection.
func (h *InstanceConnectionsHandler) TestExisting(w http.ResponseWriter, r *http.Request) {
	instanceID, ok := parseIntParam(w, r, "instanceID", "Invalid instance ID")
	if !ok {
		return
	}

	id, ok := parseInt64Param(w, r, "id", "Invalid connection ID")
	if !ok {
		return
	}

	conn, err := h.store.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, models.ErrConnectionNotFound) {
			RespondError(w, http.StatusNotFound, "Connection not found")
			return
		}
		log.Error().Err(err).Int64("id", id).Msg("connections: failed to get connection for test")
		RespondError(w, http.StatusInternalServerError, "Failed to get connection")
		return
	}

	if conn.InstanceID != instanceID {
		RespondError(w, http.StatusNotFound, "Connection not found")
		return
	}

	if models.IsSSHType(conn.Type) {
		h.testExistingSSH(w, r, conn)
	} else if models.IsFTPType(conn.Type) {
		h.testExistingFTP(w, r, conn)
	} else {
		RespondJSON(w, http.StatusOK, ConnectionTestResult{
			Success: false,
			Message: "Invalid connection type",
		})
	}
}

// testExistingSSH tests an existing SSH connection.
func (h *InstanceConnectionsHandler) testExistingSSH(w http.ResponseWriter, r *http.Request, conn *models.InstanceConnection) {
	// Get decrypted password if needed
	password, err := h.store.GetDecryptedPassword(conn)
	if err != nil {
		log.Error().Err(err).Int64("connID", conn.ID).Msg("connections: failed to decrypt password")
		// Continue without password if key is available
		password = ""
	}

	ctx := r.Context()

	// Build expected host key from stored values (TOFU)
	var expectedHostKey *sshclient.HostKeyInfo
	if conn.HostKeyFingerprint != nil && *conn.HostKeyFingerprint != "" {
		alg := ""
		if conn.HostKeyAlgorithm != nil {
			alg = *conn.HostKeyAlgorithm
		}
		expectedHostKey = &sshclient.HostKeyInfo{
			Fingerprint: *conn.HostKeyFingerprint,
			Algorithm:   alg,
		}
	}

	result := doSSHConnectionTest(ctx, sshTestParams{
		Host:            conn.Host,
		Port:            conn.Port,
		Username:        conn.Username,
		Password:        password,
		PrivateKeyPath:  conn.PrivateKeyPath,
		ExpectedHostKey: expectedHostKey,
	})

	// If successful and we got a new host key, store it (TOFU - Trust On First Use)
	if result.Success && result.HostKeyFingerprint != "" {
		// Only update if we didn't have a key before (first use)
		if conn.HostKeyFingerprint == nil || *conn.HostKeyFingerprint == "" {
			if err := h.store.UpdateHostKey(ctx, conn.ID, result.HostKeyFingerprint, result.HostKeyAlgorithm); err != nil {
				log.Warn().Err(err).Int64("connID", conn.ID).Msg("connections: failed to save host key")
			} else {
				log.Info().
					Int64("connID", conn.ID).
					Str("fingerprint", result.HostKeyFingerprint).
					Str("algorithm", result.HostKeyAlgorithm).
					Msg("connections: saved SSH host key (TOFU)")
			}
		}
	}

	// Save capabilities to database if test was successful and we got caps
	if result.Success && result.SSHCapabilities != nil {
		dbCaps := &models.ConnectionCapabilities{
			RsyncAvailable:     result.SSHCapabilities.RsyncAvailable,
			RsyncVersion:       result.SSHCapabilities.RsyncVersion,
			SFTPAvailable:      result.SSHCapabilities.SFTPAvailable,
			HardlinksSupported: result.SSHCapabilities.HardlinksSupported,
			ReflinksSupported:  result.SSHCapabilities.ReflinksSupported,
		}
		if err := h.store.UpdateCapabilities(ctx, conn.ID, dbCaps); err != nil {
			log.Warn().Err(err).Int64("connID", conn.ID).Msg("connections: failed to save capabilities")
		}
	}

	RespondJSON(w, http.StatusOK, result)
}

// testExistingFTP tests an existing FTP connection.
func (h *InstanceConnectionsHandler) testExistingFTP(w http.ResponseWriter, _ *http.Request, conn *models.InstanceConnection) {
	password, err := h.store.GetDecryptedPassword(conn)
	if err != nil {
		log.Error().Err(err).Int64("connID", conn.ID).Msg("connections: failed to decrypt password")
		RespondJSON(w, http.StatusOK, ConnectionTestResult{
			Success: false,
			Message: "Failed to decrypt password",
		})
		return
	}

	result := doFTPConnectionTest(ftpTestParams{
		Host:           conn.Host,
		Port:           conn.Port,
		Username:       conn.Username,
		Password:       password,
		ConnectionType: conn.Type,
		TLSSkipVerify:  conn.TLSSkipVerify,
	})
	RespondJSON(w, http.StatusOK, result)
}

// AcceptHostKeyPayload is the request body for accepting a new host key.
type AcceptHostKeyPayload struct {
	Fingerprint string `json:"fingerprint"` // The new host key fingerprint to accept
	Algorithm   string `json:"algorithm"`   // The key algorithm (e.g., "ssh-ed25519")
}

// AcceptHostKey handles POST /api/instances/{instanceID}/connections/{id}/accept-host-key
// This endpoint allows accepting a new host key when there's a mismatch (key changed).
func (h *InstanceConnectionsHandler) AcceptHostKey(w http.ResponseWriter, r *http.Request) {
	instanceID, ok := parseIntParam(w, r, "instanceID", "Invalid instance ID")
	if !ok {
		return
	}

	id, ok := parseInt64Param(w, r, "id", "Invalid connection ID")
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)

	var payload AcceptHostKeyPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		log.Warn().Err(err).Msg("connections: failed to decode accept-host-key payload")
		RespondError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	if payload.Fingerprint == "" {
		RespondError(w, http.StatusBadRequest, "Fingerprint is required")
		return
	}

	// Verify connection exists and belongs to instance
	conn, err := h.store.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, models.ErrConnectionNotFound) {
			RespondError(w, http.StatusNotFound, "Connection not found")
			return
		}
		log.Error().Err(err).Int64("id", id).Msg("connections: failed to get connection")
		RespondError(w, http.StatusInternalServerError, "Failed to get connection")
		return
	}

	if conn.InstanceID != instanceID {
		RespondError(w, http.StatusNotFound, "Connection not found")
		return
	}

	if !models.IsSSHType(conn.Type) {
		RespondError(w, http.StatusBadRequest, "Host key acceptance is only valid for SSH connections")
		return
	}

	// Update the host key
	if err := h.store.UpdateHostKey(r.Context(), id, payload.Fingerprint, payload.Algorithm); err != nil {
		log.Error().Err(err).Int64("id", id).Msg("connections: failed to update host key")
		RespondError(w, http.StatusInternalServerError, "Failed to update host key")
		return
	}

	log.Info().
		Int64("connID", id).
		Str("fingerprint", payload.Fingerprint).
		Str("algorithm", payload.Algorithm).
		Msg("connections: accepted new SSH host key")

	RespondJSON(w, http.StatusOK, map[string]string{
		"message": "Host key accepted",
	})
}

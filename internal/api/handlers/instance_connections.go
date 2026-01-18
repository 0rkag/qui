// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/sshclient"
)

// sanitizeSSHError returns a user-friendly error message without exposing sensitive details.
// The full error is logged server-side for debugging.
func sanitizeSSHError(err error) string {
	errStr := err.Error()

	// Map common SSH errors to user-friendly messages
	switch {
	case strings.Contains(errStr, "no such file or directory"):
		return "Private key file not found"
	case strings.Contains(errStr, "permission denied"):
		return "Authentication failed (check username and private key)"
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
	default:
		return "Connection failed"
	}
}

// maxRequestBodySize limits request body size to prevent memory exhaustion attacks.
const maxRequestBodySize = 1 << 20 // 1 MB

// InstanceConnectionsHandler handles instance connection API endpoints.
type InstanceConnectionsHandler struct {
	store *models.InstanceConnectionStore
}

// NewInstanceConnectionsHandler creates a new InstanceConnectionsHandler.
func NewInstanceConnectionsHandler(store *models.InstanceConnectionStore) *InstanceConnectionsHandler {
	return &InstanceConnectionsHandler{store: store}
}

// CreateConnectionPayload is the request body for creating a connection.
type CreateConnectionPayload struct {
	Protocol       string `json:"protocol"`
	Host           string `json:"host"`
	Port           int    `json:"port"`
	Username       string `json:"username"`
	PrivateKeyPath string `json:"privateKeyPath,omitempty"`
	Enabled        *bool  `json:"enabled,omitempty"`
}

// UpdateConnectionPayload is the request body for updating a connection.
type UpdateConnectionPayload struct {
	Host           string `json:"host"`
	Port           int    `json:"port"`
	Username       string `json:"username"`
	PrivateKeyPath string `json:"privateKeyPath,omitempty"`
	Enabled        *bool  `json:"enabled,omitempty"`
}

// TestConnectionPayload is the request body for testing a connection.
type TestConnectionPayload struct {
	Protocol       string `json:"protocol"`
	Host           string `json:"host"`
	Port           int    `json:"port"`
	Username       string `json:"username"`
	PrivateKeyPath string `json:"privateKeyPath,omitempty"`
}

// SSHTestResult is the response for SSH connection testing.
type SSHTestResult struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Details string `json:"details,omitempty"`
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

	// Limit request body size
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)

	var payload CreateConnectionPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		log.Warn().Err(err).Msg("connections: failed to decode create payload")
		RespondError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	// Validate private key path if provided
	if payload.PrivateKeyPath != "" {
		if err := sshclient.ValidatePath(payload.PrivateKeyPath); err != nil {
			RespondError(w, http.StatusBadRequest, "Invalid private key path: "+err.Error())
			return
		}
	}

	enabled := true
	if payload.Enabled != nil {
		enabled = *payload.Enabled
	}

	// Use default port if not provided
	port := payload.Port
	if port == 0 {
		port = models.DefaultPort(payload.Protocol)
	}

	conn := &models.InstanceConnection{
		InstanceID:     instanceID,
		Protocol:       payload.Protocol,
		Host:           payload.Host,
		Port:           port,
		Username:       payload.Username,
		PrivateKeyPath: payload.PrivateKeyPath,
		Enabled:        enabled,
	}

	created, err := h.store.Create(r.Context(), conn)
	if err != nil {
		if errors.Is(err, models.ErrDuplicateConnection) {
			RespondError(w, http.StatusConflict, "Connection already exists for this protocol")
			return
		}
		if errors.Is(err, models.ErrUnsupportedProtocol) {
			RespondError(w, http.StatusBadRequest, "Unsupported protocol. Use ssh, sftp, or ftp")
			return
		}
		// Check for validation errors
		if errors.Is(err, models.ErrInvalidConnectionConfig) {
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

	// Limit request body size
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)

	var payload UpdateConnectionPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		log.Warn().Err(err).Msg("connections: failed to decode update payload")
		RespondError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	// Validate private key path if provided
	if payload.PrivateKeyPath != "" {
		if err := sshclient.ValidatePath(payload.PrivateKeyPath); err != nil {
			RespondError(w, http.StatusBadRequest, "Invalid private key path: "+err.Error())
			return
		}
	}

	// Get existing connection to preserve fields
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

	// Verify the connection belongs to the specified instance
	if existing.InstanceID != instanceID {
		RespondError(w, http.StatusNotFound, "Connection not found")
		return
	}

	// Update fields
	existing.Host = payload.Host
	existing.Port = payload.Port
	existing.Username = payload.Username
	existing.PrivateKeyPath = payload.PrivateKeyPath
	if payload.Enabled != nil {
		existing.Enabled = *payload.Enabled
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

	// Verify the connection belongs to the specified instance
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
// Tests an SSH/SFTP connection without saving it.
func (h *InstanceConnectionsHandler) Test(w http.ResponseWriter, r *http.Request) {
	// Limit request body size
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)

	var payload TestConnectionPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		log.Warn().Err(err).Msg("connections: failed to decode test payload")
		RespondError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	// Validate private key path if provided
	if payload.PrivateKeyPath != "" {
		if err := sshclient.ValidatePath(payload.PrivateKeyPath); err != nil {
			RespondJSON(w, http.StatusOK, SSHTestResult{
				Success: false,
				Message: "Invalid private key path: " + err.Error(),
			})
			return
		}
	}

	// Only SSH and SFTP can be tested (they use the same protocol)
	if payload.Protocol != models.ProtocolSSH && payload.Protocol != models.ProtocolSFTP {
		RespondJSON(w, http.StatusOK, SSHTestResult{
			Success: false,
			Message: "Only SSH and SFTP connections can be tested",
		})
		return
	}

	// Validate required fields
	if payload.Host == "" {
		RespondJSON(w, http.StatusOK, SSHTestResult{
			Success: false,
			Message: "Host is required",
		})
		return
	}

	if payload.Username == "" {
		RespondJSON(w, http.StatusOK, SSHTestResult{
			Success: false,
			Message: "Username is required",
		})
		return
	}

	// Use default port if not provided
	port := payload.Port
	if port == 0 {
		port = models.DefaultPort(payload.Protocol)
	}

	// Create SSH config for testing
	cfg := &sshclient.Config{
		Host:           payload.Host,
		Port:           port,
		Username:       payload.Username,
		PrivateKeyPath: payload.PrivateKeyPath,
	}

	// Try to connect
	client, err := sshclient.New(cfg)
	if err != nil {
		log.Debug().Err(err).Str("host", payload.Host).Msg("connections: SSH test failed")
		RespondJSON(w, http.StatusOK, SSHTestResult{
			Success: false,
			Message: sanitizeSSHError(err),
		})
		return
	}
	defer client.Close()

	// Test connection by running a simple command
	ctx := r.Context()
	_, err = client.Exec(ctx, "echo 'Connection successful'")
	if err != nil {
		log.Debug().Err(err).Str("host", payload.Host).Msg("connections: SSH command execution failed")
		RespondJSON(w, http.StatusOK, SSHTestResult{
			Success: false,
			Message: "Connection established but command execution failed",
		})
		return
	}

	RespondJSON(w, http.StatusOK, SSHTestResult{
		Success: true,
		Message: "Connection successful",
	})
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

	// Get the connection
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

	// Verify the connection belongs to the specified instance
	if conn.InstanceID != instanceID {
		RespondError(w, http.StatusNotFound, "Connection not found")
		return
	}

	// Only SSH and SFTP can be tested
	if conn.Protocol != models.ProtocolSSH && conn.Protocol != models.ProtocolSFTP {
		RespondJSON(w, http.StatusOK, SSHTestResult{
			Success: false,
			Message: "Only SSH and SFTP connections can be tested",
		})
		return
	}

	// Create SSH config for testing
	cfg := &sshclient.Config{
		Host:           conn.Host,
		Port:           conn.Port,
		Username:       conn.Username,
		PrivateKeyPath: conn.PrivateKeyPath,
	}

	// Try to connect
	client, err := sshclient.New(cfg)
	if err != nil {
		log.Debug().Err(err).Str("host", conn.Host).Int64("connID", id).Msg("connections: SSH test failed")
		RespondJSON(w, http.StatusOK, SSHTestResult{
			Success: false,
			Message: sanitizeSSHError(err),
		})
		return
	}
	defer client.Close()

	// Test connection by running a simple command
	ctx := r.Context()
	_, err = client.Exec(ctx, "echo 'Connection successful'")
	if err != nil {
		log.Debug().Err(err).Str("host", conn.Host).Int64("connID", id).Msg("connections: SSH command execution failed")
		RespondJSON(w, http.StatusOK, SSHTestResult{
			Success: false,
			Message: "Connection established but command execution failed",
		})
		return
	}

	RespondJSON(w, http.StatusOK, SSHTestResult{
		Success: true,
		Message: "Connection successful",
	})
}

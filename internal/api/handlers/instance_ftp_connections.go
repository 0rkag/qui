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
	"github.com/autobrr/qui/pkg/ftpclient"
)

// sanitizeFTPError returns a user-friendly error message.
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
		return "TLS handshake failed (try disabling TLS or skip verification)"
	case strings.Contains(errStr, "certificate"):
		return "Certificate verification failed (enable skip verification)"
	default:
		return "Connection failed"
	}
}

// InstanceFTPConnectionsHandler handles FTP connection API endpoints.
type InstanceFTPConnectionsHandler struct {
	store *models.InstanceFTPConnectionStore
}

// NewInstanceFTPConnectionsHandler creates a new InstanceFTPConnectionsHandler.
func NewInstanceFTPConnectionsHandler(store *models.InstanceFTPConnectionStore) *InstanceFTPConnectionsHandler {
	return &InstanceFTPConnectionsHandler{store: store}
}

// CreateFTPConnectionPayload is the request body for creating an FTP connection.
type CreateFTPConnectionPayload struct {
	Host          string `json:"host"`
	Port          int    `json:"port"`
	Username      string `json:"username"`
	Password      string `json:"password"`
	UseTLS        bool   `json:"useTls"`
	TLSSkipVerify bool   `json:"tlsSkipVerify"`
	PassiveMode   bool   `json:"passiveMode"`
	BasePath      string `json:"basePath,omitempty"`
	Enabled       *bool  `json:"enabled,omitempty"`
}

// UpdateFTPConnectionPayload is the request body for updating an FTP connection.
type UpdateFTPConnectionPayload struct {
	Host          string `json:"host"`
	Port          int    `json:"port"`
	Username      string `json:"username"`
	Password      string `json:"password,omitempty"` // Optional on update
	UseTLS        bool   `json:"useTls"`
	TLSSkipVerify bool   `json:"tlsSkipVerify"`
	PassiveMode   bool   `json:"passiveMode"`
	BasePath      string `json:"basePath,omitempty"`
	Enabled       *bool  `json:"enabled,omitempty"`
}

// TestFTPConnectionPayload is the request body for testing an FTP connection.
type TestFTPConnectionPayload struct {
	Host          string `json:"host"`
	Port          int    `json:"port"`
	Username      string `json:"username"`
	Password      string `json:"password"`
	UseTLS        bool   `json:"useTls"`
	TLSSkipVerify bool   `json:"tlsSkipVerify"`
	PassiveMode   bool   `json:"passiveMode"`
}

// FTPTestResult is the response for FTP connection testing.
type FTPTestResult struct {
	Success      bool                    `json:"success"`
	Message      string                  `json:"message"`
	Details      string                  `json:"details,omitempty"`
	Capabilities *ftpclient.Capabilities `json:"capabilities,omitempty"`
}

// Get handles GET /api/instances/{instanceID}/ftp-connection
func (h *InstanceFTPConnectionsHandler) Get(w http.ResponseWriter, r *http.Request) {
	instanceID, ok := parseIntParam(w, r, "instanceID", "Invalid instance ID")
	if !ok {
		return
	}

	conn, err := h.store.GetByInstance(r.Context(), instanceID)
	if err != nil {
		if errors.Is(err, models.ErrFTPConnectionNotFound) {
			RespondError(w, http.StatusNotFound, "FTP connection not found")
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Msg("ftp: failed to get connection")
		RespondError(w, http.StatusInternalServerError, "Failed to get FTP connection")
		return
	}

	RespondJSON(w, http.StatusOK, conn)
}

// Create handles POST /api/instances/{instanceID}/ftp-connection
func (h *InstanceFTPConnectionsHandler) Create(w http.ResponseWriter, r *http.Request) {
	instanceID, ok := parseIntParam(w, r, "instanceID", "Invalid instance ID")
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)

	var payload CreateFTPConnectionPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		log.Warn().Err(err).Msg("ftp: failed to decode create payload")
		RespondError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	enabled := true
	if payload.Enabled != nil {
		enabled = *payload.Enabled
	}

	port := payload.Port
	if port == 0 {
		port = 21
	}

	conn := &models.InstanceFTPConnection{
		InstanceID:    instanceID,
		Host:          payload.Host,
		Port:          port,
		Username:      payload.Username,
		Password:      payload.Password,
		UseTLS:        payload.UseTLS,
		TLSSkipVerify: payload.TLSSkipVerify,
		PassiveMode:   payload.PassiveMode,
		BasePath:      payload.BasePath,
		Enabled:       enabled,
	}

	created, err := h.store.Create(r.Context(), conn)
	if err != nil {
		if errors.Is(err, models.ErrDuplicateFTPConnection) {
			RespondError(w, http.StatusConflict, "FTP connection already exists for this instance")
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Msg("ftp: failed to create connection")
		RespondError(w, http.StatusInternalServerError, "Failed to create FTP connection")
		return
	}

	RespondJSON(w, http.StatusCreated, created)
}

// Update handles PUT /api/instances/{instanceID}/ftp-connection
func (h *InstanceFTPConnectionsHandler) Update(w http.ResponseWriter, r *http.Request) {
	instanceID, ok := parseIntParam(w, r, "instanceID", "Invalid instance ID")
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)

	var payload UpdateFTPConnectionPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		log.Warn().Err(err).Msg("ftp: failed to decode update payload")
		RespondError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	existing, err := h.store.GetByInstance(r.Context(), instanceID)
	if err != nil {
		if errors.Is(err, models.ErrFTPConnectionNotFound) {
			RespondError(w, http.StatusNotFound, "FTP connection not found")
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Msg("ftp: failed to get existing connection")
		RespondError(w, http.StatusInternalServerError, "Failed to get FTP connection")
		return
	}

	// Update fields
	existing.Host = payload.Host
	existing.Port = payload.Port
	existing.Username = payload.Username
	existing.Password = payload.Password // May be empty (keeps existing)
	existing.UseTLS = payload.UseTLS
	existing.TLSSkipVerify = payload.TLSSkipVerify
	existing.PassiveMode = payload.PassiveMode
	existing.BasePath = payload.BasePath
	if payload.Enabled != nil {
		existing.Enabled = *payload.Enabled
	}

	if err := h.store.Update(r.Context(), existing); err != nil {
		if errors.Is(err, models.ErrFTPConnectionNotFound) {
			RespondError(w, http.StatusNotFound, "FTP connection not found")
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Msg("ftp: failed to update connection")
		RespondError(w, http.StatusInternalServerError, "Failed to update FTP connection")
		return
	}

	RespondJSON(w, http.StatusOK, existing)
}

// Delete handles DELETE /api/instances/{instanceID}/ftp-connection
func (h *InstanceFTPConnectionsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	instanceID, ok := parseIntParam(w, r, "instanceID", "Invalid instance ID")
	if !ok {
		return
	}

	existing, err := h.store.GetByInstance(r.Context(), instanceID)
	if err != nil {
		if errors.Is(err, models.ErrFTPConnectionNotFound) {
			RespondError(w, http.StatusNotFound, "FTP connection not found")
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Msg("ftp: failed to get connection for delete")
		RespondError(w, http.StatusInternalServerError, "Failed to get FTP connection")
		return
	}

	if err := h.store.Delete(r.Context(), existing.ID); err != nil {
		if errors.Is(err, models.ErrFTPConnectionNotFound) {
			RespondError(w, http.StatusNotFound, "FTP connection not found")
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Msg("ftp: failed to delete connection")
		RespondError(w, http.StatusInternalServerError, "Failed to delete FTP connection")
		return
	}

	RespondJSON(w, http.StatusNoContent, nil)
}

// Test handles POST /api/instances/{instanceID}/ftp-connection/test
// Tests an FTP connection without saving it.
func (h *InstanceFTPConnectionsHandler) Test(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)

	var payload TestFTPConnectionPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		log.Warn().Err(err).Msg("ftp: failed to decode test payload")
		RespondError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	// Validate required fields
	if payload.Host == "" {
		RespondJSON(w, http.StatusOK, FTPTestResult{
			Success: false,
			Message: "Host is required",
		})
		return
	}

	if payload.Username == "" {
		RespondJSON(w, http.StatusOK, FTPTestResult{
			Success: false,
			Message: "Username is required",
		})
		return
	}

	if payload.Password == "" {
		RespondJSON(w, http.StatusOK, FTPTestResult{
			Success: false,
			Message: "Password is required",
		})
		return
	}

	port := payload.Port
	if port == 0 {
		port = 21
	}

	// Create FTP config for testing
	cfg := &ftpclient.Config{
		Host:          payload.Host,
		Port:          port,
		Username:      payload.Username,
		Password:      payload.Password,
		UseTLS:        payload.UseTLS,
		TLSSkipVerify: payload.TLSSkipVerify,
		PassiveMode:   payload.PassiveMode,
	}

	// Try to connect
	client, err := ftpclient.New(cfg)
	if err != nil {
		log.Debug().Err(err).Str("host", payload.Host).Msg("ftp: test failed")
		RespondJSON(w, http.StatusOK, FTPTestResult{
			Success: false,
			Message: sanitizeFTPError(err),
		})
		return
	}
	defer client.Close()

	// Check capabilities
	caps, err := client.CheckCapabilities()
	if err != nil {
		log.Warn().Err(err).Str("host", payload.Host).Msg("ftp: capability check failed")
		RespondJSON(w, http.StatusOK, FTPTestResult{
			Success: true,
			Message: "Connection successful (capability detection failed)",
		})
		return
	}

	// Build message
	message := "Connection successful"
	if cfg.UseTLS {
		message += " • TLS enabled"
	}
	if caps.PassiveModeWorks {
		message += " • Passive mode working"
	}

	RespondJSON(w, http.StatusOK, FTPTestResult{
		Success:      true,
		Message:      message,
		Capabilities: caps,
	})
}

// TestExisting handles POST /api/instances/{instanceID}/ftp-connection/test-existing
// Tests an existing saved FTP connection.
func (h *InstanceFTPConnectionsHandler) TestExisting(w http.ResponseWriter, r *http.Request) {
	instanceID, ok := parseIntParam(w, r, "instanceID", "Invalid instance ID")
	if !ok {
		return
	}

	conn, err := h.store.GetByInstance(r.Context(), instanceID)
	if err != nil {
		if errors.Is(err, models.ErrFTPConnectionNotFound) {
			RespondError(w, http.StatusNotFound, "FTP connection not found")
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Msg("ftp: failed to get connection for test")
		RespondError(w, http.StatusInternalServerError, "Failed to get FTP connection")
		return
	}

	// Get decrypted password
	password, err := h.store.GetDecryptedPassword(conn)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("ftp: failed to decrypt password")
		RespondJSON(w, http.StatusOK, FTPTestResult{
			Success: false,
			Message: "Failed to decrypt password",
		})
		return
	}

	// Create FTP config for testing
	cfg := &ftpclient.Config{
		Host:          conn.Host,
		Port:          conn.Port,
		Username:      conn.Username,
		Password:      password,
		UseTLS:        conn.UseTLS,
		TLSSkipVerify: conn.TLSSkipVerify,
		PassiveMode:   conn.PassiveMode,
	}

	// Try to connect
	client, err := ftpclient.New(cfg)
	if err != nil {
		log.Debug().Err(err).Str("host", conn.Host).Int("instanceID", instanceID).Msg("ftp: test failed")
		RespondJSON(w, http.StatusOK, FTPTestResult{
			Success: false,
			Message: sanitizeFTPError(err),
		})
		return
	}
	defer client.Close()

	// Check capabilities
	caps, err := client.CheckCapabilities()
	if err != nil {
		log.Warn().Err(err).Str("host", conn.Host).Msg("ftp: capability check failed")
		RespondJSON(w, http.StatusOK, FTPTestResult{
			Success: true,
			Message: "Connection successful (capability detection failed)",
		})
		return
	}

	// Save capabilities to database
	dbCaps := &models.FTPCapabilities{
		TLSEnabled:       caps.TLSEnabled,
		PassiveModeWorks: caps.PassiveModeWorks,
		FXPSupported:     caps.FXPSupported,
	}
	if err := h.store.UpdateCapabilities(r.Context(), conn.ID, dbCaps); err != nil {
		log.Warn().Err(err).Int64("connID", conn.ID).Msg("ftp: failed to save capabilities")
	}

	// Build message
	message := "Connection successful"
	if cfg.UseTLS {
		message += " • TLS enabled"
	}
	if caps.PassiveModeWorks {
		message += " • Passive mode working"
	}

	RespondJSON(w, http.StatusOK, FTPTestResult{
		Success:      true,
		Message:      message,
		Capabilities: caps,
	})
}

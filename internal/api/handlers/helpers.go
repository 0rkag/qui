// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	internalqbittorrent "github.com/autobrr/qui/internal/qbittorrent"
)

// ErrorResponse represents an API error response
type ErrorResponse struct {
	Error string `json:"error"`
}

// RespondJSON sends a JSON response.
// For 204 No Content and 304 Not Modified, no body or Content-Type is sent per HTTP spec.
func RespondJSON(w http.ResponseWriter, status int, data any) {
	// 204 and 304 must not have a body per RFC 7230/9110
	if status == http.StatusNoContent || status == http.StatusNotModified {
		w.WriteHeader(status)
		return
	}

	if data != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if err := json.NewEncoder(w).Encode(data); err != nil {
			log.Error().Err(err).Msg("Failed to encode JSON response")
		}
		return
	}

	w.WriteHeader(status)
}

// RespondError sends an error response
func RespondError(w http.ResponseWriter, status int, message string) {
	RespondJSON(w, status, ErrorResponse{
		Error: message,
	})
}

func respondIfInstanceDisabled(w http.ResponseWriter, err error, instanceID int, context string) bool {
	if errors.Is(err, internalqbittorrent.ErrInstanceDisabled) {
		log.Trace().
			Int("instanceID", instanceID).
			Str("context", context).
			Msg("Ignoring request for disabled instance")
		RespondError(w, http.StatusConflict, "Instance is disabled")
		return true
	}

	return false
}

// parseIntParam extracts and validates a named int URL parameter.
// Returns the parsed ID or writes an error response and returns 0, false.
func parseIntParam(w http.ResponseWriter, r *http.Request, name string, errorMsg string) (int, bool) {
	id, err := strconv.Atoi(chi.URLParam(r, name))
	if err != nil {
		RespondError(w, http.StatusBadRequest, errorMsg)
		return 0, false
	}
	return id, true
}

// parseInt64Param extracts and validates a named int64 URL parameter.
// Returns the parsed ID or writes an error response and returns 0, false.
func parseInt64Param(w http.ResponseWriter, r *http.Request, name string, errorMsg string) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, name), 10, 64)
	if err != nil {
		RespondError(w, http.StatusBadRequest, errorMsg)
		return 0, false
	}
	return id, true
}

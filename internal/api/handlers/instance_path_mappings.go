// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
)

// InstancePathMappingsHandler handles instance path mapping API endpoints.
type InstancePathMappingsHandler struct {
	store    *models.InstancePathMappingStore
	resolver *models.PathResolver
}

// NewInstancePathMappingsHandler creates a new InstancePathMappingsHandler.
func NewInstancePathMappingsHandler(store *models.InstancePathMappingStore) *InstancePathMappingsHandler {
	return &InstancePathMappingsHandler{
		store:    store,
		resolver: models.NewPathResolver(store),
	}
}

// CreatePayload is the request body for creating a path mapping.
type CreatePathMappingPayload struct {
	InstancePath  string `json:"instancePath"`
	CanonicalPath string `json:"canonicalPath"`
	Enabled       *bool  `json:"enabled,omitempty"`
	Description   string `json:"description,omitempty"`
	SortOrder     int    `json:"sortOrder,omitempty"`
}

// UpdatePayload is the request body for updating a path mapping.
type UpdatePathMappingPayload struct {
	InstancePath  string `json:"instancePath"`
	CanonicalPath string `json:"canonicalPath"`
	Enabled       *bool  `json:"enabled,omitempty"`
	Description   string `json:"description,omitempty"`
	SortOrder     int    `json:"sortOrder,omitempty"`
}

// TestPathPayload is the request body for testing path resolution.
type TestPathPayload struct {
	Path      string `json:"path"`
	Direction string `json:"direction"` // "to_canonical" or "from_canonical"
}

// TestPathResponse is the response for path testing.
type TestPathResponse struct {
	InputPath    string `json:"inputPath"`
	OutputPath   string `json:"outputPath"`
	Direction    string `json:"direction"`
	MatchedRule  string `json:"matchedRule,omitempty"`
	NoMatchFound bool   `json:"noMatchFound,omitempty"`
}

// ReorderPayload is the request body for reordering mappings.
type ReorderPayload struct {
	Orders map[int64]int `json:"orders"` // mapping ID -> sort order
}

// List handles GET /api/instances/{instanceID}/path-mappings
func (h *InstancePathMappingsHandler) List(w http.ResponseWriter, r *http.Request) {
	instanceID, ok := parseIntParam(w, r, "instanceID", "Invalid instance ID")
	if !ok {
		return
	}

	mappings, err := h.store.ListByInstance(r.Context(), instanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("path-mappings: failed to list mappings")
		RespondError(w, http.StatusInternalServerError, "Failed to list path mappings")
		return
	}

	if mappings == nil {
		mappings = []*models.InstancePathMapping{}
	}

	RespondJSON(w, http.StatusOK, mappings)
}

// Create handles POST /api/instances/{instanceID}/path-mappings
func (h *InstancePathMappingsHandler) Create(w http.ResponseWriter, r *http.Request) {
	instanceID, ok := parseIntParam(w, r, "instanceID", "Invalid instance ID")
	if !ok {
		return
	}

	var payload CreatePathMappingPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		log.Warn().Err(err).Msg("path-mappings: failed to decode create payload")
		RespondError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	enabled := true
	if payload.Enabled != nil {
		enabled = *payload.Enabled
	}

	mapping := &models.InstancePathMapping{
		InstanceID:    instanceID,
		InstancePath:  payload.InstancePath,
		CanonicalPath: payload.CanonicalPath,
		Enabled:       enabled,
		Description:   payload.Description,
		SortOrder:     payload.SortOrder,
	}

	created, err := h.store.Create(r.Context(), mapping)
	if err != nil {
		if errors.Is(err, models.ErrDuplicateInstancePath) {
			RespondError(w, http.StatusConflict, "Instance path already exists for this instance")
			return
		}
		log.Error().Err(err).Int("instanceID", instanceID).Msg("path-mappings: failed to create mapping")
		RespondError(w, http.StatusInternalServerError, "Failed to create path mapping")
		return
	}

	RespondJSON(w, http.StatusCreated, created)
}

// Get handles GET /api/instances/{instanceID}/path-mappings/{id}
func (h *InstancePathMappingsHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, ok := parseInt64Param(w, r, "id", "Invalid mapping ID")
	if !ok {
		return
	}

	mapping, err := h.store.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, models.ErrPathMappingNotFound) {
			RespondError(w, http.StatusNotFound, "Path mapping not found")
			return
		}
		log.Error().Err(err).Int64("id", id).Msg("path-mappings: failed to get mapping")
		RespondError(w, http.StatusInternalServerError, "Failed to get path mapping")
		return
	}

	RespondJSON(w, http.StatusOK, mapping)
}

// Update handles PUT /api/instances/{instanceID}/path-mappings/{id}
func (h *InstancePathMappingsHandler) Update(w http.ResponseWriter, r *http.Request) {
	instanceID, ok := parseIntParam(w, r, "instanceID", "Invalid instance ID")
	if !ok {
		return
	}

	id, ok := parseInt64Param(w, r, "id", "Invalid mapping ID")
	if !ok {
		return
	}

	var payload UpdatePathMappingPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		log.Warn().Err(err).Msg("path-mappings: failed to decode update payload")
		RespondError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	// Get existing mapping to preserve fields
	existing, err := h.store.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, models.ErrPathMappingNotFound) {
			RespondError(w, http.StatusNotFound, "Path mapping not found")
			return
		}
		log.Error().Err(err).Int64("id", id).Msg("path-mappings: failed to get existing mapping")
		RespondError(w, http.StatusInternalServerError, "Failed to get path mapping")
		return
	}

	// Verify the mapping belongs to the specified instance
	if existing.InstanceID != instanceID {
		RespondError(w, http.StatusNotFound, "Path mapping not found")
		return
	}

	// Update fields
	existing.InstancePath = payload.InstancePath
	existing.CanonicalPath = payload.CanonicalPath
	existing.Description = payload.Description
	existing.SortOrder = payload.SortOrder
	if payload.Enabled != nil {
		existing.Enabled = *payload.Enabled
	}

	if err := h.store.Update(r.Context(), existing); err != nil {
		if errors.Is(err, models.ErrDuplicateInstancePath) {
			RespondError(w, http.StatusConflict, "Instance path already exists for this instance")
			return
		}
		if errors.Is(err, models.ErrPathMappingNotFound) {
			RespondError(w, http.StatusNotFound, "Path mapping not found")
			return
		}
		log.Error().Err(err).Int64("id", id).Msg("path-mappings: failed to update mapping")
		RespondError(w, http.StatusInternalServerError, "Failed to update path mapping")
		return
	}

	RespondJSON(w, http.StatusOK, existing)
}

// Delete handles DELETE /api/instances/{instanceID}/path-mappings/{id}
func (h *InstancePathMappingsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	instanceID, ok := parseIntParam(w, r, "instanceID", "Invalid instance ID")
	if !ok {
		return
	}

	id, ok := parseInt64Param(w, r, "id", "Invalid mapping ID")
	if !ok {
		return
	}

	// Verify the mapping belongs to the specified instance
	existing, err := h.store.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, models.ErrPathMappingNotFound) {
			RespondError(w, http.StatusNotFound, "Path mapping not found")
			return
		}
		log.Error().Err(err).Int64("id", id).Msg("path-mappings: failed to get mapping for delete")
		RespondError(w, http.StatusInternalServerError, "Failed to get path mapping")
		return
	}

	if existing.InstanceID != instanceID {
		RespondError(w, http.StatusNotFound, "Path mapping not found")
		return
	}

	if err := h.store.Delete(r.Context(), id); err != nil {
		if errors.Is(err, models.ErrPathMappingNotFound) {
			RespondError(w, http.StatusNotFound, "Path mapping not found")
			return
		}
		log.Error().Err(err).Int64("id", id).Msg("path-mappings: failed to delete mapping")
		RespondError(w, http.StatusInternalServerError, "Failed to delete path mapping")
		return
	}

	RespondJSON(w, http.StatusNoContent, nil)
}

// Reorder handles PUT /api/instances/{instanceID}/path-mappings/reorder
func (h *InstancePathMappingsHandler) Reorder(w http.ResponseWriter, r *http.Request) {
	instanceID, ok := parseIntParam(w, r, "instanceID", "Invalid instance ID")
	if !ok {
		return
	}

	var payload ReorderPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		log.Warn().Err(err).Msg("path-mappings: failed to decode reorder payload")
		RespondError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	if len(payload.Orders) == 0 {
		RespondError(w, http.StatusBadRequest, "No orders provided")
		return
	}

	// Verify all mappings belong to the specified instance
	for id := range payload.Orders {
		mapping, err := h.store.Get(r.Context(), id)
		if err != nil {
			if errors.Is(err, models.ErrPathMappingNotFound) {
				RespondError(w, http.StatusNotFound, "Path mapping not found")
				return
			}
			log.Error().Err(err).Int64("id", id).Msg("path-mappings: failed to verify mapping for reorder")
			RespondError(w, http.StatusInternalServerError, "Failed to verify path mapping")
			return
		}
		if mapping.InstanceID != instanceID {
			RespondError(w, http.StatusBadRequest, "Mapping does not belong to this instance")
			return
		}
	}

	if err := h.store.UpdateSortOrder(r.Context(), payload.Orders); err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Msg("path-mappings: failed to reorder mappings")
		RespondError(w, http.StatusInternalServerError, "Failed to reorder path mappings")
		return
	}

	RespondJSON(w, http.StatusNoContent, nil)
}

// TestPath handles POST /api/instances/{instanceID}/path-mappings/test
func (h *InstancePathMappingsHandler) TestPath(w http.ResponseWriter, r *http.Request) {
	instanceID, ok := parseIntParam(w, r, "instanceID", "Invalid instance ID")
	if !ok {
		return
	}

	var payload TestPathPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		log.Warn().Err(err).Msg("path-mappings: failed to decode test payload")
		RespondError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	if payload.Path == "" {
		RespondError(w, http.StatusBadRequest, "Path is required")
		return
	}

	if payload.Direction == "" {
		payload.Direction = "to_canonical"
	}

	var outputPath string
	var err error
	switch payload.Direction {
	case "to_canonical":
		outputPath, err = h.resolver.ToCanonicalPath(r.Context(), instanceID, payload.Path)
	case "from_canonical":
		outputPath, err = h.resolver.FromCanonicalPath(r.Context(), instanceID, payload.Path)
	default:
		RespondError(w, http.StatusBadRequest, "Direction must be 'to_canonical' or 'from_canonical'")
		return
	}

	if err != nil {
		log.Error().Err(err).Int("instanceID", instanceID).Str("path", payload.Path).Msg("path-mappings: failed to test path")
		RespondError(w, http.StatusInternalServerError, "Failed to test path")
		return
	}

	response := TestPathResponse{
		InputPath:    payload.Path,
		OutputPath:   outputPath,
		Direction:    payload.Direction,
		NoMatchFound: outputPath == payload.Path, // If unchanged, no mapping matched
	}

	RespondJSON(w, http.StatusOK, response)
}

// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
)

func setupPathMappingHandler(t *testing.T) (*InstancePathMappingsHandler, *database.DB, func()) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := database.New(dbPath)
	require.NoError(t, err)

	store := models.NewInstancePathMappingStore(db)
	handler := NewInstancePathMappingsHandler(store)

	cleanup := func() {
		require.NoError(t, db.Close())
	}

	return handler, db, cleanup
}

func createTestInstance(t *testing.T, db *database.DB) int {
	t.Helper()
	ctx := context.Background()

	result, err := db.ExecContext(ctx, "INSERT INTO instances (name_id, host_id, username_id, password_encrypted) VALUES (1, 1, 1, 'pass')")
	require.NoError(t, err)
	id, err := result.LastInsertId()
	require.NoError(t, err)

	return int(id)
}

func createTestMapping(t *testing.T, db *database.DB, instanceID int, instancePath, canonicalPath string) *models.InstancePathMapping {
	t.Helper()
	ctx := context.Background()

	store := models.NewInstancePathMappingStore(db)
	mapping := &models.InstancePathMapping{
		InstanceID:    instanceID,
		InstancePath:  instancePath,
		CanonicalPath: canonicalPath,
		Enabled:       true,
		SortOrder:     0,
	}
	created, err := store.Create(ctx, mapping)
	require.NoError(t, err)

	return created
}

func newPathMappingRequest(method, path string, params map[string]string, body interface{}) *http.Request {
	var reqBody *bytes.Buffer
	if body != nil {
		data, _ := json.Marshal(body)
		reqBody = bytes.NewBuffer(data)
	} else {
		reqBody = bytes.NewBuffer(nil)
	}

	req := httptest.NewRequest(method, path, reqBody)
	req.Header.Set("Content-Type", "application/json")

	ctx := chi.NewRouteContext()
	for key, value := range params {
		ctx.URLParams.Add(key, value)
	}
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, ctx))
}

// List tests
func TestList_InvalidInstanceID(t *testing.T) {
	handler, _, cleanup := setupPathMappingHandler(t)
	defer cleanup()

	req := newPathMappingRequest(http.MethodGet, "/api/instances/invalid/path-mappings", map[string]string{
		"instanceID": "invalid",
	}, nil)
	w := httptest.NewRecorder()

	handler.List(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Invalid instance ID")
}

func TestList_Empty(t *testing.T) {
	handler, db, cleanup := setupPathMappingHandler(t)
	defer cleanup()

	instanceID := createTestInstance(t, db)

	req := newPathMappingRequest(http.MethodGet, "/api/instances/1/path-mappings", map[string]string{
		"instanceID": "1",
	}, nil)
	_ = instanceID // Instance exists but no mappings
	w := httptest.NewRecorder()

	handler.List(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, "[]", w.Body.String())
}

func TestList_Success(t *testing.T) {
	handler, db, cleanup := setupPathMappingHandler(t)
	defer cleanup()

	instanceID := createTestInstance(t, db)
	createTestMapping(t, db, instanceID, "/downloads", "/data/media")
	createTestMapping(t, db, instanceID, "/uploads", "/data/uploads")

	req := newPathMappingRequest(http.MethodGet, "/api/instances/1/path-mappings", map[string]string{
		"instanceID": "1",
	}, nil)
	w := httptest.NewRecorder()

	handler.List(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var mappings []*models.InstancePathMapping
	err := json.Unmarshal(w.Body.Bytes(), &mappings)
	require.NoError(t, err)
	assert.Len(t, mappings, 2)
}

// Create tests
func TestCreate_InvalidInstanceID(t *testing.T) {
	handler, _, cleanup := setupPathMappingHandler(t)
	defer cleanup()

	req := newPathMappingRequest(http.MethodPost, "/api/instances/invalid/path-mappings", map[string]string{
		"instanceID": "invalid",
	}, CreatePathMappingPayload{
		InstancePath:  "/downloads",
		CanonicalPath: "/data/media",
	})
	w := httptest.NewRecorder()

	handler.Create(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Invalid instance ID")
}

func TestCreate_InvalidPayload(t *testing.T) {
	handler, db, cleanup := setupPathMappingHandler(t)
	defer cleanup()

	_ = createTestInstance(t, db)

	req := httptest.NewRequest(http.MethodPost, "/api/instances/1/path-mappings", bytes.NewBufferString("invalid json"))
	req.Header.Set("Content-Type", "application/json")
	ctx := chi.NewRouteContext()
	ctx.URLParams.Add("instanceID", "1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, ctx))
	w := httptest.NewRecorder()

	handler.Create(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Invalid request body")
}

func TestCreate_Success(t *testing.T) {
	handler, db, cleanup := setupPathMappingHandler(t)
	defer cleanup()

	instanceID := createTestInstance(t, db)

	payload := CreatePathMappingPayload{
		InstancePath:  "/downloads",
		CanonicalPath: "/data/media",
		Description:   "Test mapping",
	}

	req := newPathMappingRequest(http.MethodPost, "/api/instances/1/path-mappings", map[string]string{
		"instanceID": "1",
	}, payload)
	_ = instanceID
	w := httptest.NewRecorder()

	handler.Create(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var created models.InstancePathMapping
	err := json.Unmarshal(w.Body.Bytes(), &created)
	require.NoError(t, err)
	assert.Equal(t, "/downloads", created.InstancePath)
	assert.Equal(t, "/data/media", created.CanonicalPath)
	assert.True(t, created.Enabled)
}

func TestCreate_DuplicatePath(t *testing.T) {
	handler, db, cleanup := setupPathMappingHandler(t)
	defer cleanup()

	instanceID := createTestInstance(t, db)
	createTestMapping(t, db, instanceID, "/downloads", "/data/media")

	payload := CreatePathMappingPayload{
		InstancePath:  "/downloads", // Duplicate
		CanonicalPath: "/other/path",
	}

	req := newPathMappingRequest(http.MethodPost, "/api/instances/1/path-mappings", map[string]string{
		"instanceID": "1",
	}, payload)
	w := httptest.NewRecorder()

	handler.Create(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Contains(t, w.Body.String(), "Instance path already exists")
}

// Get tests
func TestGet_InvalidMappingID(t *testing.T) {
	handler, _, cleanup := setupPathMappingHandler(t)
	defer cleanup()

	req := newPathMappingRequest(http.MethodGet, "/api/instances/1/path-mappings/invalid", map[string]string{
		"instanceID": "1",
		"id":         "invalid",
	}, nil)
	w := httptest.NewRecorder()

	handler.Get(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Invalid mapping ID")
}

func TestGet_NotFound(t *testing.T) {
	handler, _, cleanup := setupPathMappingHandler(t)
	defer cleanup()

	req := newPathMappingRequest(http.MethodGet, "/api/instances/1/path-mappings/999", map[string]string{
		"instanceID": "1",
		"id":         "999",
	}, nil)
	w := httptest.NewRecorder()

	handler.Get(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "Path mapping not found")
}

func TestGet_Success(t *testing.T) {
	handler, db, cleanup := setupPathMappingHandler(t)
	defer cleanup()

	instanceID := createTestInstance(t, db)
	mapping := createTestMapping(t, db, instanceID, "/downloads", "/data/media")

	req := newPathMappingRequest(http.MethodGet, "/api/instances/1/path-mappings/1", map[string]string{
		"instanceID": "1",
		"id":         "1",
	}, nil)
	_ = mapping
	w := httptest.NewRecorder()

	handler.Get(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var result models.InstancePathMapping
	err := json.Unmarshal(w.Body.Bytes(), &result)
	require.NoError(t, err)
	assert.Equal(t, "/downloads", result.InstancePath)
}

// Update tests
func TestUpdate_InvalidInstanceID(t *testing.T) {
	handler, _, cleanup := setupPathMappingHandler(t)
	defer cleanup()

	req := newPathMappingRequest(http.MethodPut, "/api/instances/invalid/path-mappings/1", map[string]string{
		"instanceID": "invalid",
		"id":         "1",
	}, UpdatePathMappingPayload{
		InstancePath:  "/new/path",
		CanonicalPath: "/new/canonical",
	})
	w := httptest.NewRecorder()

	handler.Update(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Invalid instance ID")
}

func TestUpdate_InvalidMappingID(t *testing.T) {
	handler, _, cleanup := setupPathMappingHandler(t)
	defer cleanup()

	req := newPathMappingRequest(http.MethodPut, "/api/instances/1/path-mappings/invalid", map[string]string{
		"instanceID": "1",
		"id":         "invalid",
	}, UpdatePathMappingPayload{
		InstancePath:  "/new/path",
		CanonicalPath: "/new/canonical",
	})
	w := httptest.NewRecorder()

	handler.Update(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Invalid mapping ID")
}

func TestUpdate_NotFound(t *testing.T) {
	handler, db, cleanup := setupPathMappingHandler(t)
	defer cleanup()

	_ = createTestInstance(t, db)

	req := newPathMappingRequest(http.MethodPut, "/api/instances/1/path-mappings/999", map[string]string{
		"instanceID": "1",
		"id":         "999",
	}, UpdatePathMappingPayload{
		InstancePath:  "/new/path",
		CanonicalPath: "/new/canonical",
	})
	w := httptest.NewRecorder()

	handler.Update(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestUpdate_WrongInstance(t *testing.T) {
	handler, db, cleanup := setupPathMappingHandler(t)
	defer cleanup()

	instanceID := createTestInstance(t, db)
	mapping := createTestMapping(t, db, instanceID, "/downloads", "/data/media")

	// Try to update with a different instance ID
	req := newPathMappingRequest(http.MethodPut, "/api/instances/999/path-mappings/1", map[string]string{
		"instanceID": "999", // Wrong instance
		"id":         "1",
	}, UpdatePathMappingPayload{
		InstancePath:  "/new/path",
		CanonicalPath: "/new/canonical",
	})
	_ = mapping
	w := httptest.NewRecorder()

	handler.Update(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "Path mapping not found")
}

func TestUpdate_Success(t *testing.T) {
	handler, db, cleanup := setupPathMappingHandler(t)
	defer cleanup()

	instanceID := createTestInstance(t, db)
	mapping := createTestMapping(t, db, instanceID, "/downloads", "/data/media")

	enabled := false
	req := newPathMappingRequest(http.MethodPut, "/api/instances/1/path-mappings/1", map[string]string{
		"instanceID": "1",
		"id":         "1",
	}, UpdatePathMappingPayload{
		InstancePath:  "/new/downloads",
		CanonicalPath: "/new/media",
		Enabled:       &enabled,
		Description:   "Updated description",
	})
	_ = mapping
	w := httptest.NewRecorder()

	handler.Update(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var result models.InstancePathMapping
	err := json.Unmarshal(w.Body.Bytes(), &result)
	require.NoError(t, err)
	assert.Equal(t, "/new/downloads", result.InstancePath)
	assert.Equal(t, "/new/media", result.CanonicalPath)
	assert.False(t, result.Enabled)
	assert.Equal(t, "Updated description", result.Description)
}

// Delete tests
func TestDelete_InvalidInstanceID(t *testing.T) {
	handler, _, cleanup := setupPathMappingHandler(t)
	defer cleanup()

	req := newPathMappingRequest(http.MethodDelete, "/api/instances/invalid/path-mappings/1", map[string]string{
		"instanceID": "invalid",
		"id":         "1",
	}, nil)
	w := httptest.NewRecorder()

	handler.Delete(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Invalid instance ID")
}

func TestDelete_InvalidMappingID(t *testing.T) {
	handler, _, cleanup := setupPathMappingHandler(t)
	defer cleanup()

	req := newPathMappingRequest(http.MethodDelete, "/api/instances/1/path-mappings/invalid", map[string]string{
		"instanceID": "1",
		"id":         "invalid",
	}, nil)
	w := httptest.NewRecorder()

	handler.Delete(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Invalid mapping ID")
}

func TestDelete_NotFound(t *testing.T) {
	handler, _, cleanup := setupPathMappingHandler(t)
	defer cleanup()

	req := newPathMappingRequest(http.MethodDelete, "/api/instances/1/path-mappings/999", map[string]string{
		"instanceID": "1",
		"id":         "999",
	}, nil)
	w := httptest.NewRecorder()

	handler.Delete(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestDelete_WrongInstance(t *testing.T) {
	handler, db, cleanup := setupPathMappingHandler(t)
	defer cleanup()

	instanceID := createTestInstance(t, db)
	mapping := createTestMapping(t, db, instanceID, "/downloads", "/data/media")

	// Try to delete with wrong instance ID
	req := newPathMappingRequest(http.MethodDelete, "/api/instances/999/path-mappings/1", map[string]string{
		"instanceID": "999",
		"id":         "1",
	}, nil)
	_ = mapping
	w := httptest.NewRecorder()

	handler.Delete(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestDelete_Success(t *testing.T) {
	handler, db, cleanup := setupPathMappingHandler(t)
	defer cleanup()

	instanceID := createTestInstance(t, db)
	mapping := createTestMapping(t, db, instanceID, "/downloads", "/data/media")

	req := newPathMappingRequest(http.MethodDelete, "/api/instances/1/path-mappings/1", map[string]string{
		"instanceID": "1",
		"id":         "1",
	}, nil)
	_ = mapping
	w := httptest.NewRecorder()

	handler.Delete(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
}

// Reorder tests
func TestReorder_InvalidInstanceID(t *testing.T) {
	handler, _, cleanup := setupPathMappingHandler(t)
	defer cleanup()

	req := newPathMappingRequest(http.MethodPut, "/api/instances/invalid/path-mappings/reorder", map[string]string{
		"instanceID": "invalid",
	}, ReorderPayload{
		Orders: map[int64]int{1: 0, 2: 1},
	})
	w := httptest.NewRecorder()

	handler.Reorder(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Invalid instance ID")
}

func TestReorder_EmptyOrders(t *testing.T) {
	handler, db, cleanup := setupPathMappingHandler(t)
	defer cleanup()

	_ = createTestInstance(t, db)

	req := newPathMappingRequest(http.MethodPut, "/api/instances/1/path-mappings/reorder", map[string]string{
		"instanceID": "1",
	}, ReorderPayload{
		Orders: map[int64]int{},
	})
	w := httptest.NewRecorder()

	handler.Reorder(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "No orders provided")
}

func TestReorder_MappingNotFound(t *testing.T) {
	handler, db, cleanup := setupPathMappingHandler(t)
	defer cleanup()

	_ = createTestInstance(t, db)

	req := newPathMappingRequest(http.MethodPut, "/api/instances/1/path-mappings/reorder", map[string]string{
		"instanceID": "1",
	}, ReorderPayload{
		Orders: map[int64]int{999: 0}, // Non-existent mapping
	})
	w := httptest.NewRecorder()

	handler.Reorder(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestReorder_WrongInstance(t *testing.T) {
	handler, db, cleanup := setupPathMappingHandler(t)
	defer cleanup()

	instanceID := createTestInstance(t, db)
	mapping := createTestMapping(t, db, instanceID, "/downloads", "/data/media")

	// Try to reorder with wrong instance ID
	req := newPathMappingRequest(http.MethodPut, "/api/instances/999/path-mappings/reorder", map[string]string{
		"instanceID": "999",
	}, ReorderPayload{
		Orders: map[int64]int{mapping.ID: 0},
	})
	w := httptest.NewRecorder()

	handler.Reorder(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Mapping does not belong to this instance")
}

func TestReorder_Success(t *testing.T) {
	handler, db, cleanup := setupPathMappingHandler(t)
	defer cleanup()

	instanceID := createTestInstance(t, db)
	mapping1 := createTestMapping(t, db, instanceID, "/downloads", "/data/media")
	mapping2 := createTestMapping(t, db, instanceID, "/uploads", "/data/uploads")

	req := newPathMappingRequest(http.MethodPut, "/api/instances/1/path-mappings/reorder", map[string]string{
		"instanceID": "1",
	}, ReorderPayload{
		Orders: map[int64]int{
			mapping1.ID: 1, // Swap order
			mapping2.ID: 0,
		},
	})
	w := httptest.NewRecorder()

	handler.Reorder(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
}

// TestPath tests
func TestTestPath_InvalidInstanceID(t *testing.T) {
	handler, _, cleanup := setupPathMappingHandler(t)
	defer cleanup()

	req := newPathMappingRequest(http.MethodPost, "/api/instances/invalid/path-mappings/test", map[string]string{
		"instanceID": "invalid",
	}, TestPathPayload{
		Path:      "/downloads/movies",
		Direction: "to_canonical",
	})
	w := httptest.NewRecorder()

	handler.TestPath(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Invalid instance ID")
}

func TestTestPath_EmptyPath(t *testing.T) {
	handler, db, cleanup := setupPathMappingHandler(t)
	defer cleanup()

	_ = createTestInstance(t, db)

	req := newPathMappingRequest(http.MethodPost, "/api/instances/1/path-mappings/test", map[string]string{
		"instanceID": "1",
	}, TestPathPayload{
		Path:      "",
		Direction: "to_canonical",
	})
	w := httptest.NewRecorder()

	handler.TestPath(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Path is required")
}

func TestTestPath_InvalidDirection(t *testing.T) {
	handler, db, cleanup := setupPathMappingHandler(t)
	defer cleanup()

	_ = createTestInstance(t, db)

	req := newPathMappingRequest(http.MethodPost, "/api/instances/1/path-mappings/test", map[string]string{
		"instanceID": "1",
	}, TestPathPayload{
		Path:      "/downloads/movies",
		Direction: "invalid",
	})
	w := httptest.NewRecorder()

	handler.TestPath(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Direction must be")
}

func TestTestPath_ToCanonical_NoMatch(t *testing.T) {
	handler, db, cleanup := setupPathMappingHandler(t)
	defer cleanup()

	instanceID := createTestInstance(t, db)
	createTestMapping(t, db, instanceID, "/downloads", "/data/media")

	req := newPathMappingRequest(http.MethodPost, "/api/instances/1/path-mappings/test", map[string]string{
		"instanceID": "1",
	}, TestPathPayload{
		Path:      "/uploads/photos", // No matching mapping
		Direction: "to_canonical",
	})
	w := httptest.NewRecorder()

	handler.TestPath(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var result TestPathResponse
	err := json.Unmarshal(w.Body.Bytes(), &result)
	require.NoError(t, err)
	assert.Equal(t, "/uploads/photos", result.InputPath)
	assert.Equal(t, "/uploads/photos", result.OutputPath)
	assert.True(t, result.NoMatchFound)
}

func TestTestPath_ToCanonical_Success(t *testing.T) {
	handler, db, cleanup := setupPathMappingHandler(t)
	defer cleanup()

	instanceID := createTestInstance(t, db)
	createTestMapping(t, db, instanceID, "/downloads", "/data/media")

	req := newPathMappingRequest(http.MethodPost, "/api/instances/1/path-mappings/test", map[string]string{
		"instanceID": "1",
	}, TestPathPayload{
		Path:      "/downloads/movies/action",
		Direction: "to_canonical",
	})
	w := httptest.NewRecorder()

	handler.TestPath(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var result TestPathResponse
	err := json.Unmarshal(w.Body.Bytes(), &result)
	require.NoError(t, err)
	assert.Equal(t, "/downloads/movies/action", result.InputPath)
	assert.Equal(t, "/data/media/movies/action", result.OutputPath)
	assert.Equal(t, "to_canonical", result.Direction)
	assert.False(t, result.NoMatchFound)
}

func TestTestPath_FromCanonical_Success(t *testing.T) {
	handler, db, cleanup := setupPathMappingHandler(t)
	defer cleanup()

	instanceID := createTestInstance(t, db)
	createTestMapping(t, db, instanceID, "/downloads", "/data/media")

	req := newPathMappingRequest(http.MethodPost, "/api/instances/1/path-mappings/test", map[string]string{
		"instanceID": "1",
	}, TestPathPayload{
		Path:      "/data/media/movies/action",
		Direction: "from_canonical",
	})
	w := httptest.NewRecorder()

	handler.TestPath(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var result TestPathResponse
	err := json.Unmarshal(w.Body.Bytes(), &result)
	require.NoError(t, err)
	assert.Equal(t, "/data/media/movies/action", result.InputPath)
	assert.Equal(t, "/downloads/movies/action", result.OutputPath)
	assert.Equal(t, "from_canonical", result.Direction)
	assert.False(t, result.NoMatchFound)
}

func TestTestPath_DefaultDirection(t *testing.T) {
	handler, db, cleanup := setupPathMappingHandler(t)
	defer cleanup()

	instanceID := createTestInstance(t, db)
	createTestMapping(t, db, instanceID, "/downloads", "/data/media")

	// Empty direction should default to "to_canonical"
	req := newPathMappingRequest(http.MethodPost, "/api/instances/1/path-mappings/test", map[string]string{
		"instanceID": "1",
	}, TestPathPayload{
		Path: "/downloads/movies",
		// Direction not set
	})
	w := httptest.NewRecorder()

	handler.TestPath(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var result TestPathResponse
	err := json.Unmarshal(w.Body.Bytes(), &result)
	require.NoError(t, err)
	assert.Equal(t, "to_canonical", result.Direction)
	assert.Equal(t, "/data/media/movies", result.OutputPath)
}

package client

import (
	"fmt"
	"net/http"
	"testing"
)

// ListTrackerCustomizations returns all tracker customizations.
func (c *Client) ListTrackerCustomizations(t *testing.T) []TrackerCustomization {
	t.Helper()

	resp := c.get(t, "/api/tracker-customizations/")
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusOK)

	var result []TrackerCustomization
	decodeJSON(t, resp.Body, &result)
	return result
}

// CreateTrackerCustomization creates a tracker customization and returns it.
func (c *Client) CreateTrackerCustomization(t *testing.T, payload TrackerCustomizationPayload) TrackerCustomization {
	t.Helper()

	resp := c.post(t, "/api/tracker-customizations/", payload)
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusCreated)

	var result TrackerCustomization
	decodeJSON(t, resp.Body, &result)
	return result
}

// CreateTrackerCustomizationRaw creates a tracker customization and returns the raw response.
func (c *Client) CreateTrackerCustomizationRaw(t *testing.T, payload TrackerCustomizationPayload) *http.Response {
	t.Helper()
	return c.post(t, "/api/tracker-customizations/", payload)
}

// UpdateTrackerCustomization updates a tracker customization by ID.
func (c *Client) UpdateTrackerCustomization(t *testing.T, id int, payload TrackerCustomizationPayload) TrackerCustomization {
	t.Helper()

	resp := c.put(t, fmt.Sprintf("/api/tracker-customizations/%d", id), payload)
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusOK)

	var result TrackerCustomization
	decodeJSON(t, resp.Body, &result)
	return result
}

// DeleteTrackerCustomization deletes a tracker customization by ID.
func (c *Client) DeleteTrackerCustomization(t *testing.T, id int) {
	t.Helper()

	resp := c.delete(t, fmt.Sprintf("/api/tracker-customizations/%d", id))
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusNoContent)
}

// TrackerCustomization represents a tracker customization.
type TrackerCustomization struct {
	ID          int      `json:"id"`
	DisplayName string   `json:"displayName"`
	Domains     []string `json:"domains"`
	CreatedAt   string   `json:"createdAt"`
	UpdatedAt   string   `json:"updatedAt"`
}

// TrackerCustomizationPayload is the request body for creating/updating tracker customizations.
type TrackerCustomizationPayload struct {
	DisplayName string   `json:"displayName"`
	Domains     []string `json:"domains"`
}

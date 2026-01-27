package client

import (
	"fmt"
	"net/http"
	"testing"
)

// GetPreferences returns the qBittorrent preferences for an instance.
func (c *Client) GetPreferences(t *testing.T, instanceID int) map[string]any {
	t.Helper()

	url := fmt.Sprintf("/api/instances/%d/preferences", instanceID)
	resp := c.get(t, url)
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusOK)

	var result map[string]any
	decodeJSON(t, resp.Body, &result)
	return result
}

// UpdatePreferences patches qBittorrent preferences for an instance.
func (c *Client) UpdatePreferences(t *testing.T, instanceID int, prefs map[string]any) {
	t.Helper()

	url := fmt.Sprintf("/api/instances/%d/preferences", instanceID)
	resp := c.patch(t, url, prefs)
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusOK)
}

// GetAltSpeedMode returns the current alternative speed limits mode.
func (c *Client) GetAltSpeedMode(t *testing.T, instanceID int) AltSpeedResponse {
	t.Helper()

	url := fmt.Sprintf("/api/instances/%d/alternative-speed-limits", instanceID)
	resp := c.get(t, url)
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusOK)

	var result AltSpeedResponse
	decodeJSON(t, resp.Body, &result)
	return result
}

// ToggleAltSpeed toggles the alternative speed limits and returns the new state.
func (c *Client) ToggleAltSpeed(t *testing.T, instanceID int) AltSpeedResponse {
	t.Helper()

	url := fmt.Sprintf("/api/instances/%d/alternative-speed-limits/toggle", instanceID)
	resp := c.post(t, url, nil)
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusOK)

	var result AltSpeedResponse
	decodeJSON(t, resp.Body, &result)
	return result
}

// AltSpeedResponse is the response from alt speed endpoints.
type AltSpeedResponse struct {
	Enabled bool `json:"enabled"`
}

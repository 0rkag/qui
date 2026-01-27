package client

import (
	"net/http"
	"testing"
)

// GetDashboardSettings returns the current user's dashboard settings.
func (c *Client) GetDashboardSettings(t *testing.T) DashboardSettings {
	t.Helper()

	resp := c.get(t, "/api/dashboard-settings")
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusOK)

	var result DashboardSettings
	decodeJSON(t, resp.Body, &result)
	return result
}

// UpdateDashboardSettings updates the current user's dashboard settings.
func (c *Client) UpdateDashboardSettings(t *testing.T, settings DashboardSettingsInput) DashboardSettings {
	t.Helper()

	resp := c.put(t, "/api/dashboard-settings", settings)
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusOK)

	var result DashboardSettings
	decodeJSON(t, resp.Body, &result)
	return result
}

// DashboardSettings is the response from dashboard settings endpoints.
type DashboardSettings struct {
	ID                           int             `json:"id"`
	UserID                       int             `json:"userId"`
	SectionVisibility            map[string]bool `json:"sectionVisibility"`
	SectionOrder                 []string        `json:"sectionOrder"`
	SectionCollapsed             map[string]bool `json:"sectionCollapsed"`
	TrackerBreakdownSortColumn   string          `json:"trackerBreakdownSortColumn"`
	TrackerBreakdownSortDir      string          `json:"trackerBreakdownSortDirection"`
	TrackerBreakdownItemsPerPage int             `json:"trackerBreakdownItemsPerPage"`
	CreatedAt                    string          `json:"createdAt"`
	UpdatedAt                    string          `json:"updatedAt"`
}

// DashboardSettingsInput is the request body for updating dashboard settings.
type DashboardSettingsInput struct {
	SectionVisibility            map[string]bool `json:"sectionVisibility,omitempty"`
	SectionOrder                 []string        `json:"sectionOrder,omitempty"`
	SectionCollapsed             map[string]bool `json:"sectionCollapsed,omitempty"`
	TrackerBreakdownSortColumn   string          `json:"trackerBreakdownSortColumn,omitempty"`
	TrackerBreakdownSortDir      string          `json:"trackerBreakdownSortDirection,omitempty"`
	TrackerBreakdownItemsPerPage int             `json:"trackerBreakdownItemsPerPage,omitempty"`
}

package client

import (
	"fmt"
	"net/http"
	"testing"
)

// ListAutomations returns all automations for an instance.
func (c *Client) ListAutomations(t *testing.T, instanceID int) []Automation {
	t.Helper()

	url := fmt.Sprintf("/api/instances/%d/automations", instanceID)
	resp := c.get(t, url)
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusOK)

	var result []Automation
	decodeJSON(t, resp.Body, &result)
	return result
}

// CreateAutomation creates an automation rule and returns it.
func (c *Client) CreateAutomation(t *testing.T, instanceID int, payload AutomationPayload) Automation {
	t.Helper()

	url := fmt.Sprintf("/api/instances/%d/automations", instanceID)
	resp := c.post(t, url, payload)
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusCreated)

	var result Automation
	decodeJSON(t, resp.Body, &result)
	return result
}

// CreateAutomationRaw creates an automation and returns the raw response for error testing.
func (c *Client) CreateAutomationRaw(t *testing.T, instanceID int, payload AutomationPayload) *http.Response {
	t.Helper()
	return c.post(t, fmt.Sprintf("/api/instances/%d/automations", instanceID), payload)
}

// UpdateAutomation updates an existing automation rule.
func (c *Client) UpdateAutomation(t *testing.T, instanceID, ruleID int, payload AutomationPayload) Automation {
	t.Helper()

	url := fmt.Sprintf("/api/instances/%d/automations/%d", instanceID, ruleID)
	resp := c.put(t, url, payload)
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusOK)

	var result Automation
	decodeJSON(t, resp.Body, &result)
	return result
}

// DeleteAutomation removes an automation rule.
func (c *Client) DeleteAutomation(t *testing.T, instanceID, ruleID int) {
	t.Helper()

	url := fmt.Sprintf("/api/instances/%d/automations/%d", instanceID, ruleID)
	resp := c.delete(t, url)
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusNoContent)
}

// ReorderAutomations updates the sort order of automations.
func (c *Client) ReorderAutomations(t *testing.T, instanceID int, orderedIDs []int) {
	t.Helper()

	body := map[string][]int{
		"orderedIds": orderedIDs,
	}

	url := fmt.Sprintf("/api/instances/%d/automations/order", instanceID)
	resp := c.put(t, url, body)
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusNoContent)
}

// ApplyAutomations triggers immediate execution of automation rules.
func (c *Client) ApplyAutomations(t *testing.T, instanceID int) {
	t.Helper()

	url := fmt.Sprintf("/api/instances/%d/automations/apply", instanceID)
	resp := c.post(t, url, nil)
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusAccepted)
}

// PreviewAutomation previews which torrents would match the given automation rule.
// The payload must have either delete or category action enabled for preview.
func (c *Client) PreviewAutomation(t *testing.T, instanceID int, payload AutomationPayload) PreviewResult {
	t.Helper()

	url := fmt.Sprintf("/api/instances/%d/automations/preview", instanceID)
	resp := c.post(t, url, payload)
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusOK)

	var result PreviewResult
	decodeJSON(t, resp.Body, &result)
	return result
}

// PreviewAutomationRaw previews automations and returns the raw response for error testing.
func (c *Client) PreviewAutomationRaw(t *testing.T, instanceID int, payload AutomationPayload) *http.Response {
	t.Helper()
	return c.post(t, fmt.Sprintf("/api/instances/%d/automations/preview", instanceID), payload)
}

// ValidateRegex validates regex patterns in automation conditions.
func (c *Client) ValidateRegex(t *testing.T, instanceID int, payload AutomationPayload) RegexValidationResult {
	t.Helper()

	url := fmt.Sprintf("/api/instances/%d/automations/validate-regex", instanceID)
	resp := c.post(t, url, payload)
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusOK)

	var result RegexValidationResult
	decodeJSON(t, resp.Body, &result)
	return result
}

// ListAutomationActivity returns recent automation activity logs.
func (c *Client) ListAutomationActivity(t *testing.T, instanceID int, limit int) []AutomationActivity {
	t.Helper()

	url := fmt.Sprintf("/api/instances/%d/automations/activity?limit=%d", instanceID, limit)
	resp := c.get(t, url)
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusOK)

	var result []AutomationActivity
	decodeJSON(t, resp.Body, &result)
	return result
}

// DeleteAutomationActivity removes old activity logs.
func (c *Client) DeleteAutomationActivity(t *testing.T, instanceID int, olderThanDays int) int64 {
	t.Helper()

	url := fmt.Sprintf("/api/instances/%d/automations/activity?older_than=%d", instanceID, olderThanDays)
	resp := c.delete(t, url)
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusOK)

	var result struct {
		Deleted int64 `json:"deleted"`
	}
	decodeJSON(t, resp.Body, &result)
	return result.Deleted
}

package client

import (
	"fmt"
	"net/http"
	"testing"
)

// GetTags returns all tags for an instance.
func (c *Client) GetTags(t *testing.T, instanceID int) []string {
	t.Helper()

	url := fmt.Sprintf("/api/instances/%d/tags", instanceID)
	resp := c.get(t, url)
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusOK)

	var result []string
	decodeJSON(t, resp.Body, &result)
	return result
}

// CreateTags creates new tags.
func (c *Client) CreateTags(t *testing.T, instanceID int, tags []string) {
	t.Helper()

	body := map[string]any{
		"tags": tags,
	}

	url := fmt.Sprintf("/api/instances/%d/tags", instanceID)
	resp := c.post(t, url, body)
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusCreated)
}

// DeleteTags removes tags.
func (c *Client) DeleteTags(t *testing.T, instanceID int, tags []string) {
	t.Helper()

	body := map[string]any{
		"tags": tags,
	}

	url := fmt.Sprintf("/api/instances/%d/tags", instanceID)
	resp := c.deleteWithBody(t, url, body)
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusOK)
}

package client

import (
	"fmt"
	"net/http"
	"testing"
)

// GetCategories returns all categories for an instance.
func (c *Client) GetCategories(t *testing.T, instanceID int) map[string]Category {
	t.Helper()

	url := fmt.Sprintf("/api/instances/%d/categories", instanceID)
	resp := c.get(t, url)
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusOK)

	var result map[string]Category
	decodeJSON(t, resp.Body, &result)
	return result
}

// CreateCategory creates a new category.
func (c *Client) CreateCategory(t *testing.T, instanceID int, name, savePath string) {
	t.Helper()

	body := map[string]string{
		"name":     name,
		"savePath": savePath,
	}

	url := fmt.Sprintf("/api/instances/%d/categories", instanceID)
	resp := c.post(t, url, body)
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusCreated)
}

// DeleteCategory removes a category.
func (c *Client) DeleteCategory(t *testing.T, instanceID int, name string) {
	t.Helper()

	body := map[string]any{
		"categories": []string{name},
	}

	url := fmt.Sprintf("/api/instances/%d/categories", instanceID)
	resp := c.deleteWithBody(t, url, body)
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusOK)
}

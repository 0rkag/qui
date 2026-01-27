package client

import (
	"fmt"
	"net/http"
	"testing"
)

// CreateAPIKey creates a new API key and returns it.
func (c *Client) CreateAPIKey(t *testing.T, name string) APIKeyCreateResponse {
	t.Helper()

	body := map[string]string{
		"name": name,
	}

	resp := c.post(t, "/api/api-keys/", body)
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusCreated)

	var result APIKeyCreateResponse
	decodeJSON(t, resp.Body, &result)
	return result
}

// ListAPIKeys returns all API keys for the current user.
func (c *Client) ListAPIKeys(t *testing.T) []APIKey {
	t.Helper()

	resp := c.get(t, "/api/api-keys/")
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusOK)

	var result []APIKey
	decodeJSON(t, resp.Body, &result)
	return result
}

// DeleteAPIKey deletes an API key by ID.
func (c *Client) DeleteAPIKey(t *testing.T, id int) {
	t.Helper()

	resp := c.delete(t, fmt.Sprintf("/api/api-keys/%d", id))
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusNoContent)
}

// APIKeyCreateResponse is the response from creating an API key.
type APIKeyCreateResponse struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Key       string `json:"key"`
	CreatedAt string `json:"createdAt"`
}

// APIKey represents an API key in list responses.
type APIKey struct {
	ID         int     `json:"id"`
	Name       string  `json:"name"`
	CreatedAt  string  `json:"createdAt"`
	LastUsedAt *string `json:"lastUsedAt,omitempty"`
}

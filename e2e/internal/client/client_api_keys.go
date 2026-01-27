package client

import (
	"fmt"
	"net/http"
	"testing"
)

// CreateClientAPIKey creates a new client API key bound to an instance.
func (c *Client) CreateClientAPIKey(t *testing.T, clientName string, instanceID int) ClientAPIKeyCreateResponse {
	t.Helper()

	body := map[string]any{"clientName": clientName, "instanceId": instanceID}
	resp := c.post(t, "/api/client-api-keys/", body)
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusOK)

	var result ClientAPIKeyCreateResponse
	decodeJSON(t, resp.Body, &result)
	return result
}

// ListClientAPIKeys returns all client API keys.
func (c *Client) ListClientAPIKeys(t *testing.T) []ClientAPIKeyWithInstance {
	t.Helper()

	resp := c.get(t, "/api/client-api-keys/")
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusOK)

	var result []ClientAPIKeyWithInstance
	decodeJSON(t, resp.Body, &result)
	return result
}

// DeleteClientAPIKey deletes a client API key by ID.
func (c *Client) DeleteClientAPIKey(t *testing.T, id int) {
	t.Helper()

	resp := c.delete(t, fmt.Sprintf("/api/client-api-keys/%d", id))
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusNoContent)
}

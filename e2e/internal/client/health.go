package client

import (
	"net/http"
	"testing"
)

// CheckHealth calls GET /health and returns the status code.
func (c *Client) CheckHealth(t *testing.T) int {
	t.Helper()

	resp := c.get(t, "/health")
	defer resp.Body.Close()

	return resp.StatusCode
}

// CheckReadiness calls GET /healthz/readiness and returns the status code.
func (c *Client) CheckReadiness(t *testing.T) int {
	t.Helper()

	resp := c.get(t, "/healthz/readiness")
	defer resp.Body.Close()

	return resp.StatusCode
}

// CheckLiveness calls GET /healthz/liveness and returns the status code.
func (c *Client) CheckLiveness(t *testing.T) int {
	t.Helper()

	resp := c.get(t, "/healthz/liveness")
	defer resp.Body.Close()

	return resp.StatusCode
}

// HealthResponse represents the JSON response from health endpoints.
type HealthResponse struct {
	Status string `json:"status"`
}

// CheckHealthJSON calls GET /health and returns the parsed response.
func (c *Client) CheckHealthJSON(t *testing.T) HealthResponse {
	t.Helper()

	resp := c.get(t, "/health")
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusOK)

	var result HealthResponse
	decodeJSON(t, resp.Body, &result)
	return result
}

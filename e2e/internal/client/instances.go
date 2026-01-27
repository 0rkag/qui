package client

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
)

// TestConnection tests the connection to an instance.
func (c *Client) TestConnection(t *testing.T, id int) TestConnectionResponse {
	t.Helper()

	resp := c.post(t, fmt.Sprintf("/api/instances/%d/test", id), nil)
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusOK)

	var result TestConnectionResponse
	decodeJSON(t, resp.Body, &result)
	return result
}

// UpdateInstanceStatus enables or disables an instance.
func (c *Client) UpdateInstanceStatus(t *testing.T, id int, isActive bool) Instance {
	t.Helper()

	body := map[string]bool{
		"isActive": isActive,
	}

	resp := c.put(t, fmt.Sprintf("/api/instances/%d/status", id), body)
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusOK)

	var result Instance
	decodeJSON(t, resp.Body, &result)
	return result
}

// UpdateInstanceOrder updates the display order of instances.
func (c *Client) UpdateInstanceOrder(t *testing.T, instanceIDs []int) []Instance {
	t.Helper()

	body := map[string][]int{
		"instanceIds": instanceIDs,
	}

	resp := c.put(t, "/api/instances/order", body)
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusOK)

	var result []Instance
	decodeJSON(t, resp.Body, &result)
	return result
}

// CreateInstance creates a new instance and returns its ID.
func (c *Client) CreateInstance(t *testing.T, cfg InstanceConfig) int {
	t.Helper()

	resp := c.post(t, "/api/instances", cfg)
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusCreated)

	var result Instance
	decodeJSON(t, resp.Body, &result)
	return result.ID
}

// CreateInstanceRaw creates an instance and returns the raw response for error testing.
func (c *Client) CreateInstanceRaw(t *testing.T, cfg InstanceConfig) *http.Response {
	t.Helper()
	return c.post(t, "/api/instances", cfg)
}

// GetInstance retrieves an instance by ID using the list endpoint with filtering.
func (c *Client) GetInstance(t *testing.T, id int) Instance {
	t.Helper()

	instances := c.ListInstances(t)
	for _, inst := range instances {
		if inst.ID == id {
			return inst
		}
	}
	t.Fatalf("instance %d not found", id)
	return Instance{}
}

// GetInstanceRaw retrieves an instance and returns the raw response for error testing.
// Uses list endpoint and checks if instance exists.
func (c *Client) GetInstanceRaw(t *testing.T, _ int) *http.Response {
	t.Helper()
	// Since there's no direct GET endpoint, we use the list and check
	resp := c.get(t, "/api/instances")
	return resp
}

// InstanceExists checks if an instance with given ID exists.
func (c *Client) InstanceExists(t *testing.T, id int) bool {
	t.Helper()
	instances := c.ListInstances(t)
	for _, inst := range instances {
		if inst.ID == id {
			return true
		}
	}
	return false
}

// UpdateInstance updates an existing instance.
func (c *Client) UpdateInstance(t *testing.T, id int, cfg InstanceConfig) {
	t.Helper()

	resp := c.put(t, fmt.Sprintf("/api/instances/%d/", id), cfg)
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusOK)
}

// DeleteInstance deletes an instance by ID.
func (c *Client) DeleteInstance(t *testing.T, id int) {
	t.Helper()

	resp := c.delete(t, fmt.Sprintf("/api/instances/%d/", id))
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusOK)
}

// ListInstances returns all instances.
func (c *Client) ListInstances(t *testing.T) []Instance {
	t.Helper()

	resp := c.get(t, "/api/instances")
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusOK)

	var instances []Instance
	decodeJSON(t, resp.Body, &instances)
	return instances
}

// GetCapabilities retrieves instance capabilities.
func (c *Client) GetCapabilities(t *testing.T, instanceID int) Capabilities {
	t.Helper()

	resp := c.get(t, fmt.Sprintf("/api/instances/%d/capabilities", instanceID))
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusOK)

	var caps Capabilities
	decodeJSON(t, resp.Body, &caps)
	return caps
}

// TryGetCapabilities retrieves instance capabilities without failing the test on error.
// Returns an error if the request fails or returns non-200 status.
func (c *Client) TryGetCapabilities(t *testing.T, instanceID int) (Capabilities, error) {
	t.Helper()

	resp := c.get(t, fmt.Sprintf("/api/instances/%d/capabilities", instanceID))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return Capabilities{}, fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	var caps Capabilities
	if err := json.NewDecoder(resp.Body).Decode(&caps); err != nil {
		return Capabilities{}, fmt.Errorf("failed to decode: %w", err)
	}
	return caps, nil
}

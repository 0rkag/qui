package client

import (
	"net/http"
	"testing"
)

// GetLogExclusions returns the current log exclusion patterns.
func (c *Client) GetLogExclusions(t *testing.T) LogExclusions {
	t.Helper()

	resp := c.get(t, "/api/log-exclusions")
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusOK)

	var result LogExclusions
	decodeJSON(t, resp.Body, &result)
	return result
}

// UpdateLogExclusions updates the log exclusion patterns.
func (c *Client) UpdateLogExclusions(t *testing.T, patterns []string) LogExclusions {
	t.Helper()

	body := map[string][]string{"patterns": patterns}
	resp := c.put(t, "/api/log-exclusions", body)
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusOK)

	var result LogExclusions
	decodeJSON(t, resp.Body, &result)
	return result
}

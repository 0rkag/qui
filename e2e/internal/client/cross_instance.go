package client

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
)

// CrossInstanceTorrentResponse is the response from the cross-instance torrents endpoint.
type CrossInstanceTorrentResponse struct {
	Torrents        []CrossInstanceTorrent `json:"torrents"`
	Total           int                    `json:"total"`
	HasMore         bool                   `json:"hasMore"`
	IsCrossInstance bool                   `json:"isCrossInstance"`
}

// CrossInstanceTorrent represents a torrent in cross-instance results.
type CrossInstanceTorrent struct {
	InstanceID   int     `json:"instanceId"`
	InstanceName string  `json:"instanceName"`
	Hash         string  `json:"hash"`
	Name         string  `json:"name"`
	State        string  `json:"state"`
	Progress     float64 `json:"progress"`
	Size         int64   `json:"size"`
	Category     string  `json:"category"`
	Tags         string  `json:"tags"`
}

// ListCrossInstanceTorrents returns torrents from all instances matching the filter expression.
func (c *Client) ListCrossInstanceTorrents(t *testing.T, expr string) CrossInstanceTorrentResponse {
	t.Helper()

	// Build filter JSON and URL encode it
	filters := map[string]string{"expr": expr}
	filtersJSON, err := json.Marshal(filters)
	requireNoError(t, err)

	// URL encode the JSON
	encodedFilters := url.QueryEscape(string(filtersJSON))
	path := "/api/torrents/cross-instance?filters=" + encodedFilters
	resp := c.get(t, path)
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusOK)

	var result CrossInstanceTorrentResponse
	decodeJSON(t, resp.Body, &result)
	return result
}

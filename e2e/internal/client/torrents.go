package client

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// AddTorrent adds a torrent from file data and returns the hash.
func (c *Client) AddTorrent(t *testing.T, instanceID int, torrentData []byte, opts AddTorrentOptions) string {
	t.Helper()

	// Build multipart form
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// Add torrent file
	part, err := writer.CreateFormFile("torrents", "test.torrent")
	requireNoError(t, err)
	_, err = part.Write(torrentData)
	requireNoError(t, err)

	// Add options
	if opts.SavePath != "" {
		requireNoError(t, writer.WriteField("savepath", opts.SavePath))
	}
	if opts.Category != "" {
		requireNoError(t, writer.WriteField("category", opts.Category))
	}
	if opts.Paused {
		requireNoError(t, writer.WriteField("paused", "true"))
	}
	for _, tag := range opts.Tags {
		requireNoError(t, writer.WriteField("tags", tag))
	}

	requireNoError(t, writer.Close())

	url := fmt.Sprintf("/api/instances/%d/torrents", instanceID)
	req := c.newRequest(t, "POST", url, &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp := c.do(t, req)
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusOK)

	var result struct {
		Added []string `json:"added"`
	}
	decodeJSON(t, resp.Body, &result)

	if len(result.Added) == 0 {
		t.Fatal("no torrent hash returned")
	}
	return result.Added[0]
}

// AddTorrentFromMagnet adds a torrent from a magnet link and returns the hash.
// The hash is extracted from the magnet link itself since the API doesn't return it.
func (c *Client) AddTorrentFromMagnet(t *testing.T, instanceID int, magnet string, opts AddTorrentOptions) string {
	t.Helper()

	// Extract hash from magnet link (btih parameter)
	hash := extractHashFromMagnet(magnet)
	if hash == "" {
		t.Fatalf("could not extract hash from magnet link: %s", magnet)
	}

	// Build multipart form
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// Add magnet URL
	requireNoError(t, writer.WriteField("urls", magnet))

	// Add options
	if opts.SavePath != "" {
		requireNoError(t, writer.WriteField("savepath", opts.SavePath))
	}
	if opts.Category != "" {
		requireNoError(t, writer.WriteField("category", opts.Category))
	}
	if opts.Paused {
		requireNoError(t, writer.WriteField("paused", "true"))
	}
	if len(opts.Tags) > 0 {
		requireNoError(t, writer.WriteField("tags", strings.Join(opts.Tags, ",")))
	}

	requireNoError(t, writer.Close())

	url := fmt.Sprintf("/api/instances/%d/torrents", instanceID)
	req := c.newRequest(t, "POST", url, &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp := c.do(t, req)
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusCreated)

	// API returns {"message": "...", "added": N, "failed": N}
	var result struct {
		Added   int    `json:"added"`
		Failed  int    `json:"failed"`
		Message string `json:"message"`
	}
	decodeJSON(t, resp.Body, &result)

	if result.Added == 0 {
		t.Fatalf("torrent not added: %s", result.Message)
	}

	return hash
}

// extractHashFromMagnet extracts the btih hash from a magnet link.
func extractHashFromMagnet(magnet string) string {
	// Parse: magnet:?xt=urn:btih:HASH&...
	const prefix = "urn:btih:"
	idx := strings.Index(strings.ToLower(magnet), prefix)
	if idx == -1 {
		return ""
	}
	start := idx + len(prefix)
	end := start
	for end < len(magnet) && magnet[end] != '&' {
		end++
	}
	return strings.ToLower(magnet[start:end])
}

// AddTorrentFromFile adds a torrent from a .torrent file path and returns the hash.
// The hash is discovered by listing torrents before and after adding.
func (c *Client) AddTorrentFromFile(t *testing.T, instanceID int, filePath string, opts AddTorrentOptions) string {
	t.Helper()

	// Get existing torrent hashes before adding
	existingHashes := make(map[string]bool)
	before := c.ListTorrents(t, instanceID, ListOptions{})
	for _, tor := range before.Torrents {
		existingHashes[tor.Hash] = true
	}

	// Read torrent file
	torrentData, err := os.ReadFile(filePath)
	requireNoError(t, err)

	// Build multipart form
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// Add torrent file (API expects field name "torrent")
	part, err := writer.CreateFormFile("torrent", filepath.Base(filePath))
	requireNoError(t, err)
	_, err = part.Write(torrentData)
	requireNoError(t, err)

	// Add options
	if opts.SavePath != "" {
		requireNoError(t, writer.WriteField("savepath", opts.SavePath))
	}
	if opts.Category != "" {
		requireNoError(t, writer.WriteField("category", opts.Category))
	}
	if opts.Paused {
		requireNoError(t, writer.WriteField("paused", "true"))
	}
	for _, tag := range opts.Tags {
		requireNoError(t, writer.WriteField("tags", tag))
	}

	requireNoError(t, writer.Close())

	url := fmt.Sprintf("/api/instances/%d/torrents", instanceID)
	req := c.newRequest(t, "POST", url, &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp := c.do(t, req)
	defer resp.Body.Close()

	// API returns 201 Created for successful add
	requireStatus(t, resp, http.StatusCreated)

	var result struct {
		Added   int    `json:"added"`
		Failed  int    `json:"failed"`
		Message string `json:"message"`
	}
	decodeJSON(t, resp.Body, &result)

	if result.Added == 0 {
		t.Fatalf("torrent not added from file %s: %s", filePath, result.Message)
	}

	// Find the newly added torrent by comparing before/after
	// Poll a few times in case qBittorrent hasn't synced yet
	var newHash string
	for i := 0; i < 10; i++ {
		time.Sleep(500 * time.Millisecond)
		after := c.ListTorrents(t, instanceID, ListOptions{})
		for _, tor := range after.Torrents {
			if !existingHashes[tor.Hash] {
				newHash = tor.Hash
				break
			}
		}
		if newHash != "" {
			break
		}
	}

	if newHash == "" {
		t.Fatalf("could not find newly added torrent from file %s", filePath)
	}

	return newHash
}

// WaitForCondition polls a torrent until the condition returns true or timeout.
// Returns the final torrent state when condition is met.
func (c *Client) WaitForCondition(t *testing.T, instanceID int, hash string, timeout time.Duration, condition func(Torrent) bool) Torrent {
	t.Helper()

	deadline := time.Now().Add(timeout)
	pollInterval := 500 * time.Millisecond

	for time.Now().Before(deadline) {
		torrents := c.ListTorrents(t, instanceID, ListOptions{Hashes: []string{hash}})
		if len(torrents.Torrents) == 0 {
			t.Fatalf("torrent %s not found while waiting for condition", hash)
		}

		torrent := torrents.Torrents[0]
		if condition(torrent) {
			return torrent
		}

		time.Sleep(pollInterval)
	}

	// Final check
	torrents := c.ListTorrents(t, instanceID, ListOptions{Hashes: []string{hash}})
	if len(torrents.Torrents) == 0 {
		t.Fatalf("torrent %s not found at timeout", hash)
	}

	t.Fatalf("condition not met within %v for torrent %s (final state: %s, progress: %.2f)",
		timeout, hash, torrents.Torrents[0].State, torrents.Torrents[0].Progress)
	return Torrent{} // unreachable
}

// ListTorrents returns torrents for an instance with optional filters.
func (c *Client) ListTorrents(t *testing.T, instanceID int, opts ...ListOptions) TorrentListResponse {
	t.Helper()

	url := fmt.Sprintf("/api/instances/%d/torrents", instanceID)

	if len(opts) > 0 {
		query := opts[0].Encode()
		if query != "" {
			url += "?" + query
		}
	}

	resp := c.get(t, url)
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusOK)

	var result TorrentListResponse
	decodeJSON(t, resp.Body, &result)
	return result
}

// GetTorrent retrieves a single torrent by hash.
func (c *Client) GetTorrent(t *testing.T, instanceID int, hash string) Torrent {
	t.Helper()

	torrents := c.ListTorrents(t, instanceID, ListOptions{
		Hashes: []string{hash},
	})

	if len(torrents.Torrents) == 0 {
		t.Fatalf("torrent %s not found", hash)
	}
	return torrents.Torrents[0]
}

// DeleteTorrent removes a torrent.
func (c *Client) DeleteTorrent(t *testing.T, instanceID int, hash string, deleteFiles bool) {
	t.Helper()
	c.DeleteTorrents(t, instanceID, []string{hash}, deleteFiles)
}

// DeleteTorrents removes multiple torrents via the bulk action endpoint.
func (c *Client) DeleteTorrents(t *testing.T, instanceID int, hashes []string, deleteFiles bool) {
	t.Helper()

	action := "delete"
	if deleteFiles {
		action = "deleteWithFiles"
	}

	body := map[string]any{
		"hashes": hashes,
		"action": action,
	}

	url := fmt.Sprintf("/api/instances/%d/torrents/bulk-action", instanceID)
	resp := c.post(t, url, body)
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusOK)
}

// ---- Torrent Details ----

// GetTorrentProperties returns detailed properties for a specific torrent.
func (c *Client) GetTorrentProperties(t *testing.T, instanceID int, hash string) TorrentProperties {
	t.Helper()

	url := fmt.Sprintf("/api/instances/%d/torrents/%s/properties", instanceID, hash)
	resp := c.get(t, url)
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusOK)

	var result TorrentProperties
	decodeJSON(t, resp.Body, &result)
	return result
}

// GetTorrentTrackers returns trackers for a specific torrent.
func (c *Client) GetTorrentTrackers(t *testing.T, instanceID int, hash string) []Tracker {
	t.Helper()

	url := fmt.Sprintf("/api/instances/%d/torrents/%s/trackers", instanceID, hash)
	resp := c.get(t, url)
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusOK)

	var result []Tracker
	decodeJSON(t, resp.Body, &result)
	return result
}

// GetTorrentFiles returns the file list for a specific torrent.
func (c *Client) GetTorrentFiles(t *testing.T, instanceID int, hash string) []TorrentFile {
	t.Helper()

	url := fmt.Sprintf("/api/instances/%d/torrents/%s/files", instanceID, hash)
	resp := c.get(t, url)
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusOK)

	var result []TorrentFile
	decodeJSON(t, resp.Body, &result)
	return result
}

// ---- Bulk Actions ----

// BulkAction performs a bulk action on multiple torrents.
func (c *Client) BulkAction(t *testing.T, instanceID int, action string, hashes []string) {
	t.Helper()

	body := map[string]any{
		"action": action,
		"hashes": hashes,
	}

	url := fmt.Sprintf("/api/instances/%d/torrents/bulk-action", instanceID)
	resp := c.post(t, url, body)
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusOK)
}

// PauseTorrents pauses multiple torrents.
func (c *Client) PauseTorrents(t *testing.T, instanceID int, hashes []string) {
	c.BulkAction(t, instanceID, "pause", hashes)
}

// ResumeTorrents resumes multiple torrents.
func (c *Client) ResumeTorrents(t *testing.T, instanceID int, hashes []string) {
	c.BulkAction(t, instanceID, "resume", hashes)
}

// RecheckTorrents forces a recheck on multiple torrents.
func (c *Client) RecheckTorrents(t *testing.T, instanceID int, hashes []string) {
	c.BulkAction(t, instanceID, "recheck", hashes)
}

// SetCategory sets the category on multiple torrents.
func (c *Client) SetCategory(t *testing.T, instanceID int, hashes []string, category string) {
	t.Helper()

	body := map[string]any{
		"action":   "setCategory",
		"hashes":   hashes,
		"category": category,
	}

	url := fmt.Sprintf("/api/instances/%d/torrents/bulk-action", instanceID)
	resp := c.post(t, url, body)
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusOK)
}

// AddTags adds tags to multiple torrents.
func (c *Client) AddTags(t *testing.T, instanceID int, hashes []string, tags []string) {
	t.Helper()

	body := map[string]any{
		"action": "addTags",
		"hashes": hashes,
		"tags":   strings.Join(tags, ","),
	}

	url := fmt.Sprintf("/api/instances/%d/torrents/bulk-action", instanceID)
	resp := c.post(t, url, body)
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusOK)
}

// RemoveTags removes tags from multiple torrents.
func (c *Client) RemoveTags(t *testing.T, instanceID int, hashes []string, tags []string) {
	t.Helper()

	body := map[string]any{
		"action": "removeTags",
		"hashes": hashes,
		"tags":   strings.Join(tags, ","),
	}

	url := fmt.Sprintf("/api/instances/%d/torrents/bulk-action", instanceID)
	resp := c.post(t, url, body)
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusOK)
}

// ---- Tracker Operations ----

// EditTorrentTracker edits a tracker URL on a torrent.
func (c *Client) EditTorrentTracker(t *testing.T, instanceID int, hash, oldURL, newURL string) {
	t.Helper()
	body := map[string]string{"oldURL": oldURL, "newURL": newURL}
	resp := c.put(t, fmt.Sprintf("/api/instances/%d/torrents/%s/trackers", instanceID, hash), body)
	defer resp.Body.Close()
	requireStatus(t, resp, http.StatusOK)
}

// AddTorrentTrackers adds tracker URLs to a torrent (newline-separated).
func (c *Client) AddTorrentTrackers(t *testing.T, instanceID int, hash, urls string) {
	t.Helper()
	body := map[string]string{"urls": urls}
	resp := c.post(t, fmt.Sprintf("/api/instances/%d/torrents/%s/trackers", instanceID, hash), body)
	defer resp.Body.Close()
	requireStatus(t, resp, http.StatusOK)
}

// RemoveTorrentTrackers removes tracker URLs from a torrent (newline-separated).
func (c *Client) RemoveTorrentTrackers(t *testing.T, instanceID int, hash, urls string) {
	t.Helper()
	body := map[string]string{"urls": urls}
	resp := c.deleteWithBody(t, fmt.Sprintf("/api/instances/%d/torrents/%s/trackers", instanceID, hash), body)
	defer resp.Body.Close()
	requireStatus(t, resp, http.StatusOK)
}

// GetActiveTrackers returns active tracker domains for an instance.
// The API returns a map of domain -> example tracker URL.
func (c *Client) GetActiveTrackers(t *testing.T, instanceID int) map[string]string {
	t.Helper()
	resp := c.get(t, fmt.Sprintf("/api/instances/%d/trackers", instanceID))
	defer resp.Body.Close()
	requireStatus(t, resp, http.StatusOK)
	var result map[string]string
	decodeJSON(t, resp.Body, &result)
	return result
}

// ---- File Operations ----

// SetTorrentFilePriority sets download priority for specific files.
// Priority: 0=don't download, 1=normal, 6=high, 7=maximum.
func (c *Client) SetTorrentFilePriority(t *testing.T, instanceID int, hash string, indices []int, priority int) {
	t.Helper()
	body := map[string]any{"indices": indices, "priority": priority}
	resp := c.put(t, fmt.Sprintf("/api/instances/%d/torrents/%s/files", instanceID, hash), body)
	defer resp.Body.Close()
	requireStatus(t, resp, http.StatusNoContent)
}

// ---- Rename Operations ----

// RenameTorrent renames the display name of a torrent.
func (c *Client) RenameTorrent(t *testing.T, instanceID int, hash, name string) {
	t.Helper()
	body := map[string]string{"name": name}
	resp := c.put(t, fmt.Sprintf("/api/instances/%d/torrents/%s/rename", instanceID, hash), body)
	defer resp.Body.Close()
	requireStatus(t, resp, http.StatusOK)
}

// RenameTorrentFile renames a file within a torrent.
func (c *Client) RenameTorrentFile(t *testing.T, instanceID int, hash, oldPath, newPath string) {
	t.Helper()
	body := map[string]string{"oldPath": oldPath, "newPath": newPath}
	resp := c.put(t, fmt.Sprintf("/api/instances/%d/torrents/%s/rename-file", instanceID, hash), body)
	defer resp.Body.Close()
	requireStatus(t, resp, http.StatusOK)
}

// RenameTorrentFolder renames a folder within a torrent.
func (c *Client) RenameTorrentFolder(t *testing.T, instanceID int, hash, oldPath, newPath string) {
	t.Helper()
	body := map[string]string{"oldPath": oldPath, "newPath": newPath}
	resp := c.put(t, fmt.Sprintf("/api/instances/%d/torrents/%s/rename-folder", instanceID, hash), body)
	defer resp.Body.Close()
	requireStatus(t, resp, http.StatusOK)
}

// ---- Export ----

// ExportTorrent exports the .torrent file and returns raw bytes.
func (c *Client) ExportTorrent(t *testing.T, instanceID int, hash string) []byte {
	t.Helper()
	resp := c.get(t, fmt.Sprintf("/api/instances/%d/torrents/%s/export", instanceID, hash))
	defer resp.Body.Close()
	requireStatus(t, resp, http.StatusOK)
	data, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return data
}

// ---- Read-Only Detail Endpoints ----

// GetTorrentPeersRaw returns the raw HTTP response for torrent peers.
func (c *Client) GetTorrentPeersRaw(t *testing.T, instanceID int, hash string) *http.Response {
	t.Helper()
	return c.get(t, fmt.Sprintf("/api/instances/%d/torrents/%s/peers", instanceID, hash))
}

// GetTorrentWebSeedsRaw returns the raw HTTP response for torrent web seeds.
func (c *Client) GetTorrentWebSeedsRaw(t *testing.T, instanceID int, hash string) *http.Response {
	t.Helper()
	return c.get(t, fmt.Sprintf("/api/instances/%d/torrents/%s/webseeds", instanceID, hash))
}

// GetTorrentPieceStates returns piece states (0=not downloaded, 1=downloading, 2=downloaded).
func (c *Client) GetTorrentPieceStates(t *testing.T, instanceID int, hash string) []int {
	t.Helper()
	resp := c.get(t, fmt.Sprintf("/api/instances/%d/torrents/%s/pieces", instanceID, hash))
	defer resp.Body.Close()
	requireStatus(t, resp, http.StatusOK)
	var result []int
	decodeJSON(t, resp.Body, &result)
	return result
}

// ---- Duplicate Check & Category Edit ----

// CheckDuplicates checks if provided hashes already exist as torrents.
func (c *Client) CheckDuplicates(t *testing.T, instanceID int, hashes []string) DuplicateCheckResponse {
	t.Helper()
	body := map[string][]string{"hashes": hashes}
	resp := c.post(t, fmt.Sprintf("/api/instances/%d/torrents/check-duplicates", instanceID), body)
	defer resp.Body.Close()
	requireStatus(t, resp, http.StatusOK)
	var result DuplicateCheckResponse
	decodeJSON(t, resp.Body, &result)
	return result
}

// EditCategory edits an existing category's save path.
func (c *Client) EditCategory(t *testing.T, instanceID int, name, savePath string) {
	t.Helper()
	body := map[string]string{"name": name, "savePath": savePath}
	resp := c.put(t, fmt.Sprintf("/api/instances/%d/categories", instanceID), body)
	defer resp.Body.Close()
	requireStatus(t, resp, http.StatusOK)
}

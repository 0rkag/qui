// Package client provides an HTTP client for testing qui's API.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Client is the e2e test client for qui's API.
type Client struct {
	baseURL    string
	httpClient *http.Client
	cookies    []*http.Cookie
}

// New creates a new test client.
func New(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			// Don't follow redirects - we want to catch them
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// ---- Authentication ----

// Setup creates the initial admin user (first-time setup).
func (c *Client) Setup(t *testing.T, username, password string) {
	t.Helper()

	body := map[string]string{
		"username": username,
		"password": password,
	}

	resp := c.post(t, "/api/auth/setup", body)
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusCreated)
	c.extractCookies(resp)
}

// Login authenticates with existing credentials.
func (c *Client) Login(t *testing.T, username, password string) {
	t.Helper()

	body := map[string]string{
		"username": username,
		"password": password,
	}

	resp := c.post(t, "/api/auth/login", body)
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusOK)
	c.extractCookies(resp)
}

func (c *Client) extractCookies(resp *http.Response) {
	c.cookies = resp.Cookies()
}

// SetCookies sets the client's session cookies (used by container setup).
func (c *Client) SetCookies(cookies []*http.Cookie) {
	c.cookies = cookies
}

// ---- Instances ----

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

// ---- Torrents ----

// AddTorrent adds a torrent from file data and returns the hash.
func (c *Client) AddTorrent(t *testing.T, instanceID int, torrentData []byte, opts AddTorrentOptions) string {
	t.Helper()

	// Build multipart form
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// Add torrent file
	part, err := writer.CreateFormFile("torrents", "test.torrent")
	require(t, err)
	_, err = part.Write(torrentData)
	require(t, err)

	// Add options
	if opts.SavePath != "" {
		require(t, writer.WriteField("savepath", opts.SavePath))
	}
	if opts.Category != "" {
		require(t, writer.WriteField("category", opts.Category))
	}
	if opts.Paused {
		require(t, writer.WriteField("paused", "true"))
	}
	for _, tag := range opts.Tags {
		require(t, writer.WriteField("tags", tag))
	}

	require(t, writer.Close())

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
	require(t, writer.WriteField("urls", magnet))

	// Add options
	if opts.SavePath != "" {
		require(t, writer.WriteField("savepath", opts.SavePath))
	}
	if opts.Category != "" {
		require(t, writer.WriteField("category", opts.Category))
	}
	if opts.Paused {
		require(t, writer.WriteField("paused", "true"))
	}
	if len(opts.Tags) > 0 {
		require(t, writer.WriteField("tags", strings.Join(opts.Tags, ",")))
	}

	require(t, writer.Close())

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
		"tags":   tags,
	}

	url := fmt.Sprintf("/api/instances/%d/torrents/bulk-action", instanceID)
	resp := c.post(t, url, body)
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusOK)
}

// ---- Raw Access ----

// GetRaw returns the raw response body for a GET request.
func (c *Client) GetRaw(t *testing.T, path string) []byte {
	t.Helper()

	resp := c.get(t, path)
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusOK)

	data, err := io.ReadAll(resp.Body)
	require(t, err)
	return data
}

// ---- HTTP Helpers ----

func (c *Client) get(t *testing.T, path string) *http.Response {
	t.Helper()
	req := c.newRequest(t, "GET", path, nil)
	return c.do(t, req)
}

func (c *Client) post(t *testing.T, path string, body any) *http.Response {
	t.Helper()

	jsonBody, err := json.Marshal(body)
	require(t, err)

	req := c.newRequest(t, "POST", path, bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	return c.do(t, req)
}

func (c *Client) put(t *testing.T, path string, body any) *http.Response {
	t.Helper()

	jsonBody, err := json.Marshal(body)
	require(t, err)

	req := c.newRequest(t, "PUT", path, bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	return c.do(t, req)
}

func (c *Client) delete(t *testing.T, path string) *http.Response {
	t.Helper()
	req := c.newRequest(t, "DELETE", path, nil)
	return c.do(t, req)
}

func (c *Client) newRequest(t *testing.T, method, path string, body io.Reader) *http.Request {
	t.Helper()

	url := c.baseURL + path
	req, err := http.NewRequestWithContext(context.Background(), method, url, body)
	require(t, err)

	// Add session cookies
	for _, cookie := range c.cookies {
		req.AddCookie(cookie)
	}

	return req
}

func (c *Client) do(t *testing.T, req *http.Request) *http.Response {
	t.Helper()

	resp, err := c.httpClient.Do(req)
	require(t, err)

	return resp
}

// ---- Test Helpers ----

func requireStatus(t *testing.T, resp *http.Response, expected int) {
	t.Helper()
	if resp.StatusCode != expected {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status %d, got %d: %s", expected, resp.StatusCode, string(body))
	}
}

func decodeJSON(t *testing.T, r io.Reader, v any) {
	t.Helper()
	if err := json.NewDecoder(r).Decode(v); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
}

func require(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

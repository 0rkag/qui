// Package client provides an HTTP client for testing qui's API.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"
)

// Client is the e2e test client for qui's API.
type Client struct {
	baseURL    string
	httpClient *http.Client
	cookies    []*http.Cookie
	apiKey     string
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

// SetCookies sets the client's session cookies (used by container setup).
func (c *Client) SetCookies(cookies []*http.Cookie) {
	c.cookies = cookies
}

// SetAPIKey sets the API key for authentication via X-API-Key header.
func (c *Client) SetAPIKey(key string) {
	c.apiKey = key
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
	requireNoError(t, err)

	req := c.newRequest(t, "POST", path, bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	return c.do(t, req)
}

func (c *Client) put(t *testing.T, path string, body any) *http.Response {
	t.Helper()

	jsonBody, err := json.Marshal(body)
	requireNoError(t, err)

	req := c.newRequest(t, "PUT", path, bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	return c.do(t, req)
}

func (c *Client) patch(t *testing.T, path string, body any) *http.Response {
	t.Helper()

	jsonBody, err := json.Marshal(body)
	requireNoError(t, err)

	req := c.newRequest(t, "PATCH", path, bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	return c.do(t, req)
}

func (c *Client) delete(t *testing.T, path string) *http.Response {
	t.Helper()
	req := c.newRequest(t, "DELETE", path, nil)
	return c.do(t, req)
}

func (c *Client) deleteWithBody(t *testing.T, path string, body any) *http.Response {
	t.Helper()

	jsonBody, err := json.Marshal(body)
	requireNoError(t, err)

	req := c.newRequest(t, "DELETE", path, bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	return c.do(t, req)
}

func (c *Client) newRequest(t *testing.T, method, path string, body io.Reader) *http.Request {
	t.Helper()

	url := c.baseURL + path
	req, err := http.NewRequestWithContext(context.Background(), method, url, body)
	requireNoError(t, err)

	// Add API key header if set
	if c.apiKey != "" {
		req.Header.Set("X-API-Key", c.apiKey)
	}

	// Add session cookies
	for _, cookie := range c.cookies {
		req.AddCookie(cookie)
	}

	return req
}

func (c *Client) do(t *testing.T, req *http.Request) *http.Response {
	t.Helper()

	resp, err := c.httpClient.Do(req)
	requireNoError(t, err)

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

func requireNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// GetRaw returns the raw response body for a GET request.
func (c *Client) GetRaw(t *testing.T, path string) []byte {
	t.Helper()

	resp := c.get(t, path)
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusOK)

	data, err := io.ReadAll(resp.Body)
	requireNoError(t, err)
	return data
}

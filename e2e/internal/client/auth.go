package client

import (
	"net/http"
	"testing"
)

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

// SetupRaw calls setup and returns the raw response for error testing.
func (c *Client) SetupRaw(t *testing.T, username, password string) *http.Response {
	t.Helper()

	body := map[string]string{
		"username": username,
		"password": password,
	}

	return c.post(t, "/api/auth/setup", body)
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

// LoginRaw calls login and returns the raw response for error testing.
func (c *Client) LoginRaw(t *testing.T, username, password string) *http.Response {
	t.Helper()

	body := map[string]string{
		"username": username,
		"password": password,
	}

	return c.post(t, "/api/auth/login", body)
}

// Logout ends the current session.
func (c *Client) Logout(t *testing.T) {
	t.Helper()

	resp := c.post(t, "/api/auth/logout", nil)
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusOK)
	c.cookies = nil
}

// LogoutRaw calls logout and returns the raw response.
func (c *Client) LogoutRaw(t *testing.T) *http.Response {
	t.Helper()
	return c.post(t, "/api/auth/logout", nil)
}

// GetCurrentUser returns the current authenticated user.
func (c *Client) GetCurrentUser(t *testing.T) CurrentUserResponse {
	t.Helper()

	resp := c.get(t, "/api/auth/me")
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusOK)

	var result CurrentUserResponse
	decodeJSON(t, resp.Body, &result)
	return result
}

// Validate checks if the current session is valid (200 = valid).
func (c *Client) Validate(t *testing.T) *http.Response {
	t.Helper()
	return c.get(t, "/api/auth/validate")
}

// CheckSetup returns whether initial setup is required.
func (c *Client) CheckSetup(t *testing.T) CheckSetupResponse {
	t.Helper()

	resp := c.get(t, "/api/auth/check-setup")
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusOK)

	var result CheckSetupResponse
	decodeJSON(t, resp.Body, &result)
	return result
}

// ChangePassword changes the current user's password.
func (c *Client) ChangePassword(t *testing.T, currentPassword, newPassword string) {
	t.Helper()

	body := map[string]string{
		"currentPassword": currentPassword,
		"newPassword":     newPassword,
	}

	resp := c.put(t, "/api/auth/change-password", body)
	defer resp.Body.Close()

	requireStatus(t, resp, http.StatusOK)
}

func (c *Client) extractCookies(resp *http.Response) {
	c.cookies = resp.Cookies()
}

// CurrentUserResponse is the response from GET /api/auth/me.
type CurrentUserResponse struct {
	ID       int    `json:"id"`
	Username string `json:"username"`
}

// CheckSetupResponse is the response from GET /api/auth/check-setup.
type CheckSetupResponse struct {
	SetupRequired bool `json:"setupRequired"`
}

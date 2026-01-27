package tests

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/e2e/internal/client"
	"github.com/autobrr/qui/e2e/internal/containers"
)

func TestAuth(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	// NO t.Parallel() — subtests are order-dependent (auth state flows sequentially)

	ctx := context.Background()
	cfg := containers.DefaultConfig()
	cfg.SkipQuiSetup = true

	env := containers.Setup(ctx, t, cfg, 1)
	t.Cleanup(func() { env.Teardown(ctx) })

	c := client.New(env.QuiURL)

	const (
		username    = "admin"
		password    = "adminadmin"
		newPassword = "newpassword123"
	)

	var apiKeyID int
	var apiKeyValue string

	t.Run("check_setup_required_initially", func(t *testing.T) {
		result := c.CheckSetup(t)
		assert.True(t, result.SetupRequired, "setup should be required initially")
	})

	t.Run("setup_creates_admin", func(t *testing.T) {
		c.Setup(t, username, password)
	})

	t.Run("setup_rejects_duplicate", func(t *testing.T) {
		resp := c.SetupRaw(t, username, password)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("check_setup_not_required_after", func(t *testing.T) {
		result := c.CheckSetup(t)
		assert.False(t, result.SetupRequired, "setup should not be required after setup")
	})

	t.Run("login_success", func(t *testing.T) {
		c.Login(t, username, password)
	})

	t.Run("login_wrong_password", func(t *testing.T) {
		badClient := client.New(env.QuiURL)
		resp := badClient.LoginRaw(t, username, "wrongpassword")
		defer resp.Body.Close()
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})

	t.Run("get_current_user", func(t *testing.T) {
		user := c.GetCurrentUser(t)
		assert.Equal(t, username, user.Username)
	})

	t.Run("validate_session", func(t *testing.T) {
		resp := c.Validate(t)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("change_password", func(t *testing.T) {
		c.ChangePassword(t, password, newPassword)
	})

	t.Run("login_with_new_password", func(t *testing.T) {
		freshClient := client.New(env.QuiURL)
		freshClient.Login(t, username, newPassword)
	})

	t.Run("login_with_old_password_fails", func(t *testing.T) {
		badClient := client.New(env.QuiURL)
		resp := badClient.LoginRaw(t, username, password)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})

	// Re-login with new password for API key tests
	c = client.New(env.QuiURL)
	c.Login(t, username, newPassword)

	t.Run("create_api_key", func(t *testing.T) {
		result := c.CreateAPIKey(t, "test-key")
		require.Positive(t, result.ID)
		require.NotEmpty(t, result.Key)
		apiKeyID = result.ID
		apiKeyValue = result.Key
	})

	t.Run("list_api_keys", func(t *testing.T) {
		keys := c.ListAPIKeys(t)
		require.Len(t, keys, 1)
		assert.Equal(t, apiKeyID, keys[0].ID)
		assert.Equal(t, "test-key", keys[0].Name)
	})

	t.Run("api_key_authenticates", func(t *testing.T) {
		keyClient := client.New(env.QuiURL)
		keyClient.SetAPIKey(apiKeyValue)
		// Use a protected endpoint that works with API key auth.
		// GetCurrentUser relies on session data so it won't work here.
		// ListTrackerCustomizations returns 200 if authenticated (empty list is fine).
		keyClient.ListTrackerCustomizations(t)
	})

	t.Run("delete_api_key", func(t *testing.T) {
		c.DeleteAPIKey(t, apiKeyID)
	})

	t.Run("deleted_api_key_rejected", func(t *testing.T) {
		// Hit a protected endpoint with the deleted API key — middleware returns 401.
		req, err := http.NewRequest("GET", env.QuiURL+"/api/tracker-customizations/", nil)
		require.NoError(t, err)
		req.Header.Set("X-API-Key", apiKeyValue)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})

	t.Run("logout", func(t *testing.T) {
		c.Logout(t)
	})

	t.Run("session_invalid_after_logout", func(t *testing.T) {
		resp := c.Validate(t)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})
}

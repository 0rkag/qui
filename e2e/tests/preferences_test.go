package tests

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/e2e/internal/containers"
)

func TestPreferences(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	t.Parallel()

	ctx := context.Background()
	env := containers.Setup(ctx, t, containers.DefaultConfig(), 1)
	t.Cleanup(func() { env.Teardown(ctx) })

	c := env.Client()
	env.RegisterInstances(t)
	instanceID := env.Instances[0].ID

	t.Run("get_preferences", func(t *testing.T) {
		prefs := c.GetPreferences(t, instanceID)
		require.NotEmpty(t, prefs, "preferences should not be empty")
		// qBittorrent always returns a locale field
		assert.Contains(t, prefs, "locale")
	})

	t.Run("update_preferences", func(t *testing.T) {
		// Change download path and verify persistence
		c.UpdatePreferences(t, instanceID, map[string]any{
			"save_path": "/downloads/updated",
		})

		prefs := c.GetPreferences(t, instanceID)
		assert.Equal(t, "/downloads/updated", prefs["save_path"])
	})

	t.Run("get_alt_speed_mode", func(t *testing.T) {
		alt := c.GetAltSpeedMode(t, instanceID)
		// Default state is disabled
		assert.False(t, alt.Enabled, "alt speed should be disabled by default")
	})

	t.Run("toggle_alt_speed", func(t *testing.T) {
		// Toggle on
		result := c.ToggleAltSpeed(t, instanceID)
		assert.True(t, result.Enabled, "alt speed should be enabled after toggle")

		// Toggle off
		result = c.ToggleAltSpeed(t, instanceID)
		assert.False(t, result.Enabled, "alt speed should be disabled after second toggle")
	})
}

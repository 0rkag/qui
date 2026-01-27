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

func TestTrackerCustomizations(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	t.Parallel()

	ctx := context.Background()
	env := containers.Setup(ctx, t, containers.DefaultConfig(), 0)
	t.Cleanup(func() { env.Teardown(ctx) })

	c := env.Client()

	var createdID int

	t.Run("list_empty", func(t *testing.T) {
		items := c.ListTrackerCustomizations(t)
		assert.Empty(t, items, "tracker customizations should be empty initially")
	})

	t.Run("create_customization", func(t *testing.T) {
		result := c.CreateTrackerCustomization(t, client.TrackerCustomizationPayload{
			DisplayName: "My Tracker",
			Domains:     []string{"tracker.example.com", "tracker2.example.com"},
		})

		require.Positive(t, result.ID)
		assert.Equal(t, "My Tracker", result.DisplayName)
		assert.Len(t, result.Domains, 2)
		assert.Contains(t, result.Domains, "tracker.example.com")
		assert.Contains(t, result.Domains, "tracker2.example.com")

		createdID = result.ID
	})

	t.Run("create_validates_missing_name", func(t *testing.T) {
		resp := c.CreateTrackerCustomizationRaw(t, client.TrackerCustomizationPayload{
			DisplayName: "",
			Domains:     []string{"example.com"},
		})
		defer resp.Body.Close()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("create_validates_missing_domains", func(t *testing.T) {
		resp := c.CreateTrackerCustomizationRaw(t, client.TrackerCustomizationPayload{
			DisplayName: "No Domains",
			Domains:     []string{},
		})
		defer resp.Body.Close()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("list_shows_created", func(t *testing.T) {
		items := c.ListTrackerCustomizations(t)
		require.Len(t, items, 1)
		assert.Equal(t, createdID, items[0].ID)
		assert.Equal(t, "My Tracker", items[0].DisplayName)
	})

	t.Run("update_customization", func(t *testing.T) {
		updated := c.UpdateTrackerCustomization(t, createdID, client.TrackerCustomizationPayload{
			DisplayName: "Updated Tracker",
			Domains:     []string{"tracker.example.com"},
		})

		assert.Equal(t, "Updated Tracker", updated.DisplayName)
		assert.Len(t, updated.Domains, 1)
	})

	t.Run("delete_customization", func(t *testing.T) {
		c.DeleteTrackerCustomization(t, createdID)
	})

	t.Run("list_after_delete", func(t *testing.T) {
		items := c.ListTrackerCustomizations(t)
		assert.Empty(t, items, "tracker customizations should be empty after delete")
	})
}

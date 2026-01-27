package tests

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/e2e/internal/client"
	"github.com/autobrr/qui/e2e/internal/containers"
)

func TestDashboardSettings(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	t.Parallel()

	ctx := context.Background()
	env := containers.Setup(ctx, t, containers.DefaultConfig(), 0)
	t.Cleanup(func() { env.Teardown(ctx) })

	c := env.Client()

	t.Run("get_default_settings", func(t *testing.T) {
		settings := c.GetDashboardSettings(t)

		// Verify default section order
		expectedOrder := []string{"server-stats", "tracker-breakdown", "global-stats", "instances"}
		assert.Equal(t, expectedOrder, settings.SectionOrder)

		// Verify all sections visible by default
		for _, section := range expectedOrder {
			require.Contains(t, settings.SectionVisibility, section)
			assert.True(t, settings.SectionVisibility[section], "section %s should be visible by default", section)
		}

		// Verify default tracker breakdown settings
		assert.Equal(t, "uploaded", settings.TrackerBreakdownSortColumn)
		assert.Equal(t, "desc", settings.TrackerBreakdownSortDir)
		assert.Equal(t, 15, settings.TrackerBreakdownItemsPerPage)
	})

	t.Run("update_settings", func(t *testing.T) {
		input := client.DashboardSettingsInput{
			SectionOrder: []string{"instances", "server-stats", "tracker-breakdown", "global-stats"},
			SectionVisibility: map[string]bool{
				"server-stats":      true,
				"tracker-breakdown": false,
				"global-stats":      true,
				"instances":         true,
			},
			TrackerBreakdownSortColumn:   "downloaded",
			TrackerBreakdownSortDir:      "asc",
			TrackerBreakdownItemsPerPage: 25,
		}

		result := c.UpdateDashboardSettings(t, input)
		assert.Equal(t, input.SectionOrder, result.SectionOrder)
		assert.Equal(t, false, result.SectionVisibility["tracker-breakdown"])
		assert.Equal(t, "downloaded", result.TrackerBreakdownSortColumn)
		assert.Equal(t, "asc", result.TrackerBreakdownSortDir)
		assert.Equal(t, 25, result.TrackerBreakdownItemsPerPage)
	})

	t.Run("get_reflects_update", func(t *testing.T) {
		settings := c.GetDashboardSettings(t)

		assert.Equal(t, []string{"instances", "server-stats", "tracker-breakdown", "global-stats"}, settings.SectionOrder)
		assert.Equal(t, false, settings.SectionVisibility["tracker-breakdown"])
		assert.Equal(t, "downloaded", settings.TrackerBreakdownSortColumn)
		assert.Equal(t, "asc", settings.TrackerBreakdownSortDir)
		assert.Equal(t, 25, settings.TrackerBreakdownItemsPerPage)
	})
}

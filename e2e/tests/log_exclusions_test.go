package tests

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/autobrr/qui/e2e/internal/containers"
)

func TestLogExclusions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	t.Parallel()

	ctx := context.Background()
	env := containers.Setup(ctx, t, containers.DefaultConfig(), 0)
	t.Cleanup(func() { env.Teardown(ctx) })

	c := env.Client()

	t.Run("get_default_exclusions", func(t *testing.T) {
		exclusions := c.GetLogExclusions(t)
		assert.NotNil(t, exclusions.Patterns, "patterns should not be nil")
	})

	t.Run("update_exclusions", func(t *testing.T) {
		updated := c.UpdateLogExclusions(t, []string{"test-pattern", "another-pattern"})
		assert.Len(t, updated.Patterns, 2)
		assert.Contains(t, updated.Patterns, "test-pattern")
		assert.Contains(t, updated.Patterns, "another-pattern")
	})

	t.Run("get_reflects_update", func(t *testing.T) {
		exclusions := c.GetLogExclusions(t)
		assert.Len(t, exclusions.Patterns, 2)
		assert.Contains(t, exclusions.Patterns, "test-pattern")
	})
}

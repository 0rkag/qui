package tests

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/autobrr/qui/e2e/internal/client"
	"github.com/autobrr/qui/e2e/internal/containers"
)

func TestHealth(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	t.Parallel()

	ctx := context.Background()
	env := containers.Setup(ctx, t, containers.DefaultConfig(), 0)
	t.Cleanup(func() { env.Teardown(ctx) })

	c := client.New(env.QuiURL)

	t.Run("health_ok", func(t *testing.T) {
		status := c.CheckHealth(t)
		assert.Equal(t, http.StatusOK, status)
	})

	t.Run("readiness_ok", func(t *testing.T) {
		status := c.CheckReadiness(t)
		assert.Equal(t, http.StatusOK, status)
	})

	t.Run("liveness_ok", func(t *testing.T) {
		status := c.CheckLiveness(t)
		assert.Equal(t, http.StatusOK, status)
	})
}

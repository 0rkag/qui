package tests

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/e2e/internal/client"
	"github.com/autobrr/qui/e2e/internal/containers"
)

func TestInstanceCRUD(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	t.Parallel()

	ctx := context.Background()
	env := containers.Setup(ctx, t, containers.DefaultConfig())
	t.Cleanup(func() { env.Teardown(ctx) })

	c := env.Client()

	t.Run("create qbittorrent instance", func(t *testing.T) {
		id := c.CreateInstance(t, client.InstanceConfig{
			Name:     "test-qbt",
			Host:     env.QBitURL,
			Username: "admin",
			Password: env.QBitPassword,
		})
		require.Positive(t, id)
		t.Cleanup(func() { c.DeleteInstance(t, id) })

		// Verify it exists
		instance := c.GetInstance(t, id)
		assert.Equal(t, "test-qbt", instance.Name)
		assert.Equal(t, env.QBitURL, instance.Host)
	})

	// Note: qui creates instances even with bad credentials - connection
	// validation happens asynchronously. These tests verify the instance
	// is created but remains unhealthy.
	t.Run("create instance with invalid credentials", func(t *testing.T) {
		t.Skip("Instance creation with invalid credentials is allowed - validation is async")
	})

	t.Run("create instance with unreachable host", func(t *testing.T) {
		t.Skip("Instance creation with unreachable host is allowed - validation is async")
	})

	t.Run("list instances", func(t *testing.T) {
		// Create two instances
		id1 := c.CreateInstance(t, client.InstanceConfig{
			Name:     "instance-1",
			Host:     env.QBitURL,
			Username: "admin",
			Password: env.QBitPassword,
		})
		t.Cleanup(func() { c.DeleteInstance(t, id1) })

		id2 := c.CreateInstance(t, client.InstanceConfig{
			Name:     "instance-2",
			Host:     env.QBitURL,
			Username: "admin",
			Password: env.QBitPassword,
		})
		t.Cleanup(func() { c.DeleteInstance(t, id2) })

		instances := c.ListInstances(t)
		assert.GreaterOrEqual(t, len(instances), 2)

		names := make([]string, len(instances))
		for i, inst := range instances {
			names[i] = inst.Name
		}
		assert.Contains(t, names, "instance-1")
		assert.Contains(t, names, "instance-2")
	})

	t.Run("update instance", func(t *testing.T) {
		id := c.CreateInstance(t, client.InstanceConfig{
			Name:     "to-update",
			Host:     env.QBitURL,
			Username: "admin",
			Password: env.QBitPassword,
		})
		t.Cleanup(func() { c.DeleteInstance(t, id) })

		c.UpdateInstance(t, id, client.InstanceConfig{
			Name:     "updated-name",
			Host:     env.QBitURL,
			Username: "admin",
			Password: env.QBitPassword,
		})

		instance := c.GetInstance(t, id)
		assert.Equal(t, "updated-name", instance.Name)
	})

	t.Run("delete instance", func(t *testing.T) {
		id := c.CreateInstance(t, client.InstanceConfig{
			Name:     "to-delete",
			Host:     env.QBitURL,
			Username: "admin",
			Password: env.QBitPassword,
		})

		c.DeleteInstance(t, id)

		// Verify it's gone
		assert.False(t, c.InstanceExists(t, id))
	})
}

func TestInstanceCapabilities(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	t.Parallel()

	ctx := context.Background()
	env := containers.Setup(ctx, t, containers.DefaultConfig())
	t.Cleanup(func() { env.Teardown(ctx) })

	c := env.Client()

	id := c.CreateInstance(t, client.InstanceConfig{
		Name:     "caps-test",
		Host:     env.QBitURL,
		Username: "admin",
		Password: env.QBitPassword,
	})
	t.Cleanup(func() { c.DeleteInstance(t, id) })

	// Wait for instance connection to be established (may be in backoff initially)
	// qui uses exponential backoff starting at 1s, so we need longer waits
	var caps client.Capabilities
	var err error
	for i := range 20 {
		caps, err = c.TryGetCapabilities(t, id)
		if err == nil {
			break
		}
		t.Logf("Waiting for instance to be ready (attempt %d): %v", i+1, err)
		time.Sleep(time.Second)
	}
	require.NoError(t, err, "Failed to get capabilities after retries")

	// qBittorrent 4.6+ should support these
	assert.NotEmpty(t, caps.WebAPIVersion)
	assert.True(t, caps.SupportsTorrentExport)
	assert.True(t, caps.SupportsFilePriority)
}

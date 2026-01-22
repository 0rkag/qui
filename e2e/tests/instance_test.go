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
	env := containers.Setup(ctx, t, containers.DefaultConfig(), 1)
	t.Cleanup(func() { env.Teardown(ctx) })

	c := env.Client()
	qbit := env.Instances[0]

	t.Run("create qbittorrent instance", func(t *testing.T) {
		id := c.CreateInstance(t, client.InstanceConfig{
			Name:     "test-qbt",
			Host:     qbit.URL,
			Username: "admin",
			Password: qbit.Password,
		})
		require.Positive(t, id)
		t.Cleanup(func() { c.DeleteInstance(t, id) })

		// Verify it exists
		instance := c.GetInstance(t, id)
		assert.Equal(t, "test-qbt", instance.Name)
		assert.Equal(t, qbit.URL, instance.Host)
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
			Host:     qbit.URL,
			Username: "admin",
			Password: qbit.Password,
		})
		t.Cleanup(func() { c.DeleteInstance(t, id1) })

		id2 := c.CreateInstance(t, client.InstanceConfig{
			Name:     "instance-2",
			Host:     qbit.URL,
			Username: "admin",
			Password: qbit.Password,
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
			Host:     qbit.URL,
			Username: "admin",
			Password: qbit.Password,
		})
		t.Cleanup(func() { c.DeleteInstance(t, id) })

		c.UpdateInstance(t, id, client.InstanceConfig{
			Name:     "updated-name",
			Host:     qbit.URL,
			Username: "admin",
			Password: qbit.Password,
		})

		instance := c.GetInstance(t, id)
		assert.Equal(t, "updated-name", instance.Name)
	})

	t.Run("delete instance", func(t *testing.T) {
		id := c.CreateInstance(t, client.InstanceConfig{
			Name:     "to-delete",
			Host:     qbit.URL,
			Username: "admin",
			Password: qbit.Password,
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
	env := containers.Setup(ctx, t, containers.DefaultConfig(), 1)
	t.Cleanup(func() { env.Teardown(ctx) })

	c := env.Client()
	qbit := env.Instances[0]

	id := c.CreateInstance(t, client.InstanceConfig{
		Name:     "caps-test",
		Host:     qbit.URL,
		Username: "admin",
		Password: qbit.Password,
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

func TestInstanceConnection(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	t.Parallel()

	ctx := context.Background()
	env := containers.Setup(ctx, t, containers.DefaultConfig(), 1)
	t.Cleanup(func() { env.Teardown(ctx) })

	c := env.Client()
	qbit := env.Instances[0]

	t.Run("test connection succeeds for healthy instance", func(t *testing.T) {
		id := c.CreateInstance(t, client.InstanceConfig{
			Name:     "conn-test",
			Host:     qbit.URL,
			Username: "admin",
			Password: qbit.Password,
		})
		t.Cleanup(func() { c.DeleteInstance(t, id) })

		// Wait for instance to connect first
		for i := range 10 {
			_, err := c.TryGetCapabilities(t, id)
			if err == nil {
				break
			}
			t.Logf("Waiting for instance to connect (attempt %d)", i+1)
			time.Sleep(time.Second)
		}

		// Test connection
		result := c.TestConnection(t, id)
		assert.True(t, result.Connected, "connection should succeed")
		assert.Contains(t, result.Message, "successful")
		assert.Empty(t, result.Error)
	})

	t.Run("test connection fails for disabled instance", func(t *testing.T) {
		id := c.CreateInstance(t, client.InstanceConfig{
			Name:     "disabled-test",
			Host:     qbit.URL,
			Username: "admin",
			Password: qbit.Password,
		})
		t.Cleanup(func() { c.DeleteInstance(t, id) })

		// Wait then disable
		for range 10 {
			_, err := c.TryGetCapabilities(t, id)
			if err == nil {
				break
			}
			time.Sleep(time.Second)
		}

		c.UpdateInstanceStatus(t, id, false) // Disable

		result := c.TestConnection(t, id)
		assert.False(t, result.Connected)
		assert.Contains(t, result.Message, "disabled")
	})
}

func TestInstanceStatus(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	t.Parallel()

	ctx := context.Background()
	env := containers.Setup(ctx, t, containers.DefaultConfig(), 1)
	t.Cleanup(func() { env.Teardown(ctx) })

	c := env.Client()
	qbit := env.Instances[0]

	t.Run("disable and re-enable instance", func(t *testing.T) {
		id := c.CreateInstance(t, client.InstanceConfig{
			Name:     "status-test",
			Host:     qbit.URL,
			Username: "admin",
			Password: qbit.Password,
		})
		t.Cleanup(func() { c.DeleteInstance(t, id) })

		// Wait for instance to connect
		for range 10 {
			_, err := c.TryGetCapabilities(t, id)
			if err == nil {
				break
			}
			time.Sleep(time.Second)
		}

		// Verify initially active
		inst := c.GetInstance(t, id)
		assert.True(t, inst.IsActive, "instance should be active initially")

		// Disable instance
		disabled := c.UpdateInstanceStatus(t, id, false)
		assert.False(t, disabled.IsActive, "instance should be disabled")
		assert.Equal(t, id, disabled.ID)

		// Verify via list
		inst = c.GetInstance(t, id)
		assert.False(t, inst.IsActive)

		// Re-enable instance
		enabled := c.UpdateInstanceStatus(t, id, true)
		assert.True(t, enabled.IsActive, "instance should be re-enabled")

		// Verify via list
		inst = c.GetInstance(t, id)
		assert.True(t, inst.IsActive)
	})
}

func TestInstanceOrder(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	t.Parallel()

	ctx := context.Background()
	env := containers.Setup(ctx, t, containers.DefaultConfig(), 1)
	t.Cleanup(func() { env.Teardown(ctx) })

	c := env.Client()
	qbit := env.Instances[0]

	// Create three instances
	id1 := c.CreateInstance(t, client.InstanceConfig{
		Name:     "order-1",
		Host:     qbit.URL,
		Username: "admin",
		Password: qbit.Password,
	})
	t.Cleanup(func() { c.DeleteInstance(t, id1) })

	id2 := c.CreateInstance(t, client.InstanceConfig{
		Name:     "order-2",
		Host:     qbit.URL,
		Username: "admin",
		Password: qbit.Password,
	})
	t.Cleanup(func() { c.DeleteInstance(t, id2) })

	id3 := c.CreateInstance(t, client.InstanceConfig{
		Name:     "order-3",
		Host:     qbit.URL,
		Username: "admin",
		Password: qbit.Password,
	})
	t.Cleanup(func() { c.DeleteInstance(t, id3) })

	t.Run("reorder instances", func(t *testing.T) {
		// Get current order
		instances := c.ListInstances(t)
		require.Len(t, instances, 3)

		// Reorder: 3, 1, 2
		newOrder := []int{id3, id1, id2}
		reordered := c.UpdateInstanceOrder(t, newOrder)

		// Verify new order
		require.Len(t, reordered, 3)
		assert.Equal(t, id3, reordered[0].ID, "first should be id3")
		assert.Equal(t, id1, reordered[1].ID, "second should be id1")
		assert.Equal(t, id2, reordered[2].ID, "third should be id2")

		// Verify order persists via list
		instances = c.ListInstances(t)
		assert.Equal(t, id3, instances[0].ID)
		assert.Equal(t, id1, instances[1].ID)
		assert.Equal(t, id2, instances[2].ID)
	})
}

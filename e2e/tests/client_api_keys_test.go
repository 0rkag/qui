package tests

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/e2e/internal/client"
	"github.com/autobrr/qui/e2e/internal/containers"
)

func TestClientAPIKeys(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	t.Parallel()

	ctx := context.Background()
	env := containers.Setup(ctx, t, containers.DefaultConfig(), 1)
	t.Cleanup(func() { env.Teardown(ctx) })

	c := env.Client()
	qbit := env.Instances[0]

	instanceID := c.CreateInstance(t, client.InstanceConfig{
		Name:     "client-api-key-test",
		Host:     qbit.URL,
		Username: "admin",
		Password: qbit.Password,
	})
	t.Cleanup(func() { c.DeleteInstance(t, instanceID) })

	waitForInstance(t, c, instanceID)

	var createdKeyID int

	t.Run("list_empty", func(t *testing.T) {
		keys := c.ListClientAPIKeys(t)
		assert.Empty(t, keys, "client API keys should be empty initially")
	})

	t.Run("create_client_api_key", func(t *testing.T) {
		result := c.CreateClientAPIKey(t, "test-client", instanceID)
		require.NotEmpty(t, result.Key, "key should be returned")
		require.Positive(t, result.ClientAPIKey.ID, "ID should be positive")
		assert.Equal(t, "test-client", result.ClientAPIKey.ClientName)
		assert.Equal(t, instanceID, result.ClientAPIKey.InstanceID)
		assert.NotEmpty(t, result.ProxyURL, "proxy URL should be set")
		createdKeyID = result.ClientAPIKey.ID
	})

	t.Run("list_shows_created", func(t *testing.T) {
		keys := c.ListClientAPIKeys(t)
		require.Len(t, keys, 1)
		assert.Equal(t, createdKeyID, keys[0].ID)
		assert.Equal(t, "test-client", keys[0].ClientName)
		assert.NotNil(t, keys[0].Instance, "instance info should be populated")
	})

	t.Run("delete_client_api_key", func(t *testing.T) {
		c.DeleteClientAPIKey(t, createdKeyID)
	})

	t.Run("list_after_delete", func(t *testing.T) {
		keys := c.ListClientAPIKeys(t)
		assert.Empty(t, keys, "client API keys should be empty after delete")
	})
}

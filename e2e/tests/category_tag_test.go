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

func TestCategories(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	t.Parallel()

	ctx := context.Background()
	env := containers.Setup(ctx, t, containers.DefaultConfig())
	t.Cleanup(func() { env.Teardown(ctx) })

	c := env.Client()

	// Create an instance to work with
	instanceID := c.CreateInstance(t, client.InstanceConfig{
		Name:     "category-test",
		Host:     env.QBitURL,
		Username: "admin",
		Password: env.QBitPassword,
	})
	t.Cleanup(func() { c.DeleteInstance(t, instanceID) })

	waitForInstance(t, c, instanceID)

	t.Run("create and list categories", func(t *testing.T) {
		// Create a category
		c.CreateCategory(t, instanceID, "movies", "/downloads/movies")
		t.Cleanup(func() { c.DeleteCategory(t, instanceID, "movies") })

		// Wait for sync
		time.Sleep(time.Second)

		// List categories
		categories := c.GetCategories(t, instanceID)
		require.Contains(t, categories, "movies")
		assert.Equal(t, "/downloads/movies", categories["movies"].SavePath)
	})

	t.Run("set category on torrent", func(t *testing.T) {
		// Create a category first
		c.CreateCategory(t, instanceID, "tv-shows", "/downloads/tv")
		t.Cleanup(func() { c.DeleteCategory(t, instanceID, "tv-shows") })

		// Add a torrent
		hash := c.AddTorrentFromMagnet(t, instanceID, testMagnet, client.AddTorrentOptions{
			Paused: true,
		})
		t.Cleanup(func() { c.DeleteTorrent(t, instanceID, hash, true) })

		// Wait for sync
		time.Sleep(time.Second)

		// Set category
		c.SetCategory(t, instanceID, []string{hash}, "tv-shows")

		// Wait for qui to sync category from qBittorrent
		time.Sleep(2 * time.Second)

		// Verify
		torrent := c.GetTorrent(t, instanceID, hash)
		assert.Equal(t, "tv-shows", torrent.Category)
	})

	t.Run("delete category", func(t *testing.T) {
		// Create a category
		c.CreateCategory(t, instanceID, "temp-cat", "/downloads/temp")

		// Wait for sync
		time.Sleep(time.Second)

		// Delete it
		c.DeleteCategory(t, instanceID, "temp-cat")

		// Wait for sync
		time.Sleep(time.Second)

		// Verify it's gone
		categories := c.GetCategories(t, instanceID)
		assert.NotContains(t, categories, "temp-cat")
	})
}

func TestTags(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	t.Parallel()

	ctx := context.Background()
	env := containers.Setup(ctx, t, containers.DefaultConfig())
	t.Cleanup(func() { env.Teardown(ctx) })

	c := env.Client()

	// Create an instance to work with
	instanceID := c.CreateInstance(t, client.InstanceConfig{
		Name:     "tag-test",
		Host:     env.QBitURL,
		Username: "admin",
		Password: env.QBitPassword,
	})
	t.Cleanup(func() { c.DeleteInstance(t, instanceID) })

	waitForInstance(t, c, instanceID)

	t.Run("create and list tags", func(t *testing.T) {
		// Create tags
		c.CreateTags(t, instanceID, []string{"hd", "favorite"})
		t.Cleanup(func() { c.DeleteTags(t, instanceID, []string{"hd", "favorite"}) })

		// Wait for sync
		time.Sleep(time.Second)

		// List tags
		tags := c.GetTags(t, instanceID)
		assert.Contains(t, tags, "hd")
		assert.Contains(t, tags, "favorite")
	})

	t.Run("add tags to torrent", func(t *testing.T) {
		// Add a torrent
		hash := c.AddTorrentFromMagnet(t, instanceID, testMagnet, client.AddTorrentOptions{
			Paused: true,
		})
		t.Cleanup(func() { c.DeleteTorrent(t, instanceID, hash, true) })

		// Wait for sync
		time.Sleep(time.Second)

		// Add tags to torrent
		c.AddTags(t, instanceID, []string{hash}, []string{"test-tag", "another-tag"})
		t.Cleanup(func() { c.DeleteTags(t, instanceID, []string{"test-tag", "another-tag"}) })

		// Wait for qui to sync tags from qBittorrent
		time.Sleep(2 * time.Second)

		// Verify tags on torrent
		torrent := c.GetTorrent(t, instanceID, hash)
		assert.Contains(t, torrent.Tags, "test-tag")
		assert.Contains(t, torrent.Tags, "another-tag")
	})

	t.Run("remove tags from torrent", func(t *testing.T) {
		// Add a torrent with tags
		hash := c.AddTorrentFromMagnet(t, instanceID, testMagnet, client.AddTorrentOptions{
			Paused: true,
		})
		t.Cleanup(func() { c.DeleteTorrent(t, instanceID, hash, true) })

		// Wait for sync
		time.Sleep(time.Second)

		// Add tags
		c.AddTags(t, instanceID, []string{hash}, []string{"remove-me", "keep-me"})
		t.Cleanup(func() { c.DeleteTags(t, instanceID, []string{"remove-me", "keep-me"}) })

		// Wait for sync
		time.Sleep(time.Second)

		// Remove one tag
		c.RemoveTags(t, instanceID, []string{hash}, []string{"remove-me"})

		// Wait for sync
		time.Sleep(time.Second)

		// Verify
		torrent := c.GetTorrent(t, instanceID, hash)
		assert.NotContains(t, torrent.Tags, "remove-me")
		assert.Contains(t, torrent.Tags, "keep-me")
	})

	t.Run("delete tags", func(t *testing.T) {
		// Create a tag
		c.CreateTags(t, instanceID, []string{"temp-tag"})

		// Wait for sync
		time.Sleep(time.Second)

		// Verify it exists
		tags := c.GetTags(t, instanceID)
		require.Contains(t, tags, "temp-tag")

		// Delete it
		c.DeleteTags(t, instanceID, []string{"temp-tag"})

		// Wait for sync
		time.Sleep(time.Second)

		// Verify it's gone
		tags = c.GetTags(t, instanceID)
		assert.NotContains(t, tags, "temp-tag")
	})
}

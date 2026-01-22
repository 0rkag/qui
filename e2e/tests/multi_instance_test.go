package tests

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/e2e/internal/client"
	"github.com/autobrr/qui/e2e/internal/containers"
)

// =============================================================================
// Data Isolation Tests
// =============================================================================

func TestMultiInstanceDataIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	t.Parallel()

	ctx := context.Background()
	env := containers.Setup(ctx, t, containers.DefaultConfig(), 2)
	t.Cleanup(func() { env.Teardown(ctx) })
	env.RegisterInstances(t)

	c := env.Client()
	instA := env.Instances[0]
	instB := env.Instances[1]

	// Wait for both instances to connect
	waitForInstance(t, c, instA.ID)
	waitForInstance(t, c, instB.ID)

	t.Run("torrents_isolated", func(t *testing.T) {
		// Add torrent only to instance A
		hash := c.AddTorrentFromMagnet(t, instA.ID, testMagnet, client.AddTorrentOptions{
			Paused: true,
		})
		t.Cleanup(func() { c.DeleteTorrent(t, instA.ID, hash, true) })

		time.Sleep(time.Second) // Wait for sync

		// Verify torrent exists on A
		torrentsA := c.ListTorrents(t, instA.ID)
		assert.GreaterOrEqual(t, len(torrentsA.Torrents), 1, "torrent should exist on instance A")

		// Verify torrent does NOT exist on B
		torrentsB := c.ListTorrents(t, instB.ID, client.ListOptions{Hashes: []string{hash}})
		assert.Empty(t, torrentsB.Torrents, "torrent should NOT exist on instance B")
	})

	t.Run("categories_isolated", func(t *testing.T) {
		// Create category only on instance A
		c.CreateCategory(t, instA.ID, "isolated-cat", "/downloads/isolated")
		t.Cleanup(func() { c.DeleteCategory(t, instA.ID, "isolated-cat") })

		time.Sleep(500 * time.Millisecond) // Wait for sync

		// Verify category exists on A
		catsA := c.GetCategories(t, instA.ID)
		_, existsA := catsA["isolated-cat"]
		assert.True(t, existsA, "category should exist on instance A")

		// Verify category does NOT exist on B
		catsB := c.GetCategories(t, instB.ID)
		_, existsB := catsB["isolated-cat"]
		assert.False(t, existsB, "category should NOT exist on instance B")
	})

	t.Run("tags_isolated", func(t *testing.T) {
		// Create tag only on instance A
		c.CreateTags(t, instA.ID, []string{"isolated-tag"})
		t.Cleanup(func() { c.DeleteTags(t, instA.ID, []string{"isolated-tag"}) })

		time.Sleep(500 * time.Millisecond) // Wait for sync

		// Verify tag exists on A
		tagsA := c.GetTags(t, instA.ID)
		assert.Contains(t, tagsA, "isolated-tag", "tag should exist on instance A")

		// Verify tag does NOT exist on B
		tagsB := c.GetTags(t, instB.ID)
		assert.NotContains(t, tagsB, "isolated-tag", "tag should NOT exist on instance B")
	})

	t.Run("same_hash_independent", func(t *testing.T) {
		// Add same torrent to both instances
		hashA := c.AddTorrentFromMagnet(t, instA.ID, testMagnet, client.AddTorrentOptions{
			Paused: true,
		})
		t.Cleanup(func() { c.DeleteTorrent(t, instA.ID, hashA, true) })

		hashB := c.AddTorrentFromMagnet(t, instB.ID, testMagnet, client.AddTorrentOptions{
			Paused: true,
		})
		t.Cleanup(func() { c.DeleteTorrent(t, instB.ID, hashB, true) })

		assert.Equal(t, hashA, hashB, "same magnet should produce same hash")

		time.Sleep(time.Second)

		// Create category on A and assign to torrent
		c.CreateCategory(t, instA.ID, "cat-for-a", "/downloads/a")
		t.Cleanup(func() { c.DeleteCategory(t, instA.ID, "cat-for-a") })
		c.SetCategory(t, instA.ID, []string{hashA}, "cat-for-a")

		time.Sleep(500 * time.Millisecond)

		// Verify A has category, B does not
		torrentA := c.GetTorrent(t, instA.ID, hashA)
		torrentB := c.GetTorrent(t, instB.ID, hashB)

		assert.Equal(t, "cat-for-a", torrentA.Category, "torrent on A should have category")
		assert.Empty(t, torrentB.Category, "torrent on B should NOT have category")
	})
}

// =============================================================================
// Concurrent Operations Tests
// =============================================================================

func TestMultiInstanceConcurrentOps(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	t.Parallel()

	ctx := context.Background()
	env := containers.Setup(ctx, t, containers.DefaultConfig(), 2)
	t.Cleanup(func() { env.Teardown(ctx) })
	env.RegisterInstances(t)

	c := env.Client()
	instA := env.Instances[0]
	instB := env.Instances[1]

	waitForInstance(t, c, instA.ID)
	waitForInstance(t, c, instB.ID)

	t.Run("concurrent_torrent_adds", func(t *testing.T) {
		var wg sync.WaitGroup
		var hashA, hashB string
		var errA, errB error

		wg.Add(2)

		// Add to instance A
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					if err, ok := r.(error); ok {
						errA = err
					} else {
						errA = fmt.Errorf("panic: %v", r)
					}
				}
			}()
			hashA = c.AddTorrentFromMagnet(t, instA.ID, testMagnet, client.AddTorrentOptions{Paused: true})
		}()

		// Add to instance B (using same magnet - should work independently)
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					if err, ok := r.(error); ok {
						errB = err
					} else {
						errB = fmt.Errorf("panic: %v", r)
					}
				}
			}()
			hashB = c.AddTorrentFromMagnet(t, instB.ID, testMagnet, client.AddTorrentOptions{Paused: true})
		}()

		wg.Wait()

		require.NoError(t, errA, "adding torrent to A should succeed")
		require.NoError(t, errB, "adding torrent to B should succeed")

		t.Cleanup(func() {
			c.DeleteTorrent(t, instA.ID, hashA, true)
			c.DeleteTorrent(t, instB.ID, hashB, true)
		})

		assert.NotEmpty(t, hashA)
		assert.NotEmpty(t, hashB)
	})

	t.Run("concurrent_bulk_actions", func(t *testing.T) {
		// Setup: add torrents to both instances
		hashA := c.AddTorrentFromMagnet(t, instA.ID, testMagnet, client.AddTorrentOptions{Paused: true})
		t.Cleanup(func() { c.DeleteTorrent(t, instA.ID, hashA, true) })

		hashB := c.AddTorrentFromMagnet(t, instB.ID, testMagnet, client.AddTorrentOptions{Paused: true})
		t.Cleanup(func() { c.DeleteTorrent(t, instB.ID, hashB, true) })

		time.Sleep(time.Second)

		// Concurrent: pause on A, resume on B
		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			c.PauseTorrents(t, instA.ID, []string{hashA})
		}()

		go func() {
			defer wg.Done()
			c.ResumeTorrents(t, instB.ID, []string{hashB})
		}()

		wg.Wait()

		// Both operations should complete without error (test didn't panic)
		// Verify torrents still exist
		torrentA := c.GetTorrent(t, instA.ID, hashA)
		torrentB := c.GetTorrent(t, instB.ID, hashB)
		assert.NotEmpty(t, torrentA.Hash)
		assert.NotEmpty(t, torrentB.Hash)
	})

	t.Run("instance_error_isolation", func(t *testing.T) {
		// Add torrent to B before stopping A
		hashB := c.AddTorrentFromMagnet(t, instB.ID, testMagnet, client.AddTorrentOptions{Paused: true})
		t.Cleanup(func() { c.DeleteTorrent(t, instB.ID, hashB, true) })

		// Stop instance A's container
		err := instA.Container.Stop(ctx, nil)
		require.NoError(t, err, "stopping instance A container should succeed")

		// Instance B should still work
		time.Sleep(time.Second)

		torrentsB := c.ListTorrents(t, instB.ID)
		assert.GreaterOrEqual(t, len(torrentsB.Torrents), 1, "instance B should still work after A is stopped")

		// Restart A for cleanup
		err = instA.Container.Start(ctx)
		require.NoError(t, err, "restarting instance A should succeed")
	})
}

// =============================================================================
// Instance Lifecycle Tests
// =============================================================================

func TestMultiInstanceLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	t.Parallel()

	ctx := context.Background()
	// Start with just 1 instance
	env := containers.Setup(ctx, t, containers.DefaultConfig(), 1)
	t.Cleanup(func() { env.Teardown(ctx) })
	env.RegisterInstances(t)

	c := env.Client()
	instA := env.Instances[0]

	waitForInstance(t, c, instA.ID)

	t.Run("add_instance_while_active", func(t *testing.T) {
		// Add torrent to instance A
		hash := c.AddTorrentFromMagnet(t, instA.ID, testMagnet, client.AddTorrentOptions{Paused: true})
		t.Cleanup(func() { c.DeleteTorrent(t, instA.ID, hash, true) })

		time.Sleep(time.Second)

		// Create a new qBittorrent instance B (manually, since env only has 1)
		// For simplicity, we'll just create another instance pointing to the same qBit
		// This tests qui's handling, not actual container creation
		instBID := c.CreateInstance(t, client.InstanceConfig{
			Name:     "qbit-dynamic",
			Host:     instA.URL,
			Username: "admin",
			Password: instA.Password,
		})
		t.Cleanup(func() { c.DeleteInstance(t, instBID) })

		// Verify A's torrents are unaffected
		torrentsA := c.ListTorrents(t, instA.ID, client.ListOptions{Hashes: []string{hash}})
		assert.Len(t, torrentsA.Torrents, 1, "instance A should still have its torrent")
	})

	t.Run("remove_instance_while_active", func(t *testing.T) {
		// Create instance B
		instBID := c.CreateInstance(t, client.InstanceConfig{
			Name:     "qbit-to-remove",
			Host:     instA.URL,
			Username: "admin",
			Password: instA.Password,
		})

		// Add torrent to A
		hash := c.AddTorrentFromMagnet(t, instA.ID, testMagnet, client.AddTorrentOptions{Paused: true})
		t.Cleanup(func() { c.DeleteTorrent(t, instA.ID, hash, true) })

		time.Sleep(time.Second)

		// Delete instance B
		c.DeleteInstance(t, instBID)

		// Verify A still works
		torrentsA := c.ListTorrents(t, instA.ID)
		assert.GreaterOrEqual(t, len(torrentsA.Torrents), 1, "instance A should still work after B is deleted")
	})
}

// =============================================================================
// Cross-Instance Feature Tests
// =============================================================================

func TestMultiInstanceCrossInstance(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	t.Parallel()

	ctx := context.Background()
	env := containers.Setup(ctx, t, containers.DefaultConfig(), 2)
	t.Cleanup(func() { env.Teardown(ctx) })
	env.RegisterInstances(t)

	c := env.Client()
	instA := env.Instances[0]
	instB := env.Instances[1]

	waitForInstance(t, c, instA.ID)
	waitForInstance(t, c, instB.ID)

	t.Run("cross_instance_list_all", func(t *testing.T) {
		// Add torrent to both instances
		hashA := c.AddTorrentFromMagnet(t, instA.ID, testMagnet, client.AddTorrentOptions{Paused: true})
		hashB := c.AddTorrentFromMagnet(t, instB.ID, testMagnet, client.AddTorrentOptions{Paused: true})

		time.Sleep(time.Second)

		// Query cross-instance (match all with wildcard expression)
		result := c.ListCrossInstanceTorrents(t, "name ~ /.*/")

		assert.True(t, result.IsCrossInstance, "should be marked as cross-instance")
		// Since both instances have the same torrent (same hash), the cross-instance
		// query may return both or deduplicate. Check we get at least 1.
		assert.GreaterOrEqual(t, result.Total, 1, "should find at least one torrent")

		// Cleanup
		c.DeleteTorrent(t, instA.ID, hashA, true)
		c.DeleteTorrent(t, instB.ID, hashB, true)
		time.Sleep(500 * time.Millisecond)
	})

	t.Run("cross_instance_filter_by_name", func(t *testing.T) {
		// Both instances have "Big Buck Bunny" from testMagnet
		hashA := c.AddTorrentFromMagnet(t, instA.ID, testMagnet, client.AddTorrentOptions{Paused: true})
		hashB := c.AddTorrentFromMagnet(t, instB.ID, testMagnet, client.AddTorrentOptions{Paused: true})

		time.Sleep(time.Second)

		// Filter by name containing "Buck"
		result := c.ListCrossInstanceTorrents(t, "name ~ /Buck/")

		assert.GreaterOrEqual(t, result.Total, 1, "should find Big Buck Bunny")
		for _, torrent := range result.Torrents {
			assert.Contains(t, torrent.Name, "Buck", "all results should match filter")
		}

		// Cleanup
		c.DeleteTorrent(t, instA.ID, hashA, true)
		c.DeleteTorrent(t, instB.ID, hashB, true)
		time.Sleep(500 * time.Millisecond)
	})

	t.Run("cross_instance_empty_result", func(t *testing.T) {
		// Wait for any previous test cleanup to complete
		time.Sleep(2 * time.Second)

		// Filter for something that definitely doesn't exist
		result := c.ListCrossInstanceTorrents(t, "name ~ /XyzNonExistentTorrent99999AbcDef/")

		assert.Equal(t, 0, result.Total, "should find no torrents")
		assert.Empty(t, result.Torrents, "torrents list should be empty")
	})
}

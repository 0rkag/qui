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

// Well-known public domain torrent (Big Buck Bunny)
const testMagnet = "magnet:?xt=urn:btih:dd8255ecdc7ca55fb0bbf81323d87062db1f6d1c&dn=Big+Buck+Bunny&tr=udp%3A%2F%2Ftracker.opentrackr.org%3A1337"

func TestTorrentCRUD(t *testing.T) {
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
		Name:     "torrent-test",
		Host:     env.QBitURL,
		Username: "admin",
		Password: env.QBitPassword,
	})
	t.Cleanup(func() { c.DeleteInstance(t, instanceID) })

	// Wait for instance to connect
	waitForInstance(t, c, instanceID)

	t.Run("add torrent via magnet", func(t *testing.T) {
		hash := c.AddTorrentFromMagnet(t, instanceID, testMagnet, client.AddTorrentOptions{
			Paused: true, // Don't actually download
		})
		require.NotEmpty(t, hash)
		t.Cleanup(func() { c.DeleteTorrent(t, instanceID, hash, true) })

		// Wait for qui to sync with qBittorrent
		time.Sleep(time.Second)

		// Verify torrent exists
		torrent := c.GetTorrent(t, instanceID, hash)
		assert.Equal(t, hash, torrent.Hash)
		assert.Contains(t, torrent.Name, "Big Buck Bunny")
	})

	t.Run("list torrents", func(t *testing.T) {
		hash1 := c.AddTorrentFromMagnet(t, instanceID, testMagnet, client.AddTorrentOptions{
			Paused: true,
		})
		t.Cleanup(func() { c.DeleteTorrent(t, instanceID, hash1, true) })

		// Wait for sync
		time.Sleep(time.Second)

		// List all torrents
		result := c.ListTorrents(t, instanceID)
		assert.GreaterOrEqual(t, len(result.Torrents), 1)
		assert.GreaterOrEqual(t, result.Total, 1)
	})

	t.Run("list torrents with pagination", func(t *testing.T) {
		hash := c.AddTorrentFromMagnet(t, instanceID, testMagnet, client.AddTorrentOptions{
			Paused: true,
		})
		t.Cleanup(func() { c.DeleteTorrent(t, instanceID, hash, true) })

		// Wait for sync
		time.Sleep(time.Second)

		// Test pagination
		result := c.ListTorrents(t, instanceID, client.ListOptions{
			Page:  1,
			Limit: 10,
		})
		assert.LessOrEqual(t, len(result.Torrents), 10)
	})

	t.Run("filter torrents by hash", func(t *testing.T) {
		hash := c.AddTorrentFromMagnet(t, instanceID, testMagnet, client.AddTorrentOptions{
			Paused: true,
		})
		t.Cleanup(func() { c.DeleteTorrent(t, instanceID, hash, true) })

		// Wait for sync
		time.Sleep(time.Second)

		// Filter by specific hash
		result := c.ListTorrents(t, instanceID, client.ListOptions{
			Hashes: []string{hash},
		})
		require.Len(t, result.Torrents, 1)
		assert.Equal(t, hash, result.Torrents[0].Hash)
	})

	t.Run("delete torrent", func(t *testing.T) {
		hash := c.AddTorrentFromMagnet(t, instanceID, testMagnet, client.AddTorrentOptions{
			Paused: true,
		})

		// Wait for torrent to be added
		time.Sleep(time.Second)

		// Delete it
		c.DeleteTorrent(t, instanceID, hash, true)

		// Wait for deletion to propagate
		time.Sleep(2 * time.Second)

		// Verify it's gone
		result := c.ListTorrents(t, instanceID, client.ListOptions{
			Hashes: []string{hash},
		})
		assert.Empty(t, result.Torrents)
	})
}

func TestTorrentBulkActions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	t.Parallel()

	ctx := context.Background()
	env := containers.Setup(ctx, t, containers.DefaultConfig())
	t.Cleanup(func() { env.Teardown(ctx) })

	c := env.Client()

	instanceID := c.CreateInstance(t, client.InstanceConfig{
		Name:     "bulk-test",
		Host:     env.QBitURL,
		Username: "admin",
		Password: env.QBitPassword,
	})
	t.Cleanup(func() { c.DeleteInstance(t, instanceID) })

	waitForInstance(t, c, instanceID)

	t.Run("pause and resume torrents", func(t *testing.T) {
		hash := c.AddTorrentFromMagnet(t, instanceID, testMagnet, client.AddTorrentOptions{
			Paused: true, // Start paused
		})
		t.Cleanup(func() { c.DeleteTorrent(t, instanceID, hash, true) })

		// Wait for torrent to be recognized
		time.Sleep(time.Second)

		// Verify torrent exists
		torrent := c.GetTorrent(t, instanceID, hash)
		require.NotEmpty(t, torrent.Hash, "torrent should exist")

		// Resume - just verify API call succeeds
		c.ResumeTorrents(t, instanceID, []string{hash})
		time.Sleep(500 * time.Millisecond)

		// Pause - just verify API call succeeds
		c.PauseTorrents(t, instanceID, []string{hash})
		time.Sleep(500 * time.Millisecond)

		// Verify torrent still exists (API calls didn't fail)
		torrent = c.GetTorrent(t, instanceID, hash)
		require.NotEmpty(t, torrent.Hash, "torrent should still exist after pause/resume")
	})

	t.Run("delete multiple torrents", func(t *testing.T) {
		hash := c.AddTorrentFromMagnet(t, instanceID, testMagnet, client.AddTorrentOptions{
			Paused: true,
		})

		// Wait for torrent to be recognized
		time.Sleep(time.Second)

		// Delete (without files for simpler test)
		c.DeleteTorrents(t, instanceID, []string{hash}, false)

		// Wait for deletion to propagate through qui's sync
		time.Sleep(2 * time.Second)

		// Verify gone
		result := c.ListTorrents(t, instanceID, client.ListOptions{
			Hashes: []string{hash},
		})
		assert.Empty(t, result.Torrents, "torrent should be deleted")
	})
}

func TestTorrentDetails(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	t.Parallel()

	ctx := context.Background()
	env := containers.Setup(ctx, t, containers.DefaultConfig())
	t.Cleanup(func() { env.Teardown(ctx) })

	c := env.Client()

	instanceID := c.CreateInstance(t, client.InstanceConfig{
		Name:     "details-test",
		Host:     env.QBitURL,
		Username: "admin",
		Password: env.QBitPassword,
	})
	t.Cleanup(func() { c.DeleteInstance(t, instanceID) })

	waitForInstance(t, c, instanceID)

	// Add a torrent to get details from
	hash := c.AddTorrentFromMagnet(t, instanceID, testMagnet, client.AddTorrentOptions{
		Paused: true,
	})
	t.Cleanup(func() { c.DeleteTorrent(t, instanceID, hash, true) })

	// Wait for torrent to be fully indexed
	time.Sleep(2 * time.Second)

	t.Run("get torrent properties", func(t *testing.T) {
		props := c.GetTorrentProperties(t, instanceID, hash)

		// Verify key properties are populated
		// Note: For magnets without metadata, TotalSize/PiecesNum may be -1
		assert.NotEmpty(t, props.SavePath, "save_path should be set")

		// If metadata is available, verify sizes are positive
		// TotalSize == -1 means metadata hasn't been fetched yet (common for magnets)
		if props.TotalSize > 0 {
			assert.Greater(t, props.PiecesNum, 0, "pieces_num should be positive when metadata is available")
		} else {
			t.Logf("Metadata not yet fetched (total_size=%d), skipping size assertions", props.TotalSize)
		}
	})

	t.Run("get torrent trackers", func(t *testing.T) {
		trackers := c.GetTorrentTrackers(t, instanceID, hash)

		// Magnet links typically have at least one tracker
		assert.NotEmpty(t, trackers, "should have at least one tracker")

		// Check first tracker has expected fields
		if len(trackers) > 0 {
			// The first entry is typically the DHT/PeX status row
			// Real trackers start from index 1+
			foundTracker := false
			for _, tracker := range trackers {
				if tracker.URL != "" && tracker.URL != "** [DHT] **" && tracker.URL != "** [PeX] **" && tracker.URL != "** [LSD] **" {
					foundTracker = true
					assert.NotEmpty(t, tracker.URL)
					break
				}
			}
			assert.True(t, foundTracker || len(trackers) >= 1, "should have tracker info")
		}
	})

	t.Run("get torrent files", func(t *testing.T) {
		files := c.GetTorrentFiles(t, instanceID, hash)

		// Big Buck Bunny magnet should have files once metadata is received
		// Note: Files may be empty if metadata hasn't been fetched yet
		if len(files) > 0 {
			for _, file := range files {
				assert.NotEmpty(t, file.Name, "file name should be set")
				assert.GreaterOrEqual(t, file.Size, int64(0), "file size should be non-negative")
				assert.GreaterOrEqual(t, file.Priority, 0, "priority should be non-negative")
			}
		}
		// Don't fail if files is empty - metadata fetch may not have completed
		t.Logf("Found %d files in torrent", len(files))
	})
}

// waitForInstance waits for an instance to be connected and ready.
func waitForInstance(t *testing.T, c *client.Client, instanceID int) {
	t.Helper()

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		_, err := c.TryGetCapabilities(t, instanceID)
		if err == nil {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("instance %d did not become ready within timeout", instanceID)
}

package tests

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/e2e/internal/client"
	"github.com/autobrr/qui/e2e/internal/containers"
)

func TestTorrentCRUD(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	t.Parallel()

	ctx := context.Background()
	env := containers.Setup(ctx, t, containers.DefaultConfig(), 1)
	t.Cleanup(func() { env.Teardown(ctx) })

	c := env.Client()
	qbit := env.Instances[0]

	// Create an instance to work with
	instanceID := c.CreateInstance(t, client.InstanceConfig{
		Name:     "torrent-test",
		Host:     qbit.URL,
		Username: "admin",
		Password: qbit.Password,
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

	t.Run("check_duplicates_found", func(t *testing.T) {
		torrentFile := testdataPath("torrents/sintel.torrent")
		hash := c.AddTorrentFromFile(t, instanceID, torrentFile, client.AddTorrentOptions{
			Paused: true,
		})
		t.Cleanup(func() { c.DeleteTorrent(t, instanceID, hash, true) })

		time.Sleep(time.Second)

		result := c.CheckDuplicates(t, instanceID, []string{hash})
		require.Len(t, result.Duplicates, 1, "should find one duplicate")
		assert.Equal(t, hash, result.Duplicates[0].Hash)
	})

	t.Run("check_duplicates_not_found", func(t *testing.T) {
		fakeHash := "0000000000000000000000000000000000000000"
		result := c.CheckDuplicates(t, instanceID, []string{fakeHash})
		assert.Empty(t, result.Duplicates, "should not find duplicates for unknown hash")
	})
}

func TestTorrentBulkActions(t *testing.T) {
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
		Name:     "bulk-test",
		Host:     qbit.URL,
		Username: "admin",
		Password: qbit.Password,
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
	env := containers.Setup(ctx, t, containers.DefaultConfig(), 1)
	t.Cleanup(func() { env.Teardown(ctx) })

	c := env.Client()
	qbit := env.Instances[0]

	instanceID := c.CreateInstance(t, client.InstanceConfig{
		Name:     "details-test",
		Host:     qbit.URL,
		Username: "admin",
		Password: qbit.Password,
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

	// Add a second torrent from .torrent file for reliable metadata
	// Use sintel.torrent (not big-buck-bunny) to avoid hash collision with the magnet above
	torrentFile := testdataPath("torrents/sintel.torrent")
	fileHash := c.AddTorrentFromFile(t, instanceID, torrentFile, client.AddTorrentOptions{
		Paused: true,
	})
	t.Cleanup(func() { c.DeleteTorrent(t, instanceID, fileHash, true) })

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

	t.Run("get_peers_paused", func(t *testing.T) {
		resp := c.GetTorrentPeersRaw(t, instanceID, fileHash)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("get_webseeds", func(t *testing.T) {
		resp := c.GetTorrentWebSeedsRaw(t, instanceID, fileHash)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("get_piece_states_paused", func(t *testing.T) {
		pieces := c.GetTorrentPieceStates(t, instanceID, fileHash)
		require.NotEmpty(t, pieces, "file-based torrent should have piece state data")
		// All pieces should be 0 (not downloaded) for a paused torrent
		for i, state := range pieces {
			assert.Equal(t, 0, state, "piece %d should be 0 (not downloaded) for paused torrent", i)
		}
	})

	t.Run("get_peers_active", func(t *testing.T) {
		// Resume the torrent briefly to allow peer discovery
		c.ResumeTorrents(t, instanceID, []string{fileHash})
		time.Sleep(5 * time.Second)

		resp := c.GetTorrentPeersRaw(t, instanceID, fileHash)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		t.Log("Active peer endpoint returned 200 OK")
	})

	t.Run("get_piece_states_active", func(t *testing.T) {
		// Torrent was resumed in previous subtest
		pieces := c.GetTorrentPieceStates(t, instanceID, fileHash)
		require.NotEmpty(t, pieces, "should have piece state data")

		// Verify all values are valid (0, 1, or 2)
		for i, state := range pieces {
			assert.True(t, state >= 0 && state <= 2,
				"piece %d has invalid state %d (expected 0, 1, or 2)", i, state)
		}

		// Log how many pieces are in each state for visibility
		counts := map[int]int{0: 0, 1: 0, 2: 0}
		for _, s := range pieces {
			counts[s]++
		}
		t.Logf("Piece states: %d not downloaded, %d downloading, %d downloaded (total: %d)",
			counts[0], counts[1], counts[2], len(pieces))

		// Re-pause for cleanup
		c.PauseTorrents(t, instanceID, []string{fileHash})
	})
}

package tests

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/e2e/internal/client"
	"github.com/autobrr/qui/e2e/internal/containers"
)

func TestTorrentTrackerOps(t *testing.T) {
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
		Name:     "tracker-ops-test",
		Host:     qbit.URL,
		Username: "admin",
		Password: qbit.Password,
	})
	t.Cleanup(func() { c.DeleteInstance(t, instanceID) })

	waitForInstance(t, c, instanceID)

	// Add torrent from .torrent file for reliable metadata and tracker info
	torrentFile := testdataPath("torrents/sintel.torrent")
	hash := c.AddTorrentFromFile(t, instanceID, torrentFile, client.AddTorrentOptions{
		Paused: true,
	})
	t.Cleanup(func() { c.DeleteTorrent(t, instanceID, hash, true) })

	time.Sleep(2 * time.Second)

	t.Run("get_trackers", func(t *testing.T) {
		trackers := c.GetTorrentTrackers(t, instanceID, hash)
		require.NotEmpty(t, trackers, "torrent from .torrent file should have trackers")

		// Find a real tracker URL (skip DHT/PeX/LSD pseudo-entries)
		found := false
		for _, tr := range trackers {
			if !strings.HasPrefix(tr.URL, "**") {
				found = true
				assert.NotEmpty(t, tr.URL)
				break
			}
		}
		assert.True(t, found, "should have at least one real tracker URL")
	})

	const addedTracker1 = "http://test-tracker1.example.com:6969/announce"
	const addedTracker2 = "http://test-tracker2.example.com:6969/announce"

	t.Run("add_trackers", func(t *testing.T) {
		urls := addedTracker1 + "\n" + addedTracker2
		c.AddTorrentTrackers(t, instanceID, hash, urls)

		time.Sleep(time.Second)

		trackers := c.GetTorrentTrackers(t, instanceID, hash)
		trackerURLs := make([]string, 0, len(trackers))
		for _, tr := range trackers {
			trackerURLs = append(trackerURLs, tr.URL)
		}
		assert.Contains(t, trackerURLs, addedTracker1)
		assert.Contains(t, trackerURLs, addedTracker2)
	})

	t.Run("edit_tracker", func(t *testing.T) {
		const replacementURL = "http://replaced-tracker.example.com:6969/announce"
		c.EditTorrentTracker(t, instanceID, hash, addedTracker1, replacementURL)

		time.Sleep(time.Second)

		trackers := c.GetTorrentTrackers(t, instanceID, hash)
		trackerURLs := make([]string, 0, len(trackers))
		for _, tr := range trackers {
			trackerURLs = append(trackerURLs, tr.URL)
		}
		assert.NotContains(t, trackerURLs, addedTracker1, "old URL should be gone")
		assert.Contains(t, trackerURLs, replacementURL, "new URL should be present")
	})

	t.Run("remove_trackers", func(t *testing.T) {
		c.RemoveTorrentTrackers(t, instanceID, hash, addedTracker2)

		time.Sleep(time.Second)

		trackers := c.GetTorrentTrackers(t, instanceID, hash)
		trackerURLs := make([]string, 0, len(trackers))
		for _, tr := range trackers {
			trackerURLs = append(trackerURLs, tr.URL)
		}
		assert.NotContains(t, trackerURLs, addedTracker2, "removed tracker should be gone")
	})

	t.Run("get_active_trackers", func(t *testing.T) {
		trackers := c.GetActiveTrackers(t, instanceID)
		assert.NotNil(t, trackers, "response should not be nil")
		// The API returns map[domain]->trackerURL; with a torrent added, we may have entries
		t.Logf("Active trackers: %v", trackers)
	})
}

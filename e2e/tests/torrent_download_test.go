//go:build download

package tests

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/e2e/internal/client"
	"github.com/autobrr/qui/e2e/internal/containers"
)

// getTestdataPath returns the absolute path to a testdata file.
func getTestdataPath(relativePath string) string {
	_, filename, _, _ := runtime.Caller(0)
	testDir := filepath.Dir(filename)
	return filepath.Join(testDir, "..", "testdata", relativePath)
}

// Small magnet for testing (Big Buck Bunny - same as torrent_test.go)
const downloadTestMagnet = "magnet:?xt=urn:btih:dd8255ecdc7ca55fb0bbf81323d87062db1f6d1c&dn=Big+Buck+Bunny&tr=udp%3A%2F%2Ftracker.opentrackr.org%3A1337"

func TestTorrentDownload(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping download test in short mode")
	}

	ctx := context.Background()
	cfg := containers.DefaultConfig()
	cfg.Timeout = 15 * time.Minute // Longer timeout for downloads

	env := containers.Setup(ctx, t, cfg, 1)
	t.Cleanup(func() { env.Teardown(ctx) })

	c := env.Client()
	qbit := env.Instances[0]

	// Create instance
	instanceID := c.CreateInstance(t, client.InstanceConfig{
		Name:     "download-test",
		Host:     qbit.URL,
		Username: "admin",
		Password: qbit.Password,
	})
	t.Cleanup(func() { c.DeleteInstance(t, instanceID) })

	waitForInstance(t, c, instanceID)

	// Add torrents - one from file, one from magnet
	torrentFile := getTestdataPath("torrents/wired-cd.torrent")
	hashFile := c.AddTorrentFromFile(t, instanceID, torrentFile, client.AddTorrentOptions{
		Paused: false, // Start downloading immediately
	})
	t.Cleanup(func() { c.DeleteTorrent(t, instanceID, hashFile, true) })

	hashMagnet := c.AddTorrentFromMagnet(t, instanceID, downloadTestMagnet, client.AddTorrentOptions{
		Paused: false,
	})
	t.Cleanup(func() { c.DeleteTorrent(t, instanceID, hashMagnet, true) })

	t.Logf("Added torrents - file: %s, magnet: %s", hashFile, hashMagnet)

	// Run parallel verifications during download
	t.Run("parallel_download_verification", func(t *testing.T) {
		t.Run("file_progress_increases", func(t *testing.T) {
			t.Parallel()
			// Wait to see progress > 0 (proves download started)
			torrent := c.WaitForCondition(t, instanceID, hashFile, 5*time.Minute, func(tor client.Torrent) bool {
				return tor.Progress > 0.1
			})
			t.Logf("File torrent progress reached %.2f%%", torrent.Progress*100)
		})

		t.Run("file_download_speed_observed", func(t *testing.T) {
			t.Parallel()
			// Wait to see download speed > 0
			c.WaitForCondition(t, instanceID, hashFile, 5*time.Minute, func(tor client.Torrent) bool {
				if tor.DlSpeed > 0 {
					t.Logf("Observed download speed: %d bytes/sec", tor.DlSpeed)
					return true
				}
				return false
			})
		})

		t.Run("file_state_downloading", func(t *testing.T) {
			t.Parallel()
			// Wait to see downloading state
			c.WaitForCondition(t, instanceID, hashFile, 5*time.Minute, func(tor client.Torrent) bool {
				isDownloading := strings.Contains(strings.ToLower(tor.State), "download")
				if isDownloading {
					t.Logf("Observed downloading state: %s", tor.State)
				}
				return isDownloading
			})
		})

		t.Run("magnet_metadata_received", func(t *testing.T) {
			t.Parallel()
			// Magnet needs to fetch metadata first - wait for name to appear
			c.WaitForCondition(t, instanceID, hashMagnet, 5*time.Minute, func(tor client.Torrent) bool {
				hasName := tor.Name != "" && !strings.HasPrefix(tor.Name, "magnet:")
				if hasName {
					t.Logf("Magnet metadata received, name: %s", tor.Name)
				}
				return hasName
			})
		})
	})

	// Wait for file torrent to complete
	t.Run("file_download_completes", func(t *testing.T) {
		torrent := c.WaitForCondition(t, instanceID, hashFile, 10*time.Minute, func(tor client.Torrent) bool {
			return tor.Progress >= 1.0
		})
		assert.Equal(t, 1.0, torrent.Progress)
		t.Logf("File torrent completed, state: %s", torrent.State)
	})

	// Verify final state
	t.Run("verify_completed_state", func(t *testing.T) {
		torrent := c.GetTorrent(t, instanceID, hashFile)

		assert.Equal(t, 1.0, torrent.Progress, "should be 100% complete")
		assert.Greater(t, torrent.Size, int64(0), "should have size")
		assert.NotEmpty(t, torrent.SavePath, "should have save path")
	})

	t.Run("verify_files_exist", func(t *testing.T) {
		files := c.GetTorrentFiles(t, instanceID, hashFile)

		require.NotEmpty(t, files, "should have files")
		for _, file := range files {
			assert.NotEmpty(t, file.Name, "file should have name")
			assert.Greater(t, file.Size, int64(0), "file should have size")
			assert.Equal(t, 1.0, file.Progress, "file should be complete")
		}
		t.Logf("Torrent has %d files", len(files))
	})
}

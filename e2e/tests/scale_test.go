//go:build scale

package tests

import (
	"context"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/e2e/internal/client"
	"github.com/autobrr/qui/e2e/internal/containers"
)

// getEnvInt returns the environment variable as an int, or the default if not set.
func getEnvInt(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}
	return defaultVal
}

// TestScaleManyInstances tests qui with many qBittorrent instances.
// Configure instance count with QUI_E2E_SCALE_INSTANCES env var (default: 10).
//
// Run with: go test -v -tags=scale -run TestScaleManyInstances ./tests/...
// Stress test: QUI_E2E_SCALE_INSTANCES=50 go test -v -tags=scale -run TestScaleManyInstances ./tests/...
func TestScaleManyInstances(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping scale test in short mode")
	}
	// Don't run in parallel with other tests - this is resource intensive
	// Note: not calling t.Parallel() intentionally

	instanceCount := getEnvInt("QUI_E2E_SCALE_INSTANCES", 10)
	t.Logf("Testing with %d qBittorrent instances", instanceCount)

	ctx := context.Background()
	cfg := containers.DefaultConfig()
	cfg.Timeout = 5 * time.Minute // Longer timeout for many containers

	env := containers.SetupMultiInstance(ctx, t, cfg, instanceCount)
	t.Cleanup(func() { env.Teardown(ctx) })

	c := env.Client()

	// Wait for all instances to be connected
	t.Run("all_instances_connected", func(t *testing.T) {
		for i, inst := range env.Instances {
			t.Logf("Waiting for instance %d (ID: %d)...", i+1, inst.ID)
			waitForInstance(t, c, inst.ID)
		}
		t.Logf("All %d instances connected successfully", instanceCount)
	})

	t.Run("add_torrent_to_each", func(t *testing.T) {
		hashes := make([]string, instanceCount)

		// Add torrent to each instance concurrently
		var wg sync.WaitGroup
		var mu sync.Mutex
		errors := make([]error, instanceCount)

		for i, inst := range env.Instances {
			wg.Add(1)
			go func(idx int, instance *containers.QBitInstance) {
				defer wg.Done()
				defer func() {
					if r := recover(); r != nil {
						mu.Lock()
						if err, ok := r.(error); ok {
							errors[idx] = err
						}
						mu.Unlock()
					}
				}()
				hash := c.AddTorrentFromMagnet(t, instance.ID, testMagnet, client.AddTorrentOptions{
					Paused: true,
				})
				mu.Lock()
				hashes[idx] = hash
				mu.Unlock()
			}(i, inst)
		}

		wg.Wait()

		// Check for errors
		for i, err := range errors {
			require.NoError(t, err, "adding torrent to instance %d should succeed", i+1)
		}

		// Verify all hashes are the same (same magnet link)
		for i, hash := range hashes {
			if i > 0 {
				assert.Equal(t, hashes[0], hash, "all hashes should match")
			}
		}

		// Cleanup
		t.Cleanup(func() {
			for i, inst := range env.Instances {
				if hashes[i] != "" {
					c.DeleteTorrent(t, inst.ID, hashes[i], true)
				}
			}
		})

		t.Logf("Successfully added torrent to all %d instances", instanceCount)
	})

	t.Run("cross_instance_query", func(t *testing.T) {
		// Add torrent to each instance
		hashes := make([]string, instanceCount)
		for i, inst := range env.Instances {
			hashes[i] = c.AddTorrentFromMagnet(t, inst.ID, testMagnet, client.AddTorrentOptions{
				Paused: true,
			})
		}
		t.Cleanup(func() {
			for i, inst := range env.Instances {
				if hashes[i] != "" {
					c.DeleteTorrent(t, inst.ID, hashes[i], true)
				}
			}
		})

		time.Sleep(2 * time.Second) // Wait for sync

		// Query across all instances
		result := c.ListCrossInstanceTorrents(t, "name ~ /.*/")

		assert.True(t, result.IsCrossInstance, "should be marked as cross-instance")
		// Each instance has the same torrent, so we expect at least instanceCount matches
		// (may be deduplicated or not depending on implementation)
		assert.GreaterOrEqual(t, result.Total, 1, "should find torrents across instances")
		t.Logf("Cross-instance query returned %d torrents from %d instances", result.Total, instanceCount)
	})

	t.Run("concurrent_operations_at_scale", func(t *testing.T) {
		// Add torrent to each instance
		hashes := make([]string, instanceCount)
		for i, inst := range env.Instances {
			hashes[i] = c.AddTorrentFromMagnet(t, inst.ID, testMagnet, client.AddTorrentOptions{
				Paused: true,
			})
		}
		t.Cleanup(func() {
			for i, inst := range env.Instances {
				if hashes[i] != "" {
					c.DeleteTorrent(t, inst.ID, hashes[i], true)
				}
			}
		})

		time.Sleep(time.Second)

		// Pause/resume all concurrently
		var wg sync.WaitGroup
		for i, inst := range env.Instances {
			wg.Add(1)
			go func(idx int, instance *containers.QBitInstance) {
				defer wg.Done()
				// Alternate between pause and resume
				if idx%2 == 0 {
					c.PauseTorrents(t, instance.ID, []string{hashes[idx]})
				} else {
					c.ResumeTorrents(t, instance.ID, []string{hashes[idx]})
				}
			}(i, inst)
		}
		wg.Wait()

		t.Logf("Successfully performed concurrent operations on %d instances", instanceCount)
	})

	t.Run("verify_isolation_at_scale", func(t *testing.T) {
		// Create unique category on first instance only
		uniqueCat := "scale-test-unique-cat"
		c.CreateCategory(t, env.Instances[0].ID, uniqueCat, "/downloads/scale")
		t.Cleanup(func() { c.DeleteCategory(t, env.Instances[0].ID, uniqueCat) })

		time.Sleep(500 * time.Millisecond)

		// Verify category only exists on first instance
		cats0 := c.GetCategories(t, env.Instances[0].ID)
		_, exists0 := cats0[uniqueCat]
		assert.True(t, exists0, "category should exist on first instance")

		// Check a few other instances to verify isolation
		checkCount := min(5, instanceCount-1)
		for i := 1; i <= checkCount; i++ {
			cats := c.GetCategories(t, env.Instances[i].ID)
			_, exists := cats[uniqueCat]
			assert.False(t, exists, "category should NOT exist on instance %d", i+1)
		}

		t.Logf("Verified data isolation across %d instances", checkCount+1)
	})
}

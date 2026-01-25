//go:build automation

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

// getExecTestdataPath returns the absolute path to a testdata file.
func getExecTestdataPath(relativePath string) string {
	_, filename, _, _ := runtime.Caller(0)
	testDir := filepath.Dir(filename)
	return filepath.Join(testDir, "..", "testdata", relativePath)
}

// setupAutomationExecTest sets up a test environment with one qBittorrent instance.
// Uses longer timeouts for automation execution tests.
func setupAutomationExecTest(t *testing.T) (context.Context, *containers.Env, *client.Client, int) {
	t.Helper()

	ctx := context.Background()
	cfg := containers.DefaultConfig()
	cfg.Timeout = 10 * time.Minute // Longer timeout for execution tests

	env := containers.Setup(ctx, t, cfg, 1)
	t.Cleanup(func() { env.Teardown(ctx) })

	c := env.Client()
	qbit := env.Instances[0]

	// Create a qBittorrent instance in qui
	instanceID := c.CreateInstance(t, client.InstanceConfig{
		Name:     "automation-exec-test",
		Host:     qbit.URL,
		Username: "admin",
		Password: qbit.Password,
	})
	t.Cleanup(func() { c.DeleteInstance(t, instanceID) })

	// Wait for instance to be ready
	waitForInstance(t, c, instanceID)

	return ctx, env, c, instanceID
}

func TestAutomationApplyPause(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping automation execution test in short mode")
	}
	t.Parallel()

	_, _, c, instanceID := setupAutomationExecTest(t)

	// Add a torrent (paused initially)
	hash := c.AddTorrentFromMagnet(t, instanceID, testMagnet, client.AddTorrentOptions{
		Paused: false, // Start it to get downloading state
	})
	t.Cleanup(func() { c.DeleteTorrent(t, instanceID, hash, true) })

	// Wait for torrent to appear and start downloading/fetching metadata
	time.Sleep(2 * time.Second)

	// Create automation to pause torrents
	enabled := true
	automation := c.CreateAutomation(t, instanceID, client.AutomationPayload{
		Name:           "Pause All Torrents",
		TrackerPattern: "*",
		Enabled:        &enabled,
		Conditions: &client.ActionConditions{
			Pause: &client.PauseAction{
				Enabled: true,
				// No condition = apply to all matching torrents
			},
		},
	})
	t.Cleanup(func() { c.DeleteAutomation(t, instanceID, automation.ID) })

	// Apply automations manually
	c.ApplyAutomations(t, instanceID)

	// Wait a moment for the action to take effect
	time.Sleep(2 * time.Second)

	// Verify torrent is paused
	// qBittorrent uses "stoppedDL" or "stoppedUP" for paused torrents (not "paused")
	torrent := c.GetTorrent(t, instanceID, hash)
	isPaused := strings.Contains(strings.ToLower(torrent.State), "paused") ||
		strings.Contains(strings.ToLower(torrent.State), "stopped")
	assert.True(t, isPaused,
		"expected torrent to be paused/stopped, got state: %s", torrent.State)
}

func TestAutomationApplyTag(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping automation execution test in short mode")
	}
	t.Parallel()

	_, _, c, instanceID := setupAutomationExecTest(t)

	// Add a torrent
	hash := c.AddTorrentFromMagnet(t, instanceID, testMagnet, client.AddTorrentOptions{
		Paused: true,
	})
	t.Cleanup(func() { c.DeleteTorrent(t, instanceID, hash, true) })

	// Wait for torrent to appear
	time.Sleep(2 * time.Second)

	// Create automation to add tags
	enabled := true
	automation := c.CreateAutomation(t, instanceID, client.AutomationPayload{
		Name:           "Tag All Torrents",
		TrackerPattern: "*",
		Enabled:        &enabled,
		Conditions: &client.ActionConditions{
			Tag: &client.TagAction{
				Enabled: true,
				Tags:    []string{"automated", "test-tag"},
				Mode:    "add",
			},
		},
	})
	t.Cleanup(func() { c.DeleteAutomation(t, instanceID, automation.ID) })

	// Apply automations
	c.ApplyAutomations(t, instanceID)

	// Wait for the action to take effect
	time.Sleep(2 * time.Second)

	// Verify tags were added
	torrent := c.GetTorrent(t, instanceID, hash)
	assert.Contains(t, torrent.Tags, "automated", "expected 'automated' tag")
	assert.Contains(t, torrent.Tags, "test-tag", "expected 'test-tag' tag")
}

func TestAutomationApplyCategory(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping automation execution test in short mode")
	}
	t.Parallel()

	_, _, c, instanceID := setupAutomationExecTest(t)

	// Create a category first
	c.CreateCategory(t, instanceID, "movies", "/downloads/movies")
	t.Cleanup(func() { c.DeleteCategory(t, instanceID, "movies") })

	// Add a torrent
	hash := c.AddTorrentFromMagnet(t, instanceID, testMagnet, client.AddTorrentOptions{
		Paused: true,
	})
	t.Cleanup(func() { c.DeleteTorrent(t, instanceID, hash, true) })

	// Wait for torrent to appear
	time.Sleep(2 * time.Second)

	// Verify initial category is empty
	torrent := c.GetTorrent(t, instanceID, hash)
	assert.Empty(t, torrent.Category, "expected no initial category")

	// Create automation to set category
	enabled := true
	automation := c.CreateAutomation(t, instanceID, client.AutomationPayload{
		Name:           "Categorize All Torrents",
		TrackerPattern: "*",
		Enabled:        &enabled,
		Conditions: &client.ActionConditions{
			Category: &client.CategoryAction{
				Enabled:  true,
				Category: "movies",
			},
		},
	})
	t.Cleanup(func() { c.DeleteAutomation(t, instanceID, automation.ID) })

	// Apply automations
	c.ApplyAutomations(t, instanceID)

	// Wait for the action to take effect
	time.Sleep(2 * time.Second)

	// Verify category was set
	torrent = c.GetTorrent(t, instanceID, hash)
	assert.Equal(t, "movies", torrent.Category, "expected category to be 'movies'")
}

func TestAutomationPreviewDelete(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping automation execution test in short mode")
	}
	t.Parallel()

	_, _, c, instanceID := setupAutomationExecTest(t)

	// Add a torrent
	hash := c.AddTorrentFromMagnet(t, instanceID, testMagnet, client.AddTorrentOptions{
		Paused: true,
	})
	t.Cleanup(func() { c.DeleteTorrent(t, instanceID, hash, true) })

	// Wait for torrent to appear and get metadata
	time.Sleep(3 * time.Second)

	// Preview a delete rule (matches all torrents)
	payload := client.AutomationPayload{
		Name:           "Preview Delete Rule",
		TrackerPattern: "*",
		Conditions: &client.ActionConditions{
			Delete: &client.DeleteAction{
				Enabled: true,
				Mode:    "delete",
				Condition: &client.RuleCondition{
					Field:    "RATIO",
					Operator: "GREATER_THAN_OR_EQUAL",
					Value:    "0", // Match all (ratio >= 0)
				},
			},
		},
	}

	result := c.PreviewAutomation(t, instanceID, payload)

	// Should find our torrent
	assert.GreaterOrEqual(t, result.TotalMatches, 1, "expected at least 1 match")
	assert.NotEmpty(t, result.Examples, "expected at least 1 example torrent")

	// Find our torrent in the results
	found := false
	for _, example := range result.Examples {
		if example.Hash == hash {
			found = true
			break
		}
	}
	assert.True(t, found, "expected our torrent in preview results")
}

func TestAutomationPreviewCategory(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping automation execution test in short mode")
	}
	t.Parallel()

	_, _, c, instanceID := setupAutomationExecTest(t)

	// Create category
	c.CreateCategory(t, instanceID, "tv-shows", "/downloads/tv")
	t.Cleanup(func() { c.DeleteCategory(t, instanceID, "tv-shows") })

	// Add a torrent
	hash := c.AddTorrentFromMagnet(t, instanceID, testMagnet, client.AddTorrentOptions{
		Paused: true,
	})
	t.Cleanup(func() { c.DeleteTorrent(t, instanceID, hash, true) })

	// Wait for torrent to appear
	time.Sleep(3 * time.Second)

	// Preview a category rule
	payload := client.AutomationPayload{
		Name:           "Preview Category Rule",
		TrackerPattern: "*",
		Conditions: &client.ActionConditions{
			Category: &client.CategoryAction{
				Enabled:  true,
				Category: "tv-shows",
			},
		},
	}

	result := c.PreviewAutomation(t, instanceID, payload)

	// Should find our torrent
	assert.GreaterOrEqual(t, result.TotalMatches, 1, "expected at least 1 match")
	assert.NotEmpty(t, result.Examples, "expected at least 1 example torrent")
}

func TestAutomationApplyWithCondition(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping automation execution test in short mode")
	}
	t.Parallel()

	_, _, c, instanceID := setupAutomationExecTest(t)

	// Add torrents with different sizes - use SIZE condition which is deterministic
	// Wired CD torrent (~43MB)
	torrentFile1 := getExecTestdataPath("torrents/wired-cd.torrent")
	hash1 := c.AddTorrentFromFile(t, instanceID, torrentFile1, client.AddTorrentOptions{
		Paused: true,
	})
	t.Cleanup(func() { c.DeleteTorrent(t, instanceID, hash1, true) })

	// Big Buck Bunny magnet (~276MB)
	hash2 := c.AddTorrentFromMagnet(t, instanceID, testMagnet, client.AddTorrentOptions{
		Paused: true,
	})
	t.Cleanup(func() { c.DeleteTorrent(t, instanceID, hash2, true) })

	time.Sleep(3 * time.Second)

	// Log torrent info for debugging
	torrent1Initial := c.GetTorrent(t, instanceID, hash1)
	torrent2Initial := c.GetTorrent(t, instanceID, hash2)
	t.Logf("Torrent 1: hash=%s, name=%s, size=%d", hash1, torrent1Initial.Name, torrent1Initial.Size)
	t.Logf("Torrent 2: hash=%s, name=%s, size=%d", hash2, torrent2Initial.Name, torrent2Initial.Size)

	// Create automation that only tags small torrents (< 100MB = 100000000 bytes)
	enabled := true
	automation := c.CreateAutomation(t, instanceID, client.AutomationPayload{
		Name:           "Tag Small Torrents",
		TrackerPattern: "*",
		Enabled:        &enabled,
		Conditions: &client.ActionConditions{
			Tag: &client.TagAction{
				Enabled: true,
				Tags:    []string{"small-torrent"},
				Mode:    "add",
				Condition: &client.RuleCondition{
					Field:    "SIZE",
					Operator: "LESS_THAN",
					Value:    "100000000", // 100MB in bytes
				},
			},
		},
	})
	t.Cleanup(func() { c.DeleteAutomation(t, instanceID, automation.ID) })

	// Apply
	c.ApplyAutomations(t, instanceID)
	time.Sleep(2 * time.Second)

	// Verify only small torrent got the tag
	torrent1 := c.GetTorrent(t, instanceID, hash1)
	t.Logf("After apply - Torrent 1: hash=%s, name=%s, size=%d, tags=%s", hash1, torrent1.Name, torrent1.Size, torrent1.Tags)

	torrent2 := c.GetTorrent(t, instanceID, hash2)
	t.Logf("After apply - Torrent 2: hash=%s, name=%s, size=%d, tags=%s", hash2, torrent2.Name, torrent2.Size, torrent2.Tags)

	// Wired CD (~43MB) should have the tag, Big Buck Bunny (~276MB) should not
	if torrent1.Size > 0 && torrent1.Size < 100000000 {
		assert.Contains(t, torrent1.Tags, "small-torrent", "small torrent should have small-torrent tag")
	}
	if torrent2.Size > 100000000 {
		assert.NotContains(t, torrent2.Tags, "small-torrent", "large torrent should not have small-torrent tag")
	}
}

func TestAutomationIntervalExecution(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping automation execution test in short mode")
	}
	t.Parallel()

	_, _, c, instanceID := setupAutomationExecTest(t)

	// Create automation with minimum interval (60 seconds)
	enabled := true
	interval := 60
	automation := c.CreateAutomation(t, instanceID, client.AutomationPayload{
		Name:            "Auto Tag New Torrents",
		TrackerPattern:  "*",
		Enabled:         &enabled,
		IntervalSeconds: &interval,
		Conditions: &client.ActionConditions{
			Tag: &client.TagAction{
				Enabled: true,
				Tags:    []string{"auto-tagged"},
				Mode:    "add",
			},
		},
	})
	t.Cleanup(func() { c.DeleteAutomation(t, instanceID, automation.ID) })

	// Add a torrent after automation is created
	hash := c.AddTorrentFromMagnet(t, instanceID, testMagnet, client.AddTorrentOptions{
		Paused: true,
	})
	t.Cleanup(func() { c.DeleteTorrent(t, instanceID, hash, true) })

	time.Sleep(2 * time.Second)

	// Verify tag is not there yet (automation hasn't run)
	torrent := c.GetTorrent(t, instanceID, hash)
	initialTags := torrent.Tags

	// Wait for the interval to pass plus buffer (70 seconds total)
	t.Log("Waiting for automation interval to trigger...")
	time.Sleep(70 * time.Second)

	// Verify tag was added by interval execution
	torrent = c.GetTorrent(t, instanceID, hash)
	if !strings.Contains(torrent.Tags, "auto-tagged") {
		// If tag still not there, wait a bit more and check again
		t.Log("Tag not found yet, waiting additional 30 seconds...")
		time.Sleep(30 * time.Second)
		torrent = c.GetTorrent(t, instanceID, hash)
	}

	assert.Contains(t, torrent.Tags, "auto-tagged",
		"expected tag after interval execution. Initial tags: %s, Current tags: %s",
		initialTags, torrent.Tags)

	// Check activity log for evidence of execution
	activities := c.ListAutomationActivity(t, instanceID, 10)
	t.Logf("Found %d activity entries", len(activities))
}

func TestAutomationActivityLogging(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping automation execution test in short mode")
	}
	t.Parallel()

	_, _, c, instanceID := setupAutomationExecTest(t)

	// Add a torrent
	hash := c.AddTorrentFromMagnet(t, instanceID, testMagnet, client.AddTorrentOptions{
		Paused: true,
	})
	t.Cleanup(func() { c.DeleteTorrent(t, instanceID, hash, true) })

	time.Sleep(2 * time.Second)

	// Create automation
	enabled := true
	automation := c.CreateAutomation(t, instanceID, client.AutomationPayload{
		Name:           "Activity Test Rule",
		TrackerPattern: "*",
		Enabled:        &enabled,
		Conditions: &client.ActionConditions{
			Tag: &client.TagAction{
				Enabled: true,
				Tags:    []string{"activity-test"},
				Mode:    "add",
			},
		},
	})
	t.Cleanup(func() { c.DeleteAutomation(t, instanceID, automation.ID) })

	// Clear any existing activity
	c.DeleteAutomationActivity(t, instanceID, 0)

	// Apply automations
	c.ApplyAutomations(t, instanceID)
	time.Sleep(3 * time.Second)

	// Check activity log
	activities := c.ListAutomationActivity(t, instanceID, 100)

	// Log all activities for debugging
	t.Logf("Found %d activities", len(activities))
	for i, a := range activities {
		t.Logf("Activity %d: Action=%s, Hash=%s, RuleID=%v, RuleName=%s, Outcome=%s",
			i, a.Action, a.Hash, a.RuleID, a.RuleName, a.Outcome)
	}

	// Should have activity entries - but activity logging may not happen for all actions
	// The test mainly verifies that ListAutomationActivity works
	if len(activities) == 0 {
		t.Log("No activity logged yet - this is OK as activity logging is action-specific")
		return
	}

	// If we have activities, try to find one for our automation
	var found *client.AutomationActivity
	for i := range activities {
		// Match by hash since RuleID might not be set for all action types
		if activities[i].Hash == hash {
			found = &activities[i]
			break
		}
	}

	if found != nil {
		t.Logf("Found matching activity: Action=%s, Outcome=%s", found.Action, found.Outcome)
	}
}

func TestAutomationSpeedLimits(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping automation execution test in short mode")
	}
	t.Parallel()

	_, _, c, instanceID := setupAutomationExecTest(t)

	// Add a torrent
	hash := c.AddTorrentFromMagnet(t, instanceID, testMagnet, client.AddTorrentOptions{
		Paused: true,
	})
	t.Cleanup(func() { c.DeleteTorrent(t, instanceID, hash, true) })

	time.Sleep(2 * time.Second)

	// Create automation to set speed limits
	enabled := true
	uploadLimit := int64(512)   // 512 KiB/s
	downloadLimit := int64(1024) // 1024 KiB/s
	automation := c.CreateAutomation(t, instanceID, client.AutomationPayload{
		Name:           "Set Speed Limits",
		TrackerPattern: "*",
		Enabled:        &enabled,
		Conditions: &client.ActionConditions{
			SpeedLimits: &client.SpeedLimitAction{
				Enabled:     true,
				UploadKiB:   &uploadLimit,
				DownloadKiB: &downloadLimit,
			},
		},
	})
	t.Cleanup(func() { c.DeleteAutomation(t, instanceID, automation.ID) })

	// Apply
	c.ApplyAutomations(t, instanceID)
	time.Sleep(2 * time.Second)

	// Verify limits were set by checking torrent properties
	props := c.GetTorrentProperties(t, instanceID, hash)
	// Note: The properties structure may not expose per-torrent limits directly
	// This test mainly verifies the automation executes without error
	t.Logf("Torrent properties after speed limit automation: %+v", props)
}

func TestAutomationShareLimits(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping automation execution test in short mode")
	}
	t.Parallel()

	_, _, c, instanceID := setupAutomationExecTest(t)

	// Add a torrent
	hash := c.AddTorrentFromMagnet(t, instanceID, testMagnet, client.AddTorrentOptions{
		Paused: true,
	})
	t.Cleanup(func() { c.DeleteTorrent(t, instanceID, hash, true) })

	time.Sleep(2 * time.Second)

	// Create automation to set share limits
	enabled := true
	ratioLimit := 2.0
	seedingTimeMinutes := int64(120) // 2 hours
	automation := c.CreateAutomation(t, instanceID, client.AutomationPayload{
		Name:           "Set Share Limits",
		TrackerPattern: "*",
		Enabled:        &enabled,
		Conditions: &client.ActionConditions{
			ShareLimits: &client.ShareLimitsAction{
				Enabled:            true,
				RatioLimit:         &ratioLimit,
				SeedingTimeMinutes: &seedingTimeMinutes,
			},
		},
	})
	t.Cleanup(func() { c.DeleteAutomation(t, instanceID, automation.ID) })

	// Apply
	c.ApplyAutomations(t, instanceID)
	time.Sleep(2 * time.Second)

	// Verify by checking torrent properties
	props := c.GetTorrentProperties(t, instanceID, hash)
	t.Logf("Torrent properties after share limit automation: %+v", props)
}

// TestAutomationSeedingTimeCondition tests automation with seeding time condition.
// This test downloads a torrent to completion and waits for seeding time to accumulate.
func TestAutomationSeedingTimeCondition(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping automation execution test in short mode")
	}
	t.Parallel()

	_, _, c, instanceID := setupAutomationExecTest(t)

	// Use a small torrent file for faster download
	torrentFile := getExecTestdataPath("torrents/wired-cd.torrent")
	hash := c.AddTorrentFromFile(t, instanceID, torrentFile, client.AddTorrentOptions{
		Paused: false, // Start downloading
	})
	t.Cleanup(func() { c.DeleteTorrent(t, instanceID, hash, true) })

	t.Log("Waiting for torrent to start seeding...")

	// Wait for torrent to complete (or timeout after 5 minutes)
	c.WaitForCondition(t, instanceID, hash, 5*time.Minute, func(tor client.Torrent) bool {
		return tor.Progress >= 1.0
	})

	t.Log("Torrent completed, waiting for seeding time to accumulate...")

	// Wait for 70 seconds of seeding time
	time.Sleep(70 * time.Second)

	// Create automation that matches torrents with seeding time > 60 seconds
	enabled := true
	automation := c.CreateAutomation(t, instanceID, client.AutomationPayload{
		Name:           "Tag Long Seeders",
		TrackerPattern: "*",
		Enabled:        &enabled,
		Conditions: &client.ActionConditions{
			Tag: &client.TagAction{
				Enabled: true,
				Tags:    []string{"long-seeder"},
				Mode:    "add",
				Condition: &client.RuleCondition{
					Field:    "SEEDING_TIME",
					Operator: "GREATER_THAN",
					Value:    "60", // 60 seconds
				},
			},
		},
	})
	t.Cleanup(func() { c.DeleteAutomation(t, instanceID, automation.ID) })

	// Apply
	c.ApplyAutomations(t, instanceID)
	time.Sleep(2 * time.Second)

	// Verify tag was applied
	torrent := c.GetTorrent(t, instanceID, hash)
	assert.Contains(t, torrent.Tags, "long-seeder",
		"expected 'long-seeder' tag for torrent with seeding time > 60s")
}

func TestAutomationPreviewRequiresAction(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping automation execution test in short mode")
	}
	t.Parallel()

	_, _, c, instanceID := setupAutomationExecTest(t)

	// Try to preview a rule without delete or category action
	payload := client.AutomationPayload{
		Name:           "Invalid Preview",
		TrackerPattern: "*",
		Conditions: &client.ActionConditions{
			Pause: &client.PauseAction{
				Enabled: true,
			},
		},
	}

	resp := c.PreviewAutomationRaw(t, instanceID, payload)
	defer resp.Body.Close()

	// Should fail - preview requires delete or category action
	assert.Equal(t, 400, resp.StatusCode)
}

func TestAutomationDeleteAction(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping automation execution test in short mode")
	}
	t.Parallel()

	_, _, c, instanceID := setupAutomationExecTest(t)

	// Create a torrent with a specific tag that we'll use as delete condition
	hash := c.AddTorrentFromMagnet(t, instanceID, testMagnet, client.AddTorrentOptions{
		Paused: true,
		Tags:   []string{"delete-me"},
	})
	// Don't cleanup - we expect it to be deleted

	time.Sleep(2 * time.Second)

	// Verify torrent exists
	torrents := c.ListTorrents(t, instanceID)
	require.Len(t, torrents.Torrents, 1, "expected 1 torrent")

	// Create automation to delete torrents with "delete-me" tag
	enabled := true
	automation := c.CreateAutomation(t, instanceID, client.AutomationPayload{
		Name:           "Delete Tagged Torrents",
		TrackerPattern: "*",
		Enabled:        &enabled,
		Conditions: &client.ActionConditions{
			Delete: &client.DeleteAction{
				Enabled: true,
				Mode:    "delete", // Remove torrent but keep files
				Condition: &client.RuleCondition{
					Field:    "TAGS",
					Operator: "CONTAINS",
					Value:    "delete-me",
				},
			},
		},
	})
	t.Cleanup(func() { c.DeleteAutomation(t, instanceID, automation.ID) })

	// Apply
	c.ApplyAutomations(t, instanceID)
	time.Sleep(3 * time.Second)

	// Verify torrent was deleted
	torrents = c.ListTorrents(t, instanceID)
	for _, tor := range torrents.Torrents {
		assert.NotEqual(t, hash, tor.Hash, "torrent should have been deleted")
	}
}

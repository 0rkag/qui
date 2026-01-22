package tests

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/autobrr/qui/e2e/internal/assert/golden"
	"github.com/autobrr/qui/e2e/internal/client"
	"github.com/autobrr/qui/e2e/internal/containers"
)

// TestGoldenResponses snapshots API response structures to catch breaking changes.
// Run with -update-golden to regenerate golden files.
// Skipped when testing non-default qBittorrent versions as API structures differ.
func TestGoldenResponses(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	if img := os.Getenv("QUI_E2E_QBIT_IMAGE"); img != "" {
		t.Skip("skipping golden tests for non-default qBittorrent version")
	}
	t.Parallel()

	ctx := context.Background()
	env := containers.Setup(ctx, t, containers.DefaultConfig(), 1)
	t.Cleanup(func() { env.Teardown(ctx) })

	c := env.Client()
	qbit := env.Instances[0]

	// Create a test instance for API calls
	instanceID := c.CreateInstance(t, client.InstanceConfig{
		Name:     "golden-test",
		Host:     qbit.URL,
		Username: "admin",
		Password: qbit.Password,
	})
	t.Cleanup(func() { c.DeleteInstance(t, instanceID) })

	// Wait for instance to connect
	waitForInstance(t, c, instanceID)

	// Add a test torrent for torrent-related golden tests
	hash := c.AddTorrentFromMagnet(t, instanceID, testMagnet, client.AddTorrentOptions{
		Paused: true,
	})
	t.Cleanup(func() { c.DeleteTorrent(t, instanceID, hash, true) })

	// Create test category and tags
	c.CreateCategory(t, instanceID, "golden-category", "/downloads/golden")
	t.Cleanup(func() { c.DeleteCategory(t, instanceID, "golden-category") })

	c.CreateTags(t, instanceID, []string{"golden-tag-1", "golden-tag-2"})
	t.Cleanup(func() { c.DeleteTags(t, instanceID, []string{"golden-tag-1", "golden-tag-2"}) })

	t.Run("instances/list", func(t *testing.T) {
		data := c.GetRaw(t, "/api/instances")
		golden.AssertStructure(t, "instances_list", data)
	})

	t.Run("instances/capabilities", func(t *testing.T) {
		data := c.GetRaw(t, fmt.Sprintf("/api/instances/%d/capabilities", instanceID))
		golden.AssertStructure(t, "instance_capabilities", data)
	})

	t.Run("torrents/list", func(t *testing.T) {
		data := c.GetRaw(t, fmt.Sprintf("/api/instances/%d/torrents", instanceID))
		golden.AssertStructure(t, "torrents_list", data)
	})

	t.Run("categories/list", func(t *testing.T) {
		data := c.GetRaw(t, fmt.Sprintf("/api/instances/%d/categories", instanceID))
		golden.AssertStructure(t, "categories_list", data)
	})

	t.Run("tags/list", func(t *testing.T) {
		data := c.GetRaw(t, fmt.Sprintf("/api/instances/%d/tags", instanceID))
		golden.AssertStructure(t, "tags_list", data)
	})
}

// TestGoldenNormalization verifies that normalization works correctly.
// These tests use Assert (with value normalization) rather than AssertStructure.
// Skipped when testing non-default qBittorrent versions as API structures differ.
func TestGoldenNormalization(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	if img := os.Getenv("QUI_E2E_QBIT_IMAGE"); img != "" {
		t.Skip("skipping golden tests for non-default qBittorrent version")
	}
	t.Parallel()

	ctx := context.Background()
	env := containers.Setup(ctx, t, containers.DefaultConfig(), 1)
	t.Cleanup(func() { env.Teardown(ctx) })

	c := env.Client()
	qbit := env.Instances[0]

	instanceID := c.CreateInstance(t, client.InstanceConfig{
		Name:     "norm-test",
		Host:     qbit.URL,
		Username: "admin",
		Password: qbit.Password,
	})
	t.Cleanup(func() { c.DeleteInstance(t, instanceID) })

	waitForInstance(t, c, instanceID)

	t.Run("capabilities/normalized", func(t *testing.T) {
		data := c.GetRaw(t, fmt.Sprintf("/api/instances/%d/capabilities", instanceID))
		golden.Assert(t, "capabilities_normalized", data)
	})
}

// waitForInstance and testMagnet are defined in torrent_test.go

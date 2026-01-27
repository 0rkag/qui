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

func TestTorrentFileOps(t *testing.T) {
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
		Name:     "file-ops-test",
		Host:     qbit.URL,
		Username: "admin",
		Password: qbit.Password,
	})
	t.Cleanup(func() { c.DeleteInstance(t, instanceID) })

	waitForInstance(t, c, instanceID)

	// Use multi-file torrent for file operations
	torrentFile := testdataPath("torrents/wired-cd.torrent")
	hash := c.AddTorrentFromFile(t, instanceID, torrentFile, client.AddTorrentOptions{
		Paused: true,
	})
	t.Cleanup(func() { c.DeleteTorrent(t, instanceID, hash, true) })

	time.Sleep(2 * time.Second)

	var files []client.TorrentFile

	t.Run("get_files", func(t *testing.T) {
		files = c.GetTorrentFiles(t, instanceID, hash)
		require.NotEmpty(t, files, "multi-file torrent should have files")

		for _, f := range files {
			assert.NotEmpty(t, f.Name, "file name should be set")
			assert.Greater(t, f.Size, int64(0), "file size should be positive")
			assert.GreaterOrEqual(t, f.Priority, 0, "priority should be non-negative")
		}
		t.Logf("Found %d files in torrent", len(files))
	})

	t.Run("set_file_priority", func(t *testing.T) {
		require.NotEmpty(t, files, "need files from previous subtest")
		// Set first file to "don't download" (priority 0)
		c.SetTorrentFilePriority(t, instanceID, hash, []int{0}, 0)
	})

	t.Run("verify_priority_changed", func(t *testing.T) {
		time.Sleep(time.Second)

		updatedFiles := c.GetTorrentFiles(t, instanceID, hash)
		require.NotEmpty(t, updatedFiles)
		assert.Equal(t, 0, updatedFiles[0].Priority, "first file should have priority 0")
	})

	t.Run("rename_torrent", func(t *testing.T) {
		c.RenameTorrent(t, instanceID, hash, "renamed-torrent-display-name")

		time.Sleep(2 * time.Second)

		torrent := c.GetTorrent(t, instanceID, hash)
		assert.Equal(t, "renamed-torrent-display-name", torrent.Name)
	})

	t.Run("rename_file", func(t *testing.T) {
		require.NotEmpty(t, files, "need files from get_files subtest")

		oldPath := files[0].Name
		// Construct new path by changing the filename
		parts := strings.Split(oldPath, "/")
		parts[len(parts)-1] = "renamed-" + parts[len(parts)-1]
		newPath := strings.Join(parts, "/")

		c.RenameTorrentFile(t, instanceID, hash, oldPath, newPath)

		time.Sleep(2 * time.Second)

		updatedFiles := c.GetTorrentFiles(t, instanceID, hash)
		require.NotEmpty(t, updatedFiles)
		assert.Equal(t, newPath, updatedFiles[0].Name)
	})

	t.Run("rename_folder", func(t *testing.T) {
		// Get fresh file list after rename
		currentFiles := c.GetTorrentFiles(t, instanceID, hash)
		require.NotEmpty(t, currentFiles)

		// Find a file with a directory component
		var folderPath string
		for _, f := range currentFiles {
			if idx := strings.Index(f.Name, "/"); idx > 0 {
				folderPath = f.Name[:idx]
				break
			}
		}
		if folderPath == "" {
			t.Skip("no directory found in torrent files, skipping rename_folder")
		}

		newFolderPath := "renamed-" + folderPath
		c.RenameTorrentFolder(t, instanceID, hash, folderPath, newFolderPath)

		time.Sleep(2 * time.Second)

		updatedFiles := c.GetTorrentFiles(t, instanceID, hash)
		// Verify at least one file has the new folder prefix
		found := false
		for _, f := range updatedFiles {
			if strings.HasPrefix(f.Name, newFolderPath+"/") {
				found = true
				break
			}
		}
		assert.True(t, found, "should have files with renamed folder prefix")
	})

	t.Run("export_torrent", func(t *testing.T) {
		data := c.ExportTorrent(t, instanceID, hash)
		require.NotEmpty(t, data, "exported torrent should not be empty")
		// Bencoded dictionaries start with 'd'
		assert.Equal(t, byte('d'), data[0], "exported data should be valid bencoded dictionary")
	})
}

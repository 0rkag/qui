// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build integration

package transfer

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

// Integration tests for filesystem-dependent operations.
// Run with: go test -tags=integration ./internal/services/transfer/...

// setupTestDir creates a temp directory with test files.
func setupTestDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()

	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("failed to create directory for %s: %v", name, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("failed to create test file %s: %v", name, err)
		}
	}

	return dir
}

// testVrifyHardlink checks that two files are hardlinked (same inode).
func testVerifyHardlink(t *testing.T, path1, path2 string) {
	t.Helper()

	info1, err := os.Stat(path1)
	require.NoError(t, err, "failed to stat %s", path1)

	info2, err := os.Stat(path2)
	require.NoError(t, err, "failed to stat %s", path2)

	// On Unix, we can check if they're the same file via os.SameFile
	assert.True(t, os.SameFile(info1, info2), "files should be hardlinked: %s and %s", path1, path2)
}

func TestIntegration_DetermineLinkMode_SameFilesystem(t *testing.T) {
	// Create source and target on same filesystem (same temp dir parent)
	sourceDir := t.TempDir()
	targetDir := t.TempDir()

	executor := &LocalExecutor{}

	targetInstance := &models.Instance{
		UseHardlinks: true,
	}

	mode, err := executor.determineLinkMode(targetInstance, sourceDir, targetDir)
	require.NoError(t, err)
	assert.Equal(t, "hardlink", mode)
}

func TestIntegration_DetermineLinkMode_HardlinksDisabled(t *testing.T) {
	sourceDir := t.TempDir()
	targetDir := t.TempDir()

	executor := &LocalExecutor{}

	targetInstance := &models.Instance{
		UseHardlinks: false,
		UseReflinks:  false,
	}

	mode, err := executor.determineLinkMode(targetInstance, sourceDir, targetDir)
	require.NoError(t, err)
	assert.Equal(t, "direct", mode)
}

func TestIntegration_CreateLinks_Hardlinks(t *testing.T) {
	ctx := context.Background()

	// Create source files
	sourceDir := setupTestDir(t, map[string]string{
		"Test Torrent/file1.txt":        "content of file 1",
		"Test Torrent/file2.txt":        "content of file 2",
		"Test Torrent/subdir/file3.txt": "content of file 3",
	})
	targetDir := t.TempDir()

	transfer := &models.Transfer{
		ID:          1,
		TorrentHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}

	prep := &PrepareResult{
		TorrentName:    "Test Torrent",
		SourceSavePath: sourceDir,
		TargetSavePath: targetDir,
		LinkMode:       "hardlink",
		Files: []TorrentFile{
			{RelPath: "Test Torrent/file1.txt", AbsPath: filepath.Join(sourceDir, "Test Torrent/file1.txt"), Size: 17},
			{RelPath: "Test Torrent/file2.txt", AbsPath: filepath.Join(sourceDir, "Test Torrent/file2.txt"), Size: 17},
			{RelPath: "Test Torrent/subdir/file3.txt", AbsPath: filepath.Join(sourceDir, "Test Torrent/subdir/file3.txt"), Size: 17},
		},
		TargetInstance: &models.Instance{
			ID: 2,
		},
	}

	executor := NewLocalExecutor(nil, nil)

	count, err := executor.CreateLinks(ctx, transfer, prep)
	require.NoError(t, err)
	assert.Equal(t, 3, count)

	// Verify hardlinks were created
	testVerifyHardlink(t,
		filepath.Join(sourceDir, "Test Torrent/file1.txt"),
		filepath.Join(prep.TargetSavePath, "Test Torrent/file1.txt"))
	testVerifyHardlink(t,
		filepath.Join(sourceDir, "Test Torrent/file2.txt"),
		filepath.Join(prep.TargetSavePath, "Test Torrent/file2.txt"))
	testVerifyHardlink(t,
		filepath.Join(sourceDir, "Test Torrent/subdir/file3.txt"),
		filepath.Join(prep.TargetSavePath, "Test Torrent/subdir/file3.txt"))
}

func TestIntegration_CreateLinks_DirectMode_Skips(t *testing.T) {
	ctx := context.Background()

	transfer := &models.Transfer{ID: 1}
	prep := &PrepareResult{
		LinkMode: "direct",
	}

	executor := NewLocalExecutor(nil, nil)

	count, err := executor.CreateLinks(ctx, transfer, prep)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestIntegration_Rollback_CleansUpLinks(t *testing.T) {
	ctx := context.Background()

	// Create source files
	sourceDir := setupTestDir(t, map[string]string{
		"Test Torrent/file1.txt": "content of file 1",
		"Test Torrent/file2.txt": "content of file 2",
	})
	targetDir := t.TempDir()

	transfer := &models.Transfer{
		ID:          1,
		TorrentHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}

	prep := &PrepareResult{
		TorrentName:    "Test Torrent",
		SourceSavePath: sourceDir,
		TargetSavePath: targetDir,
		LinkMode:       "hardlink",
		Files: []TorrentFile{
			{RelPath: "Test Torrent/file1.txt", AbsPath: filepath.Join(sourceDir, "Test Torrent/file1.txt"), Size: 17},
			{RelPath: "Test Torrent/file2.txt", AbsPath: filepath.Join(sourceDir, "Test Torrent/file2.txt"), Size: 17},
		},
		TargetInstance: &models.Instance{ID: 2},
	}

	executor := NewLocalExecutor(nil, nil)

	// First create the links
	count, err := executor.CreateLinks(ctx, transfer, prep)
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	// Verify links exist
	targetFile1 := filepath.Join(prep.TargetSavePath, "Test Torrent/file1.txt")
	targetFile2 := filepath.Join(prep.TargetSavePath, "Test Torrent/file2.txt")
	require.FileExists(t, targetFile1)
	require.FileExists(t, targetFile2)

	// Now rollback
	err = executor.Rollback(ctx, transfer, prep)
	require.NoError(t, err)

	// Verify links are gone
	assert.NoFileExists(t, targetFile1)
	assert.NoFileExists(t, targetFile2)

	// Source files should still exist
	require.FileExists(t, filepath.Join(sourceDir, "Test Torrent/file1.txt"))
	require.FileExists(t, filepath.Join(sourceDir, "Test Torrent/file2.txt"))
}

func TestIntegration_Rollback_DirectMode_NoOp(t *testing.T) {
	ctx := context.Background()

	transfer := &models.Transfer{ID: 1}
	prep := &PrepareResult{
		LinkMode: "direct",
	}

	executor := NewLocalExecutor(nil, nil)

	// Should not error
	err := executor.Rollback(ctx, transfer, prep)
	require.NoError(t, err)
}

func TestIntegration_Rollback_NilPrep_NoOp(t *testing.T) {
	ctx := context.Background()

	transfer := &models.Transfer{ID: 1}

	executor := NewLocalExecutor(nil, nil)

	err := executor.Rollback(ctx, transfer, nil)
	require.NoError(t, err)
}

func TestIntegration_FullPrepareFlow(t *testing.T) {
	ctx := context.Background()

	// Create source directory with test files
	sourceDir := setupTestDir(t, map[string]string{
		"Test Torrent/file1.mkv": "video content here",
		"Test Torrent/file2.mkv": "more video content",
	})
	targetDir := t.TempDir()

	transfer := &models.Transfer{
		ID:               1,
		SourceInstanceID: 1,
		TargetInstanceID: 2,
		TorrentHash:      "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		PreserveCategory: true,
		PreserveTags:     true,
	}

	// Setup mocks
	sm := newMockSyncManager()
	sm.getTorrentsFunc = func(ctx context.Context, instanceID int, filter qbt.TorrentFilterOptions) ([]qbt.Torrent, error) {
		return []qbt.Torrent{{
			Hash:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			Name:     "Test Torrent",
			Progress: 1.0,
			Category: "movies",
			Tags:     "hd, favorite",
		}}, nil
	}
	sm.getTorrentFilesFunc = func(ctx context.Context, instanceID int, hash string) (*qbt.TorrentFiles, error) {
		files := qbt.TorrentFiles{
			{Name: "Test Torrent/file1.mkv", Size: 18},
			{Name: "Test Torrent/file2.mkv", Size: 18},
		}
		return &files, nil
	}
	sm.getTorrentPropsFunc = func(ctx context.Context, instanceID int, hash string) (*qbt.TorrentProperties, error) {
		return &qbt.TorrentProperties{
			SavePath: sourceDir,
		}, nil
	}
	sm.exportTorrentFunc = func(ctx context.Context, instanceID int, hash string) ([]byte, string, string, error) {
		return []byte("mock torrent data"), "test.torrent", "application/x-bittorrent", nil
	}

	ip := newMockInstanceProvider()
	ip.AddInstance(&models.Instance{
		ID:                       1,
		Name:                     "Source",
		HasLocalFilesystemAccess: true,
	})
	ip.AddInstance(&models.Instance{
		ID:                       2,
		Name:                     "Target",
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
		HardlinkBaseDir:          targetDir,
	})

	executor := NewLocalExecutor(sm, ip)

	// Execute prepare
	prep, err := executor.Prepare(ctx, transfer)
	require.NoError(t, err)

	// Verify result
	assert.Equal(t, "Test Torrent", prep.TorrentName)
	assert.Equal(t, sourceDir, prep.SourceSavePath)
	assert.Equal(t, targetDir, prep.TargetSavePath)
	assert.Equal(t, "hardlink", prep.LinkMode)
	assert.Equal(t, "movies", prep.Category)
	assert.Equal(t, []string{"hd", "favorite"}, prep.Tags)
	assert.Len(t, prep.Files, 2)
	assert.Equal(t, []byte("mock torrent data"), prep.TorrentData)
}

func TestIntegration_FullTransferFlow(t *testing.T) {
	ctx := context.Background()

	// Create source directory with test files
	sourceDir := setupTestDir(t, map[string]string{
		"Test Torrent/file1.mkv": "video content here",
		"Test Torrent/file2.mkv": "more video content",
	})
	targetDir := t.TempDir()

	transfer := &models.Transfer{
		ID:               1,
		SourceInstanceID: 1,
		TargetInstanceID: 2,
		TorrentHash:      "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}

	// Setup mocks
	sm := newMockSyncManager()
	sm.getTorrentsFunc = func(ctx context.Context, instanceID int, filter qbt.TorrentFilterOptions) ([]qbt.Torrent, error) {
		return []qbt.Torrent{{
			Hash:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			Name:     "Test Torrent",
			Progress: 1.0,
		}}, nil
	}
	sm.getTorrentFilesFunc = func(ctx context.Context, instanceID int, hash string) (*qbt.TorrentFiles, error) {
		files := qbt.TorrentFiles{
			{Name: "Test Torrent/file1.mkv", Size: 18},
			{Name: "Test Torrent/file2.mkv", Size: 18},
		}
		return &files, nil
	}
	sm.getTorrentPropsFunc = func(ctx context.Context, instanceID int, hash string) (*qbt.TorrentProperties, error) {
		return &qbt.TorrentProperties{
			SavePath: sourceDir,
		}, nil
	}
	sm.exportTorrentFunc = func(ctx context.Context, instanceID int, hash string) ([]byte, string, string, error) {
		return []byte("mock torrent data"), "test.torrent", "application/x-bittorrent", nil
	}

	ip := newMockInstanceProvider()
	ip.AddInstance(&models.Instance{
		ID:                       1,
		Name:                     "Source",
		HasLocalFilesystemAccess: true,
	})
	ip.AddInstance(&models.Instance{
		ID:                       2,
		Name:                     "Target",
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
		HardlinkBaseDir:          targetDir,
	})

	executor := NewLocalExecutor(sm, ip)

	// Step 1: Prepare
	prep, err := executor.Prepare(ctx, transfer)
	require.NoError(t, err)
	assert.Equal(t, "hardlink", prep.LinkMode)

	// Step 2: Create links
	count, err := executor.CreateLinks(ctx, transfer, prep)
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	// Verify links exist and are hardlinked
	targetFile1 := filepath.Join(prep.TargetSavePath, "Test Torrent/file1.mkv")
	targetFile2 := filepath.Join(prep.TargetSavePath, "Test Torrent/file2.mkv")
	require.FileExists(t, targetFile1)
	require.FileExists(t, targetFile2)

	testVerifyHardlink(t, filepath.Join(sourceDir, "Test Torrent/file1.mkv"), targetFile1)
	testVerifyHardlink(t, filepath.Join(sourceDir, "Test Torrent/file2.mkv"), targetFile2)

	// Step 3: Add torrent would be mocked (qBit API)
	err = executor.AddTorrent(ctx, transfer, prep)
	require.NoError(t, err)
	assert.Len(t, sm.addTorrentCalls, 1)
	assert.Equal(t, 2, sm.addTorrentCalls[0].InstanceID)

	// Verify options passed
	opts := sm.addTorrentCalls[0].Options
	assert.Equal(t, prep.TargetSavePath, opts["savepath"])
	assert.Equal(t, "true", opts["skip_checking"])
	assert.Equal(t, "Original", opts["contentLayout"])
}

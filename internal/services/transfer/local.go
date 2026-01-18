// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/checksum"
	"github.com/autobrr/qui/pkg/fsutil"
	"github.com/autobrr/qui/pkg/hardlinktree"
	"github.com/autobrr/qui/pkg/reflinktree"
)

// LocalExecutor handles transfers where QUI has local filesystem access
// to both source and target paths. This is the default executor for
// single-machine deployments.
type LocalExecutor struct {
	syncManager   SyncManager
	instanceStore InstanceProvider
	pathResolver  *models.PathResolver
}

// NewLocalExecutor creates a new LocalExecutor.
func NewLocalExecutor(syncManager SyncManager, instanceStore InstanceProvider) *LocalExecutor {
	return &LocalExecutor{
		syncManager:   syncManager,
		instanceStore: instanceStore,
	}
}

// NewLocalExecutorWithPathResolver creates a LocalExecutor with path mapping support.
func NewLocalExecutorWithPathResolver(syncManager SyncManager, instanceStore InstanceProvider, pathMappingStore *models.InstancePathMappingStore) *LocalExecutor {
	var resolver *models.PathResolver
	if pathMappingStore != nil {
		resolver = models.NewPathResolver(pathMappingStore)
	}
	return &LocalExecutor{
		syncManager:   syncManager,
		instanceStore: instanceStore,
		pathResolver:  resolver,
	}
}

// CanHandle returns true if both instances have local filesystem access.
func (e *LocalExecutor) CanHandle(source, target *models.Instance) bool {
	return source.HasLocalFilesystemAccess && target.HasLocalFilesystemAccess
}

// Prepare validates the transfer and gathers source information.
func (e *LocalExecutor) Prepare(ctx context.Context, t *models.Transfer) (*PrepareResult, error) {
	const logPrefix = "[TRANSFER-LOCAL]"

	// 1. Run common preparation (get instances, torrent, files, validate)
	common, err := prepareCommon(ctx, t, e.syncManager, e.instanceStore, PrepareConfig{
		LogPrefix:          logPrefix,
		RequireLocalSource: true,
		RequireLocalTarget: true,
	})
	if err != nil {
		return nil, err
	}

	// 2. Compute target save path
	targetSavePath := resolveTargetPath(
		ctx,
		common.Properties.SavePath,
		t.SourceInstanceID,
		t.TargetInstanceID,
		common.TargetInstance,
		e.pathResolver,
		t.PathMappings,
		logPrefix,
	)

	// 3. Determine link mode
	linkMode, err := e.determineLinkMode(common.TargetInstance, common.Properties.SavePath, targetSavePath)
	if err != nil {
		return nil, fmt.Errorf("failed to determine link mode: %w", err)
	}

	// 4. Validate direct mode - only allowed if paths are identical
	if linkMode == "direct" && common.Properties.SavePath != targetSavePath {
		return nil, fmt.Errorf("direct mode requires identical source and target paths (no hardlinks/reflinks available and paths differ)")
	}

	// 5. Export torrent for later use
	torrentBytes, _, _, err := e.syncManager.ExportTorrent(ctx, t.SourceInstanceID, t.TorrentHash)
	if err != nil {
		return nil, fmt.Errorf("failed to export torrent: %w", err)
	}

	// 6. Build final result
	result := buildPrepareResult(common, targetSavePath, linkMode, torrentBytes)

	log.Info().
		Int64("id", t.ID).
		Str("name", result.TorrentName).
		Str("sourcePath", result.SourceSavePath).
		Str("targetPath", result.TargetSavePath).
		Str("linkMode", result.LinkMode).
		Int("files", len(result.Files)).
		Msg("[TRANSFER-LOCAL] Prepared transfer")

	return result, nil
}

// CreateLinks creates hardlinks or reflinks for the torrent files.
func (e *LocalExecutor) CreateLinks(ctx context.Context, t *models.Transfer, prep *PrepareResult) (int, error) {
	// Direct mode - skip link creation
	if prep.LinkMode == "direct" {
		log.Debug().Int64("id", t.ID).Msg("[TRANSFER-LOCAL] Direct mode - skipping link creation")
		return 0, nil
	}

	// Build file list for linking
	sourceFiles := make([]hardlinktree.TorrentFile, 0, len(prep.Files))
	for _, f := range prep.Files {
		sourceFiles = append(sourceFiles, hardlinktree.TorrentFile{
			Path: f.RelPath,
			Size: f.Size,
		})
	}

	// Build existing files list (source locations)
	existingFiles := make([]hardlinktree.ExistingFile, 0, len(prep.Files))
	for _, f := range prep.Files {
		existingFiles = append(existingFiles, hardlinktree.ExistingFile{
			AbsPath: f.AbsPath,
			RelPath: f.RelPath,
			Size:    f.Size,
		})
	}

	// Build destination directory
	destDir := e.buildDestDir(t, prep)

	// Ensure destination directory exists
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return 0, fmt.Errorf("failed to create destination directory: %w", err)
	}

	// Build plan
	plan, err := hardlinktree.BuildPlan(sourceFiles, existingFiles, hardlinktree.LayoutOriginal, prep.TorrentName, destDir)
	if err != nil {
		return 0, fmt.Errorf("failed to build link plan: %w", err)
	}

	// Update target save path to actual plan root (may differ from computed path)
	prep.TargetSavePath = plan.RootDir

	// Execute based on link mode
	switch prep.LinkMode {
	case "hardlink":
		if err := hardlinktree.Create(plan); err != nil {
			_ = hardlinktree.Rollback(plan)
			return 0, fmt.Errorf("failed to create hardlinks: %w", err)
		}
	case "reflink":
		if err := reflinktree.Create(plan); err != nil {
			_ = reflinktree.Rollback(plan)
			if prep.TargetInstance.FallbackToRegularMode {
				log.Warn().
					Err(err).
					Int64("id", t.ID).
					Msg("[TRANSFER-LOCAL] Reflink failed, copy fallback not implemented")
				return 0, fmt.Errorf("reflink failed and copy fallback not implemented: %w", err)
			}
			return 0, fmt.Errorf("failed to create reflinks: %w", err)
		}
	default:
		return 0, fmt.Errorf("unsupported or empty link mode: %s", prep.LinkMode)
	}

	log.Info().
		Int64("id", t.ID).
		Int("files", len(plan.Files)).
		Str("destDir", destDir).
		Msg("[TRANSFER-LOCAL] Created file links")

	return len(plan.Files), nil
}

// AddTorrent adds the torrent to the target instance.
func (e *LocalExecutor) AddTorrent(ctx context.Context, t *models.Transfer, prep *PrepareResult) error {
	// Build add options
	options := map[string]string{
		"autoTMM":       "false",
		"savepath":      prep.TargetSavePath,
		"skip_checking": "true", // Files already exist via links
	}

	if prep.LinkMode != "direct" {
		options["contentLayout"] = "Original"
	}

	if prep.Category != "" {
		options["category"] = prep.Category
	}

	if len(prep.Tags) > 0 {
		options["tags"] = strings.Join(prep.Tags, ",")
	}

	// Start paused to verify before resuming
	options["paused"] = "true"
	options["stopped"] = "true"

	// Add torrent to target
	if err := e.syncManager.AddTorrent(ctx, t.TargetInstanceID, prep.TorrentData, options); err != nil {
		return fmt.Errorf("failed to add torrent to target: %w", err)
	}

	log.Info().
		Int64("id", t.ID).
		Str("hash", t.TorrentHash).
		Int("targetInstance", t.TargetInstanceID).
		Msg("[TRANSFER-LOCAL] Added torrent to target instance")

	return nil
}

// DeleteSource removes the torrent from the source instance.
// Files are deleted in all modes EXCEPT "direct" (shared storage).
// - hardlink: safe to delete - target hardlinks preserve the data via shared inodes
// - reflink: safe to delete - target has independent CoW copies
// - direct: must NOT delete - source and target are the same files
func (e *LocalExecutor) DeleteSource(ctx context.Context, t *models.Transfer) error {
	// Delete source files unless using direct mode (shared storage)
	deleteFiles := t.LinkMode != "direct"
	if err := e.syncManager.DeleteTorrents(ctx, t.SourceInstanceID, []string{t.TorrentHash}, deleteFiles); err != nil {
		return fmt.Errorf("failed to delete from source: %w", err)
	}

	log.Info().
		Int64("id", t.ID).
		Int("sourceInstance", t.SourceInstanceID).
		Msg("[TRANSFER-LOCAL] Deleted torrent from source instance")

	return nil
}

// Rollback cleans up any created files on failure.
func (e *LocalExecutor) Rollback(ctx context.Context, t *models.Transfer, prep *PrepareResult) error {
	if prep == nil || prep.LinkMode == "direct" || prep.TargetSavePath == "" {
		return nil
	}

	log.Debug().Int64("id", t.ID).Str("path", prep.TargetSavePath).Str("mode", prep.LinkMode).Msg("[TRANSFER-LOCAL] Rolling back links")

	// Build plan just for rollback
	plan := &hardlinktree.TreePlan{
		RootDir: prep.TargetSavePath,
		Files:   make([]hardlinktree.FilePlan, 0, len(prep.Files)),
	}
	for _, f := range prep.Files {
		plan.Files = append(plan.Files, hardlinktree.FilePlan{
			TargetPath: filepath.Join(prep.TargetSavePath, f.RelPath),
		})
	}

	// Rollback works the same way for both hardlinks and reflinks
	if err := hardlinktree.Rollback(plan); err != nil {
		log.Warn().Err(err).Int64("id", t.ID).Str("mode", prep.LinkMode).Msg("[TRANSFER-LOCAL] Rollback failed")
		return err
	}

	return nil
}

// VerifyTransfer verifies that the transferred files match the source.
func (e *LocalExecutor) VerifyTransfer(ctx context.Context, t *models.Transfer, prep *PrepareResult) error {
	if prep == nil {
		return fmt.Errorf("no preparation result to verify")
	}

	// Direct mode - source and target are the same files, nothing to verify
	if prep.LinkMode == "direct" {
		log.Debug().Int64("id", t.ID).Msg("[TRANSFER-LOCAL] Direct mode - skipping verification")
		return nil
	}

	log.Info().
		Int64("id", t.ID).
		Str("mode", prep.LinkMode).
		Int("files", len(prep.Files)).
		Msg("[TRANSFER-LOCAL] Starting transfer verification")

	for _, f := range prep.Files {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		srcPath := f.AbsPath
		dstPath := filepath.Join(prep.TargetSavePath, f.RelPath)

		switch prep.LinkMode {
		case "hardlink":
			// For hardlinks, verify they share the same inode
			if err := verifyHardlink(srcPath, dstPath); err != nil {
				return fmt.Errorf("hardlink verification failed for %s: %w", f.RelPath, err)
			}

		case "reflink":
			// For reflinks, verify checksums match (they're CoW copies)
			if err := verifyChecksum(srcPath, dstPath); err != nil {
				return fmt.Errorf("reflink verification failed for %s: %w", f.RelPath, err)
			}

		default:
			// Unknown mode - use checksum verification
			if err := verifyChecksum(srcPath, dstPath); err != nil {
				return fmt.Errorf("verification failed for %s: %w", f.RelPath, err)
			}
		}
	}

	log.Info().
		Int64("id", t.ID).
		Int("files", len(prep.Files)).
		Msg("[TRANSFER-LOCAL] Transfer verification complete")

	return nil
}

// verifyHardlink checks that two paths share the same inode.
func verifyHardlink(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("stat source: %w", err)
	}

	dstInfo, err := os.Stat(dst)
	if err != nil {
		return fmt.Errorf("stat destination: %w", err)
	}

	srcStat, ok := srcInfo.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("cannot get source inode (unsupported platform)")
	}

	dstStat, ok := dstInfo.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("cannot get destination inode (unsupported platform)")
	}

	if srcStat.Ino != dstStat.Ino {
		return fmt.Errorf("inode mismatch: source=%d, destination=%d", srcStat.Ino, dstStat.Ino)
	}

	return nil
}

// verifyChecksum computes and compares SHA256 checksums of two files.
func verifyChecksum(src, dst string) error {
	// Quick size check first
	match, err := checksum.FileSizeMatch(src, dst)
	if err != nil {
		return fmt.Errorf("size check failed: %w", err)
	}
	if !match {
		return fmt.Errorf("file size mismatch")
	}

	// Full checksum comparison
	equal, err := checksum.CompareFiles(src, dst, checksum.AlgorithmSHA256)
	if err != nil {
		return fmt.Errorf("checksum comparison failed: %w", err)
	}
	if !equal {
		return fmt.Errorf("checksum mismatch")
	}

	return nil
}

// determineLinkMode decides whether to use hardlinks, reflinks, or direct mode.
func (e *LocalExecutor) determineLinkMode(
	targetInstance *models.Instance,
	sourcePath, targetPath string,
) (string, error) {
	// Check hardlinks first (preferred - instant, no extra space)
	if targetInstance.UseHardlinks {
		sameFS, err := fsutil.SameFilesystem(sourcePath, targetPath)
		if err == nil && sameFS {
			return "hardlink", nil
		} else if !targetInstance.FallbackToRegularMode {
			return "", ErrNoLinkModeAvailable
		}
	} else if targetInstance.UseReflinks {
		// Reflinks require same filesystem
		if sameFS, err := fsutil.SameFilesystem(sourcePath, targetPath); err == nil && sameFS {
			if supported, _ := reflinktree.SupportsReflink(targetPath); supported {
				return "reflink", nil
			}
		}
		if !targetInstance.FallbackToRegularMode {
			return "", ErrNoLinkModeAvailable
		}
	}

	// Plain old direct usage / copy
	return "direct", nil
}

// buildDestDir determines the destination directory based on instance settings.
func (e *LocalExecutor) buildDestDir(t *models.Transfer, prep *PrepareResult) string {
	baseDir := prep.TargetSavePath
	if prep.TargetInstance.HardlinkBaseDir != "" {
		baseDir = prep.TargetInstance.HardlinkBaseDir
	}

	// Use preset if configured
	switch prep.TargetInstance.HardlinkDirPreset {
	case "flat":
		// All torrents in base dir with isolation folder
		shortHash := t.TorrentHash
		if len(shortHash) > 8 {
			shortHash = shortHash[:8]
		}
		return filepath.Join(baseDir, fmt.Sprintf("%s--%s", sanitizeName(prep.TorrentName), shortHash))
	case "by-instance":
		return filepath.Join(baseDir, fmt.Sprintf("instance-%d", t.SourceInstanceID))
	default:
		return baseDir
	}
}

// sanitizeName replaces characters that might cause issues in paths.
func sanitizeName(name string) string {
	replacer := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		":", "_",
		"*", "_",
		"?", "_",
		"\"", "_",
		"<", "_",
		">", "_",
		"|", "_",
	)
	return replacer.Replace(name)
}

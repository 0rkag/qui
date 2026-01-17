// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
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
	// 1. Validate source instance
	sourceInstance, err := e.instanceStore.Get(ctx, t.SourceInstanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get source instance: %w", err)
	}
	if !sourceInstance.HasLocalFilesystemAccess {
		return nil, ErrSourceNotAccessible
	}

	// 2. Validate target instance
	targetInstance, err := e.instanceStore.Get(ctx, t.TargetInstanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get target instance: %w", err)
	}
	if !targetInstance.HasLocalFilesystemAccess {
		return nil, ErrTargetNotAccessible
	}

	// 3. Get source torrent info
	torrents, err := e.syncManager.GetTorrents(ctx, t.SourceInstanceID,
		qbt.TorrentFilterOptions{Hashes: []string{t.TorrentHash}})
	if err != nil {
		return nil, fmt.Errorf("failed to get source torrent: %w", err)
	}
	if len(torrents) == 0 {
		return nil, ErrTorrentNotFound
	}
	sourceTorrent := torrents[0]

	// 4. Get source files
	files, err := e.syncManager.GetTorrentFiles(ctx, t.SourceInstanceID, t.TorrentHash)
	if err != nil {
		return nil, fmt.Errorf("failed to get source files: %w", err)
	}

	// 5. Get properties (save path)
	props, err := e.syncManager.GetTorrentProperties(ctx, t.SourceInstanceID, t.TorrentHash)
	if err != nil {
		return nil, fmt.Errorf("failed to get source properties: %w", err)
	}

	// 6. Build result early so that TorrentName is available even if we fail later
	result := &PrepareResult{
		TorrentName:    sourceTorrent.Name,
		SourceSavePath: props.SavePath,
		SourceInstance: sourceInstance,
		TargetInstance: targetInstance,
	}

	// 7. Build file list with path validation - only include complete files
	result.Files = make([]TorrentFile, 0, len(*files))
	var skippedFiles int
	for _, f := range *files {
		// Skip files that are not fully downloaded
		if f.Progress < 1.0 {
			skippedFiles++
			log.Debug().
				Str("file", f.Name).
				Float64("progress", float64(f.Progress)).
				Msg("[TRANSFER-LOCAL] Skipping incomplete file")
			continue
		}

		// Validate relative path to prevent path traversal attacks
		if err := ValidateRelPath(f.Name); err != nil {
			return nil, fmt.Errorf("unsafe file path in torrent %q: %w", f.Name, err)
		}
		result.Files = append(result.Files, TorrentFile{
			RelPath: f.Name,
			AbsPath: filepath.Join(props.SavePath, f.Name),
			Size:    f.Size,
		})
	}

	// 8. Fail if no files are complete
	if len(result.Files) == 0 {
		return result, fmt.Errorf("no complete files to transfer (torrent is %.1f%% complete)", sourceTorrent.Progress*100)
	}

	// Log if we're doing a partial transfer
	if skippedFiles > 0 {
		log.Info().
			Int("completeFiles", len(result.Files)).
			Int("skippedFiles", skippedFiles).
			Float64("torrentProgress", sourceTorrent.Progress*100).
			Msg("[TRANSFER-LOCAL] Partial transfer - only transferring complete files")
	}

	// 9. Extract category and tags
	if t.PreserveCategory {
		result.Category = sourceTorrent.Category
	}
	if t.PreserveTags && sourceTorrent.Tags != "" {
		tags := strings.Split(sourceTorrent.Tags, ",")
		for i := range tags {
			tags[i] = strings.TrimSpace(tags[i])
		}
		result.Tags = tags
	}

	// 10. Compute target save path
	// Use PathResolver for instance-level mappings if available
	if e.pathResolver != nil {
		resolvedPath, err := e.pathResolver.ResolveTargetPath(
			ctx,
			props.SavePath,
			t.SourceInstanceID,
			t.TargetInstanceID,
			t.PathMappings,
		)
		if err != nil {
			log.Warn().Err(err).Msg("[TRANSFER-LOCAL] Path resolution failed, falling back to legacy method")
			result.TargetSavePath = e.computeTargetPath(props.SavePath, targetInstance, t.PathMappings)
		} else {
			result.TargetSavePath = resolvedPath
			// If no mapping matched and HardlinkBaseDir is set, use it
			if resolvedPath == props.SavePath && targetInstance.HardlinkBaseDir != "" {
				result.TargetSavePath = targetInstance.HardlinkBaseDir
			}
		}
	} else {
		result.TargetSavePath = e.computeTargetPath(props.SavePath, targetInstance, t.PathMappings)
	}

	// 11. Determine link mode
	linkMode, err := e.determineLinkMode(targetInstance, props.SavePath, result.TargetSavePath)
	if err != nil {
		return nil, fmt.Errorf("failed to determine link mode: %w", err)
	}
	result.LinkMode = linkMode

	// 12. Validate direct mode - only allowed if paths are identical
	if linkMode == "direct" && props.SavePath != result.TargetSavePath {
		return nil, fmt.Errorf("direct mode requires identical source and target paths (no hardlinks/reflinks available and paths differ)")
	}

	// 13. Export torrent for later use
	torrentBytes, _, _, err := e.syncManager.ExportTorrent(ctx, t.SourceInstanceID, t.TorrentHash)
	if err != nil {
		return nil, fmt.Errorf("failed to export torrent: %w", err)
	}
	result.TorrentData = torrentBytes

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
func (e *LocalExecutor) DeleteSource(ctx context.Context, t *models.Transfer) error {
	// Delete torrent from source (keep files - they're hardlinked)
	if err := e.syncManager.DeleteTorrents(ctx, t.SourceInstanceID, []string{t.TorrentHash}, false); err != nil {
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

// computeTargetPath determines where files should be placed on target.
// This is a fallback method used when PathResolver is not available.
func (e *LocalExecutor) computeTargetPath(
	sourcePath string,
	targetInstance *models.Instance,
	mappings map[string]string,
) string {
	// 1. Apply per-transfer path mappings (longest prefix match)
	if len(mappings) > 0 {
		resolved := models.ApplyDirectMappings(sourcePath, mappings)
		if resolved != sourcePath {
			return resolved
		}
	}

	// 2. Use target instance's HardlinkBaseDir if configured
	if targetInstance.HardlinkBaseDir != "" {
		return targetInstance.HardlinkBaseDir
	}

	// 3. Same path (same server, shared storage)
	return sourcePath
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

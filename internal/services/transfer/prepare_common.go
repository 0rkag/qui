// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
)

// PrepareConfig holds configuration for the common prepare logic.
type PrepareConfig struct {
	// LogPrefix is used for log messages (e.g., "[TRANSFER-LOCAL]")
	LogPrefix string

	// RequireLocalSource requires the source instance to have local filesystem access
	RequireLocalSource bool

	// RequireLocalTarget requires the target instance to have local filesystem access
	RequireLocalTarget bool
}

// PrepareCommonResult contains the results of the common preparation steps.
type PrepareCommonResult struct {
	// SourceInstance is the source qBittorrent instance
	SourceInstance *models.Instance

	// TargetInstance is the target qBittorrent instance
	TargetInstance *models.Instance

	// SourceTorrent is the torrent metadata from qBittorrent
	SourceTorrent *qbt.Torrent

	// Properties contains torrent properties (save path, etc.)
	Properties *qbt.TorrentProperties

	// Files is the list of complete files ready for transfer
	Files []TorrentFile

	// SkippedFiles is the number of incomplete files that were skipped
	SkippedFiles int

	// Category is the torrent's category (if PreserveCategory is true)
	Category string

	// Tags is the list of tags (if PreserveTags is true)
	Tags []string
}

// prepareCommon performs the common preparation steps shared by all executors.
// It fetches instances, torrent info, files, and builds the file list.
func prepareCommon(
	ctx context.Context,
	t *models.Transfer,
	syncManager SyncManager,
	instanceStore InstanceProvider,
	cfg PrepareConfig,
) (*PrepareCommonResult, error) {
	// 1. Get source instance
	sourceInstance, err := instanceStore.Get(ctx, t.SourceInstanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get source instance: %w", err)
	}
	if cfg.RequireLocalSource && !sourceInstance.HasLocalFilesystemAccess {
		return nil, ErrSourceNotAccessible
	}

	// 2. Get target instance
	targetInstance, err := instanceStore.Get(ctx, t.TargetInstanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get target instance: %w", err)
	}
	if cfg.RequireLocalTarget && !targetInstance.HasLocalFilesystemAccess {
		return nil, ErrTargetNotAccessible
	}

	// 3. Get source torrent info
	torrents, err := syncManager.GetTorrents(ctx, t.SourceInstanceID,
		qbt.TorrentFilterOptions{Hashes: []string{t.TorrentHash}})
	if err != nil {
		return nil, fmt.Errorf("failed to get source torrent: %w", err)
	}
	if len(torrents) == 0 {
		return nil, ErrTorrentNotFound
	}
	sourceTorrent := torrents[0]

	// 4. Get source files
	files, err := syncManager.GetTorrentFiles(ctx, t.SourceInstanceID, t.TorrentHash)
	if err != nil {
		return nil, fmt.Errorf("failed to get source files: %w", err)
	}

	// 5. Get properties (save path)
	props, err := syncManager.GetTorrentProperties(ctx, t.SourceInstanceID, t.TorrentHash)
	if err != nil {
		return nil, fmt.Errorf("failed to get source properties: %w", err)
	}

	// 6. Build file list - only include complete files
	result := &PrepareCommonResult{
		SourceInstance: sourceInstance,
		TargetInstance: targetInstance,
		SourceTorrent:  &sourceTorrent,
		Properties:     props,
		Files:          make([]TorrentFile, 0, len(*files)),
	}

	for _, f := range *files {
		// Skip files that are not fully downloaded
		if f.Progress < 1.0 {
			result.SkippedFiles++
			log.Debug().
				Str("file", f.Name).
				Float64("progress", float64(f.Progress)).
				Msgf("%s Skipping incomplete file", cfg.LogPrefix)
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

	// 7. Fail if no files are complete
	if len(result.Files) == 0 {
		return nil, fmt.Errorf("no complete files to transfer (torrent is %.1f%% complete)", sourceTorrent.Progress*100)
	}

	// Log if we're doing a partial transfer
	if result.SkippedFiles > 0 {
		log.Info().
			Int("completeFiles", len(result.Files)).
			Int("skippedFiles", result.SkippedFiles).
			Float64("torrentProgress", sourceTorrent.Progress*100).
			Msgf("%s Partial transfer - only transferring complete files", cfg.LogPrefix)
	}

	// 8. Extract category and tags
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

	return result, nil
}

// buildPrepareResult creates a PrepareResult from the common result and additional parameters.
func buildPrepareResult(common *PrepareCommonResult, targetSavePath, linkMode string, torrentData []byte) *PrepareResult {
	return &PrepareResult{
		TorrentName:    common.SourceTorrent.Name,
		SourceSavePath: common.Properties.SavePath,
		TargetSavePath: targetSavePath,
		LinkMode:       linkMode,
		Files:          common.Files,
		TorrentData:    torrentData,
		Category:       common.Category,
		Tags:           common.Tags,
		SourceInstance: common.SourceInstance,
		TargetInstance: common.TargetInstance,
	}
}

// resolveTargetPath resolves the target save path using the path resolver or direct mappings.
// Returns the source path unchanged if no mappings match.
func resolveTargetPath(
	ctx context.Context,
	sourcePath string,
	sourceInstanceID, targetInstanceID int,
	targetInstance *models.Instance,
	pathResolver *models.PathResolver,
	transferMappings map[string]string,
	logPrefix string,
) string {
	if pathResolver != nil {
		resolvedPath, err := pathResolver.ResolveTargetPath(
			ctx,
			sourcePath,
			sourceInstanceID,
			targetInstanceID,
			transferMappings,
		)
		if err != nil {
			log.Warn().Err(err).Msgf("%s Path resolution failed, using source path", logPrefix)
			return applyFallbackPath(sourcePath, targetInstance)
		}
		// If no mapping matched and HardlinkBaseDir is set, use it
		if resolvedPath == sourcePath && targetInstance.HardlinkBaseDir != "" {
			return targetInstance.HardlinkBaseDir
		}
		return resolvedPath
	}

	return computeTargetPathCommon(sourcePath, targetInstance, transferMappings)
}

// applyFallbackPath returns HardlinkBaseDir if set, otherwise the source path.
func applyFallbackPath(sourcePath string, targetInstance *models.Instance) string {
	if targetInstance.HardlinkBaseDir != "" {
		return targetInstance.HardlinkBaseDir
	}
	return sourcePath
}

// computeTargetPathCommon determines where files should be placed on target.
// This is shared between executors for consistent path mapping behavior.
func computeTargetPathCommon(sourcePath string, targetInstance *models.Instance, mappings map[string]string) string {
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

// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"github.com/autobrr/qui/internal/models"
)

// ErrNoExecutorAvailable is returned when no executor can handle the transfer.
var ErrNoExecutorAvailable = errors.New("no executor available for this transfer configuration")

// TransferExecutor handles the actual file operations for a transfer.
// Different implementations support different deployment scenarios:
// - LocalExecutor: QUI has direct filesystem access to both instances
// - SSHExecutor: QUI connects via SSH for remote file operations (future)
// - AgentExecutor: QUI coordinates with agents deployed on remote machines (future)
type TransferExecutor interface {
	// Prepare validates the transfer and gathers source information.
	// This includes fetching torrent metadata, computing paths, and determining link mode.
	// Returns the prepared transfer info or an error.
	Prepare(ctx context.Context, t *models.Transfer) (*PrepareResult, error)

	// CreateLinks creates hardlinks/reflinks/copies at the target location.
	// The link mode is determined during Prepare and stored in PrepareResult.
	// Returns the number of files linked or an error.
	CreateLinks(ctx context.Context, t *models.Transfer, prep *PrepareResult) (int, error)

	// AddTorrent adds the torrent to the target instance.
	// This includes exporting from source, ensuring categories exist, and adding to target.
	AddTorrent(ctx context.Context, t *models.Transfer, prep *PrepareResult) error

	// DeleteSource removes the torrent from the source instance.
	// Files are kept on disk (they're hardlinked to the target).
	DeleteSource(ctx context.Context, t *models.Transfer) error

	// Rollback cleans up any created files on failure.
	// This is called when a transfer fails after links were created.
	Rollback(ctx context.Context, t *models.Transfer, prep *PrepareResult) error

	// CanHandle returns true if this executor can handle the given transfer.
	// Used for executor selection based on instance configuration.
	CanHandle(source, target *models.Instance) bool
}

// PrepareResult contains information gathered during the prepare phase.
// This is passed through subsequent phases to avoid re-fetching data.
type PrepareResult struct {
	// TorrentName is the human-readable name of the torrent.
	TorrentName string

	// SourceSavePath is the absolute path where files exist on the source.
	SourceSavePath string

	// TargetSavePath is the absolute path where files should be placed on the target.
	TargetSavePath string

	// LinkMode indicates how files should be linked: "hardlink", "reflink", or "direct".
	LinkMode string

	// Files is the list of files in the torrent with their paths and sizes.
	Files []TorrentFile

	// TorrentData is the exported .torrent file content.
	TorrentData []byte

	// Category is the category to assign on the target (if preserving).
	Category string

	// Tags is the list of tags to assign on the target (if preserving).
	Tags []string

	// SourceInstance is cached for use in later phases.
	SourceInstance *models.Instance

	// TargetInstance is cached for use in later phases.
	TargetInstance *models.Instance
}

// TorrentFile represents a single file in a torrent.
type TorrentFile struct {
	// RelPath is the path relative to the torrent's save directory.
	RelPath string

	// AbsPath is the absolute path on disk.
	AbsPath string

	// Size is the file size in bytes.
	Size int64
}

// ValidateRelPath checks if a relative path is safe to use.
// It rejects absolute paths, paths with traversal elements (..),
// and paths that could escape the base directory.
func ValidateRelPath(relPath string) error {
	if relPath == "" {
		return errors.New("relative path cannot be empty")
	}
	// Reject absolute paths
	if filepath.IsAbs(relPath) {
		return errors.New("relative path cannot be absolute")
	}
	// Clean the path and check for traversal. After cleaning, if the path
	// starts with ".." it means it would escape the base directory.
	// Note: filepath.Clean resolves intermediate ".." (e.g., "a/b/../c" -> "a/c")
	// but preserves leading ".." that would escape (e.g., "a/../.." -> "..").
	cleaned := filepath.Clean(relPath)
	if strings.HasPrefix(cleaned, "..") {
		return errors.New("relative path contains traversal elements")
	}
	return nil
}

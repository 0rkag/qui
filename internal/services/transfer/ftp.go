// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/ftpclient"
)

// FTPExecutor handles transfers where file operations are performed via FTP/FTPS.
// This supports deployments where instances have FTP access but not SSH.
// MVP: Only relay mode (data flows through QUI). FXP will be added later.
type FTPExecutor struct {
	syncManager        SyncManager
	instanceStore      InstanceProvider
	ftpConnectionStore *models.InstanceFTPConnectionStore
	pathResolver       *models.PathResolver
	ftpPool            *ftpclient.Pool
}

// NewFTPExecutor creates a new FTPExecutor.
func NewFTPExecutor(
	syncManager SyncManager,
	instanceStore InstanceProvider,
	ftpConnectionStore *models.InstanceFTPConnectionStore,
	pathMappingStore *models.InstancePathMappingStore,
) *FTPExecutor {
	var resolver *models.PathResolver
	if pathMappingStore != nil {
		resolver = models.NewPathResolver(pathMappingStore)
	}
	return &FTPExecutor{
		syncManager:        syncManager,
		instanceStore:      instanceStore,
		ftpConnectionStore: ftpConnectionStore,
		pathResolver:       resolver,
		ftpPool:            ftpclient.NewPool(0), // Use default idle time
	}
}

// CanHandle returns true if at least one instance has an FTP connection configured
// and no SSH connection is available (FTP is lower priority than SSH).
func (e *FTPExecutor) CanHandle(source, target *models.Instance) bool {
	// FTP is only used when SSH is not available
	// Check is done by caller via executor registry order
	ctx := context.Background()

	sourceFTP, _ := e.ftpConnectionStore.GetByInstance(ctx, source.ID)
	targetFTP, _ := e.ftpConnectionStore.GetByInstance(ctx, target.ID)

	sourceHasFTP := sourceFTP != nil && sourceFTP.Enabled
	targetHasFTP := targetFTP != nil && targetFTP.Enabled

	// We can handle if at least one side has FTP and that side needs remote access
	sourceNeedsRemote := !source.HasLocalFilesystemAccess
	targetNeedsRemote := !target.HasLocalFilesystemAccess

	return (sourceNeedsRemote && sourceHasFTP) || (targetNeedsRemote && targetHasFTP)
}

// Prepare validates the transfer and gathers source information.
func (e *FTPExecutor) Prepare(ctx context.Context, t *models.Transfer) (*PrepareResult, error) {
	// 1. Get instances
	sourceInstance, err := e.instanceStore.Get(ctx, t.SourceInstanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get source instance: %w", err)
	}

	targetInstance, err := e.instanceStore.Get(ctx, t.TargetInstanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get target instance: %w", err)
	}

	// 2. Get source torrent info (via qBittorrent API)
	torrents, err := e.syncManager.GetTorrents(ctx, t.SourceInstanceID,
		qbt.TorrentFilterOptions{Hashes: []string{t.TorrentHash}})
	if err != nil {
		return nil, fmt.Errorf("failed to get source torrent: %w", err)
	}
	if len(torrents) == 0 {
		return nil, ErrTorrentNotFound
	}
	sourceTorrent := torrents[0]

	// 3. Get source files
	files, err := e.syncManager.GetTorrentFiles(ctx, t.SourceInstanceID, t.TorrentHash)
	if err != nil {
		return nil, fmt.Errorf("failed to get source files: %w", err)
	}

	// 4. Get properties (save path)
	props, err := e.syncManager.GetTorrentProperties(ctx, t.SourceInstanceID, t.TorrentHash)
	if err != nil {
		return nil, fmt.Errorf("failed to get source properties: %w", err)
	}

	// 5. Build result
	result := &PrepareResult{
		TorrentName:    sourceTorrent.Name,
		SourceSavePath: props.SavePath,
		SourceInstance: sourceInstance,
		TargetInstance: targetInstance,
		LinkMode:       "transfer", // FTP always transfers (no hardlinks)
	}

	// 6. Build file list - only include complete files
	result.Files = make([]TorrentFile, 0, len(*files))
	var skippedFiles int
	for _, f := range *files {
		if f.Progress < 1.0 {
			skippedFiles++
			continue
		}

		if err := ValidateRelPath(f.Name); err != nil {
			return nil, fmt.Errorf("unsafe file path in torrent %q: %w", f.Name, err)
		}
		result.Files = append(result.Files, TorrentFile{
			RelPath: f.Name,
			AbsPath: filepath.Join(props.SavePath, f.Name),
			Size:    f.Size,
		})
	}

	if len(result.Files) == 0 {
		return result, fmt.Errorf("no complete files to transfer (torrent is %.1f%% complete)", sourceTorrent.Progress*100)
	}

	// 7. Extract category and tags
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

	// 8. Compute target save path
	if e.pathResolver != nil {
		resolvedPath, err := e.pathResolver.ResolveTargetPath(
			ctx,
			props.SavePath,
			t.SourceInstanceID,
			t.TargetInstanceID,
			t.PathMappings,
		)
		if err != nil {
			log.Warn().Err(err).Msg("[TRANSFER-FTP] Path resolution failed, using source path")
			result.TargetSavePath = props.SavePath
		} else {
			result.TargetSavePath = resolvedPath
		}
	} else {
		result.TargetSavePath = e.computeTargetPath(props.SavePath, targetInstance, t.PathMappings)
	}

	// 9. Export torrent for later use
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
		Int("files", len(result.Files)).
		Msg("[TRANSFER-FTP] Prepared transfer")

	return result, nil
}

// CreateLinks transfers files via FTP (relay mode).
func (e *FTPExecutor) CreateLinks(ctx context.Context, t *models.Transfer, prep *PrepareResult) (int, error) {
	sourceFTP, _ := e.ftpConnectionStore.GetByInstance(ctx, t.SourceInstanceID)
	targetFTP, _ := e.ftpConnectionStore.GetByInstance(ctx, t.TargetInstanceID)

	return e.transferFiles(ctx, t, prep, sourceFTP, targetFTP)
}

// AddTorrent adds the torrent to the target instance.
func (e *FTPExecutor) AddTorrent(ctx context.Context, t *models.Transfer, prep *PrepareResult) error {
	options := map[string]string{
		"autoTMM":       "false",
		"savepath":      prep.TargetSavePath,
		"skip_checking": "true",
		"contentLayout": "Original",
	}

	if prep.Category != "" {
		options["category"] = prep.Category
	}

	if len(prep.Tags) > 0 {
		options["tags"] = strings.Join(prep.Tags, ",")
	}

	options["paused"] = "true"
	options["stopped"] = "true"

	if err := e.syncManager.AddTorrent(ctx, t.TargetInstanceID, prep.TorrentData, options); err != nil {
		return fmt.Errorf("failed to add torrent to target: %w", err)
	}

	log.Info().
		Int64("id", t.ID).
		Str("hash", t.TorrentHash).
		Int("targetInstance", t.TargetInstanceID).
		Msg("[TRANSFER-FTP] Added torrent to target instance")

	return nil
}

// DeleteSource removes the torrent from the source instance.
func (e *FTPExecutor) DeleteSource(ctx context.Context, t *models.Transfer) error {
	if err := e.syncManager.DeleteTorrents(ctx, t.SourceInstanceID, []string{t.TorrentHash}, false); err != nil {
		return fmt.Errorf("failed to delete from source: %w", err)
	}

	log.Info().
		Int64("id", t.ID).
		Int("sourceInstance", t.SourceInstanceID).
		Msg("[TRANSFER-FTP] Deleted torrent from source instance")

	return nil
}

// Rollback cleans up any created files on failure.
func (e *FTPExecutor) Rollback(ctx context.Context, t *models.Transfer, prep *PrepareResult) error {
	if prep == nil || prep.TargetSavePath == "" {
		return nil
	}

	log.Debug().Int64("id", t.ID).Str("path", prep.TargetSavePath).Msg("[TRANSFER-FTP] Rolling back files")

	targetFTP, err := e.ftpConnectionStore.GetByInstance(ctx, t.TargetInstanceID)
	if err != nil {
		log.Warn().Err(err).Int64("id", t.ID).Msg("[TRANSFER-FTP] Cannot get FTP connection for rollback")
		return err
	}

	client, err := e.getFTPClient(ctx, targetFTP)
	if err != nil {
		return err
	}

	// Remove the files we created
	for _, f := range prep.Files {
		targetPath := path.Join(prep.TargetSavePath, f.RelPath)
		if err := client.Remove(targetPath); err != nil {
			log.Warn().Err(err).Str("path", targetPath).Msg("[TRANSFER-FTP] Failed to remove file during rollback")
		}
	}

	return nil
}

// getFTPClient creates an FTP client from a connection config.
func (e *FTPExecutor) getFTPClient(ctx context.Context, conn *models.InstanceFTPConnection) (*ftpclient.Client, error) {
	if conn == nil || !conn.Enabled {
		return nil, fmt.Errorf("FTP connection not configured or disabled")
	}

	password, err := e.ftpConnectionStore.GetDecryptedPassword(conn)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt password: %w", err)
	}

	cfg := &ftpclient.Config{
		Host:          conn.Host,
		Port:          conn.Port,
		Username:      conn.Username,
		Password:      password,
		UseTLS:        conn.UseTLS,
		TLSSkipVerify: conn.TLSSkipVerify,
		PassiveMode:   conn.PassiveMode,
	}

	return e.ftpPool.Get(cfg)
}

// transferFiles transfers files via FTP relay (data flows through QUI).
func (e *FTPExecutor) transferFiles(ctx context.Context, t *models.Transfer, prep *PrepareResult, sourceFTP, targetFTP *models.InstanceFTPConnection) (int, error) {
	sourceHasLocal := prep.SourceInstance.HasLocalFilesystemAccess
	targetHasLocal := prep.TargetInstance.HasLocalFilesystemAccess

	// Build source path
	sourcePath := path.Join(prep.SourceSavePath, prep.TorrentName)
	if len(prep.Files) == 1 && prep.Files[0].RelPath == prep.TorrentName {
		sourcePath = prep.Files[0].AbsPath
	}

	targetDir := prep.TargetSavePath
	targetPath := path.Join(targetDir, prep.TorrentName)

	opts := ftpclient.TransferOptions{
		PreservePermissions: false, // FTP doesn't preserve permissions well
	}

	switch {
	case sourceHasLocal && targetFTP != nil:
		// Upload from local to remote FTP
		client, err := e.getFTPClient(ctx, targetFTP)
		if err != nil {
			return 0, fmt.Errorf("failed to create target FTP client: %w", err)
		}

		info, err := os.Stat(sourcePath)
		if err != nil {
			return 0, fmt.Errorf("failed to stat source: %w", err)
		}

		if info.IsDir() {
			if err := client.UploadTree(ctx, sourcePath, targetPath, opts); err != nil {
				return 0, fmt.Errorf("FTP upload tree failed: %w", err)
			}
		} else {
			if err := client.Upload(ctx, sourcePath, targetPath, opts); err != nil {
				return 0, fmt.Errorf("FTP upload failed: %w", err)
			}
		}

	case targetHasLocal && sourceFTP != nil:
		// Download from remote FTP to local
		client, err := e.getFTPClient(ctx, sourceFTP)
		if err != nil {
			return 0, fmt.Errorf("failed to create source FTP client: %w", err)
		}

		isDir, err := client.IsDir(sourcePath)
		if err != nil {
			return 0, fmt.Errorf("failed to check source: %w", err)
		}

		if isDir {
			if err := client.DownloadTree(ctx, sourcePath, targetPath, opts); err != nil {
				return 0, fmt.Errorf("FTP download tree failed: %w", err)
			}
		} else {
			if err := client.Download(ctx, sourcePath, targetPath, opts); err != nil {
				return 0, fmt.Errorf("FTP download failed: %w", err)
			}
		}

	case sourceFTP != nil && targetFTP != nil:
		// FTP to FTP relay through QUI
		srcClient, err := e.getFTPClient(ctx, sourceFTP)
		if err != nil {
			return 0, fmt.Errorf("failed to create source FTP client: %w", err)
		}

		dstClient, err := e.getFTPClient(ctx, targetFTP)
		if err != nil {
			return 0, fmt.Errorf("failed to create target FTP client: %w", err)
		}

		if err := ftpclient.RelayTransfer(ctx, srcClient, dstClient, sourcePath, targetPath, opts); err != nil {
			return 0, fmt.Errorf("FTP relay transfer failed: %w", err)
		}

	default:
		return 0, fmt.Errorf("cannot determine FTP transfer direction")
	}

	log.Info().
		Int64("id", t.ID).
		Int("files", len(prep.Files)).
		Str("targetDir", targetDir).
		Msg("[TRANSFER-FTP] Files transferred via FTP")

	return len(prep.Files), nil
}

// computeTargetPath determines where files should be placed on target.
func (e *FTPExecutor) computeTargetPath(sourcePath string, targetInstance *models.Instance, mappings map[string]string) string {
	if len(mappings) > 0 {
		resolved := models.ApplyDirectMappings(sourcePath, mappings)
		if resolved != sourcePath {
			return resolved
		}
	}

	if targetInstance.HardlinkBaseDir != "" {
		return targetInstance.HardlinkBaseDir
	}

	return sourcePath
}

// Close shuts down the FTP connection pool.
func (e *FTPExecutor) Close() error {
	return e.ftpPool.Close()
}

// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer

import (
	"context"
	"fmt"
	"os"
	"path"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/ftpclient"
)

// ftpFileExistsActionToMode converts FileExistsAction to ftpclient.FileExistsMode.
func ftpFileExistsActionToMode(action models.FileExistsAction) ftpclient.FileExistsMode {
	switch action {
	case models.FileExistsSkip:
		return ftpclient.FileExistsModeSkip
	case models.FileExistsOverwrite:
		return ftpclient.FileExistsModeOverwrite
	default:
		return ftpclient.FileExistsModeAbort
	}
}

// FTPExecutor handles transfers where file operations are performed via FTP/FTPS.
// This supports deployments where instances have FTP access but not SSH.
// MVP: Only relay mode (data flows through QUI). FXP will be added later.
type FTPExecutor struct {
	syncManager     SyncManager
	instanceStore   InstanceProvider
	connectionStore *models.InstanceConnectionStore
	pathResolver    *models.PathResolver
	ftpPool         *ftpclient.Pool
}

// NewFTPExecutor creates a new FTPExecutor.
func NewFTPExecutor(
	syncManager SyncManager,
	instanceStore InstanceProvider,
	connectionStore *models.InstanceConnectionStore,
	pathMappingStore *models.InstancePathMappingStore,
) *FTPExecutor {
	var resolver *models.PathResolver
	if pathMappingStore != nil {
		resolver = models.NewPathResolver(pathMappingStore)
	}
	return &FTPExecutor{
		syncManager:     syncManager,
		instanceStore:   instanceStore,
		connectionStore: connectionStore,
		pathResolver:    resolver,
		ftpPool:         ftpclient.NewPool(0), // Use default idle time
	}
}

// CanHandle returns true if at least one instance has an FTP connection configured
// and no SSH connection is available (FTP is lower priority than SSH).
func (e *FTPExecutor) CanHandle(source, target *models.Instance) bool {
	ctx := context.Background()

	sourceFTP, err := e.connectionStore.GetFTPByInstance(ctx, source.ID)
	if err != nil && err != models.ErrConnectionNotFound {
		log.Warn().Err(err).Int("instanceID", source.ID).Msg("[TRANSFER-FTP] Error checking source FTP connection")
	}
	targetFTP, err := e.connectionStore.GetFTPByInstance(ctx, target.ID)
	if err != nil && err != models.ErrConnectionNotFound {
		log.Warn().Err(err).Int("instanceID", target.ID).Msg("[TRANSFER-FTP] Error checking target FTP connection")
	}

	sourceHasFTP := sourceFTP != nil && sourceFTP.Enabled
	targetHasFTP := targetFTP != nil && targetFTP.Enabled

	sourceNeedsRemote := !source.HasLocalFilesystemAccess
	targetNeedsRemote := !target.HasLocalFilesystemAccess

	return (sourceNeedsRemote && sourceHasFTP) || (targetNeedsRemote && targetHasFTP)
}

// Prepare validates the transfer and gathers source information.
func (e *FTPExecutor) Prepare(ctx context.Context, t *models.Transfer) (*PrepareResult, error) {
	const logPrefix = "[TRANSFER-FTP]"

	// 1. Run common preparation (get instances, torrent, files, validate)
	common, err := prepareCommon(ctx, t, e.syncManager, e.instanceStore, PrepareConfig{
		LogPrefix:          logPrefix,
		RequireLocalSource: false, // FTP executor doesn't require local access
		RequireLocalTarget: false,
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

	// 3. FTP always uses "transfer" mode - no torrent export needed for FTP
	// since we add torrent via API, not via .torrent file transfer
	result := buildPrepareResult(common, targetSavePath, "transfer", nil)

	log.Info().
		Int64("id", t.ID).
		Str("name", result.TorrentName).
		Str("sourcePath", result.SourceSavePath).
		Str("targetPath", result.TargetSavePath).
		Int("files", len(result.Files)).
		Msg("[TRANSFER-FTP] Prepared transfer")

	return result, nil
}

// Execute performs the file transfer.
func (e *FTPExecutor) Execute(ctx context.Context, t *models.Transfer, prep *PrepareResult) error {
	sourceFTP, err := e.connectionStore.GetFTPByInstance(ctx, prep.SourceInstance.ID)
	if err != nil && err != models.ErrConnectionNotFound {
		return fmt.Errorf("failed to get source FTP connection: %w", err)
	}
	targetFTP, err := e.connectionStore.GetFTPByInstance(ctx, prep.TargetInstance.ID)
	if err != nil && err != models.ErrConnectionNotFound {
		return fmt.Errorf("failed to get target FTP connection: %w", err)
	}

	transferred, err := e.transferFiles(ctx, t, prep, sourceFTP, targetFTP)
	if err != nil {
		return err
	}

	log.Info().
		Int("files", transferred).
		Str("hash", t.TorrentHash).
		Msg("ftp: transfer complete")

	return nil
}

// Cleanup performs any necessary cleanup after transfer.
func (e *FTPExecutor) Cleanup(ctx context.Context, t *models.Transfer, prep *PrepareResult) error {
	return nil
}

// Close releases all resources held by the executor.
func (e *FTPExecutor) Close() error {
	if e.ftpPool != nil {
		return e.ftpPool.Close()
	}
	return nil
}

// getFTPClient creates an FTP client from a connection config.
func (e *FTPExecutor) getFTPClient(ctx context.Context, conn *models.InstanceConnection) (*ftpclient.Client, error) {
	if conn == nil || !conn.Enabled {
		return nil, fmt.Errorf("FTP connection not configured or disabled")
	}

	password, err := e.connectionStore.GetDecryptedPassword(conn)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt password: %w", err)
	}

	var tlsMode string
	switch conn.Type {
	case models.ConnectionTypeFTPExplicit:
		tlsMode = ftpclient.TLSModeExplicit
	case models.ConnectionTypeFTPImplicit:
		tlsMode = ftpclient.TLSModeImplicit
	case models.ConnectionTypeFTPPlain:
		tlsMode = ftpclient.TLSModeNone
	default:
		tlsMode = ftpclient.TLSModeExplicit
	}

	cfg := &ftpclient.Config{
		Host:          conn.Host,
		Port:          conn.Port,
		Username:      conn.Username,
		Password:      password,
		TLSMode:       tlsMode,
		SkipTLSVerify: conn.TLSSkipVerify,
	}

	return e.ftpPool.Get(cfg)
}

// transferFiles transfers files via FTP relay (data flows through QUI).
func (e *FTPExecutor) transferFiles(ctx context.Context, t *models.Transfer, prep *PrepareResult, sourceFTP, targetFTP *models.InstanceConnection) (int, error) {
	sourceHasLocal := prep.SourceInstance.HasLocalFilesystemAccess
	targetHasLocal := prep.TargetInstance.HasLocalFilesystemAccess

	sourcePath := path.Join(prep.SourceSavePath, prep.TorrentName)
	if len(prep.Files) == 1 && prep.Files[0].RelPath == prep.TorrentName {
		sourcePath = prep.Files[0].AbsPath
	}

	targetDir := prep.TargetSavePath
	targetPath := path.Join(targetDir, prep.TorrentName)

	opts := ftpclient.TransferOptions{
		PreservePermissions: false,
		FileExistsMode:      ftpFileExistsActionToMode(t.FileExistsAction),
	}

	switch {
	case sourceHasLocal && targetFTP != nil:
		client, err := e.getFTPClient(ctx, targetFTP)
		if err != nil {
			return 0, fmt.Errorf("failed to get target FTP client: %w", err)
		}

		info, err := os.Stat(sourcePath)
		if err != nil {
			return 0, fmt.Errorf("failed to stat source: %w", err)
		}

		if info.IsDir() {
			if err := client.UploadTree(ctx, sourcePath, targetPath, opts); err != nil {
				return 0, fmt.Errorf("failed to upload directory: %w", err)
			}
		} else {
			if err := client.Upload(ctx, sourcePath, targetPath, opts); err != nil {
				return 0, fmt.Errorf("failed to upload file: %w", err)
			}
		}
		return len(prep.Files), nil

	case sourceFTP != nil && targetHasLocal:
		client, err := e.getFTPClient(ctx, sourceFTP)
		if err != nil {
			return 0, fmt.Errorf("failed to get source FTP client: %w", err)
		}

		isDir, err := client.IsDir(sourcePath)
		if err != nil {
			return 0, fmt.Errorf("failed to check source type: %w", err)
		}

		if isDir {
			if err := client.DownloadTree(ctx, sourcePath, targetPath, opts); err != nil {
				return 0, fmt.Errorf("failed to download directory: %w", err)
			}
		} else {
			if err := client.Download(ctx, sourcePath, targetPath, opts); err != nil {
				return 0, fmt.Errorf("failed to download file: %w", err)
			}
		}
		return len(prep.Files), nil

	case sourceFTP != nil && targetFTP != nil:
		sourceClient, err := e.getFTPClient(ctx, sourceFTP)
		if err != nil {
			return 0, fmt.Errorf("failed to get source FTP client: %w", err)
		}

		targetClient, err := e.getFTPClient(ctx, targetFTP)
		if err != nil {
			return 0, fmt.Errorf("failed to get target FTP client: %w", err)
		}

		if err := ftpclient.RelayTransfer(ctx, sourceClient, targetClient, sourcePath, targetPath, opts); err != nil {
			return 0, fmt.Errorf("failed to relay transfer: %w", err)
		}
		return len(prep.Files), nil

	default:
		return 0, fmt.Errorf("no valid FTP configuration for transfer")
	}
}

// TransferName returns a human-readable name for this executor.
func (e *FTPExecutor) TransferName() string {
	return "FTP"
}

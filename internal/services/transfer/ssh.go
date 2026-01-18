// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/sshclient"
)

// SSHExecutor handles transfers where file operations are performed via SSH.
// This supports deployments where QUI is remote from the qBittorrent instances
// or where instances are on different machines.
type SSHExecutor struct {
	syncManager     SyncManager
	instanceStore   InstanceProvider
	connectionStore *models.InstanceConnectionStore
	pathResolver    *models.PathResolver
	sshPool         *sshclient.Pool
}

// NewSSHExecutor creates a new SSHExecutor.
func NewSSHExecutor(
	syncManager SyncManager,
	instanceStore InstanceProvider,
	connectionStore *models.InstanceConnectionStore,
	pathMappingStore *models.InstancePathMappingStore,
) *SSHExecutor {
	var resolver *models.PathResolver
	if pathMappingStore != nil {
		resolver = models.NewPathResolver(pathMappingStore)
	}
	return &SSHExecutor{
		syncManager:     syncManager,
		instanceStore:   instanceStore,
		connectionStore: connectionStore,
		pathResolver:    resolver,
		sshPool:         sshclient.NewPool(0), // Use default idle time
	}
}

// CanHandle returns true if at least one instance has an SSH connection configured.
func (e *SSHExecutor) CanHandle(source, target *models.Instance) bool {
	// We can handle if at least one side needs SSH
	sourceNeedsSSH := !source.HasLocalFilesystemAccess
	targetNeedsSSH := !target.HasLocalFilesystemAccess
	return sourceNeedsSSH || targetNeedsSSH
}

// Prepare validates the transfer and gathers source information.
func (e *SSHExecutor) Prepare(ctx context.Context, t *models.Transfer) (*PrepareResult, error) {
	const logPrefix = "[TRANSFER-SSH]"

	// 1. Run common preparation (get instances, torrent, files, validate)
	common, err := prepareCommon(ctx, t, e.syncManager, e.instanceStore, PrepareConfig{
		LogPrefix:          logPrefix,
		RequireLocalSource: false, // SSH executor doesn't require local access
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

	// 3. Determine link mode for remote transfers
	linkMode := e.determineLinkMode(common.SourceInstance, common.TargetInstance)

	// 4. Export torrent for later use
	torrentBytes, _, _, err := e.syncManager.ExportTorrent(ctx, t.SourceInstanceID, t.TorrentHash)
	if err != nil {
		return nil, fmt.Errorf("failed to export torrent: %w", err)
	}

	// 5. Build final result
	result := buildPrepareResult(common, targetSavePath, linkMode, torrentBytes)

	log.Info().
		Int64("id", t.ID).
		Str("name", result.TorrentName).
		Str("sourcePath", result.SourceSavePath).
		Str("targetPath", result.TargetSavePath).
		Str("linkMode", result.LinkMode).
		Int("files", len(result.Files)).
		Msg("[TRANSFER-SSH] Prepared transfer")

	return result, nil
}

// CreateLinks creates files at the target location via SSH/rsync.
func (e *SSHExecutor) CreateLinks(ctx context.Context, t *models.Transfer, prep *PrepareResult) (int, error) {
	// Get SSH connections for source and target (errors are ok - means no SSH configured for that instance)
	sourceSSH, err := e.connectionStore.GetSSHByInstance(ctx, t.SourceInstanceID)
	if err != nil && err != models.ErrConnectionNotFound {
		log.Warn().Err(err).Int("instanceID", t.SourceInstanceID).Msg("[TRANSFER-SSH] Failed to get source SSH connection")
	}
	targetSSH, err := e.connectionStore.GetSSHByInstance(ctx, t.TargetInstanceID)
	if err != nil && err != models.ErrConnectionNotFound {
		log.Warn().Err(err).Int("instanceID", t.TargetInstanceID).Msg("[TRANSFER-SSH] Failed to get target SSH connection")
	}

	switch prep.LinkMode {
	case "transfer":
		// Files need to be transferred via rsync
		return e.transferFiles(ctx, t, prep, sourceSSH, targetSSH)

	case "hardlink", "reflink":
		// Same machine - create links via SSH
		return e.createRemoteLinks(ctx, t, prep, targetSSH)

	case "copy":
		// Same machine but no linking support - copy files via SSH
		return e.copyRemoteFiles(ctx, t, prep, targetSSH)

	case "direct":
		// Shared storage - no file operations needed
		log.Debug().Int64("id", t.ID).Msg("[TRANSFER-SSH] Direct mode - skipping file creation")
		return 0, nil

	default:
		return 0, fmt.Errorf("unsupported link mode: %s", prep.LinkMode)
	}
}

// AddTorrent adds the torrent to the target instance.
func (e *SSHExecutor) AddTorrent(ctx context.Context, t *models.Transfer, prep *PrepareResult) error {
	// Build add options
	options := map[string]string{
		"autoTMM":       "false",
		"savepath":      prep.TargetSavePath,
		"skip_checking": "true",
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

	options["paused"] = "true"
	options["stopped"] = "true"

	if err := e.syncManager.AddTorrent(ctx, t.TargetInstanceID, prep.TorrentData, options); err != nil {
		return fmt.Errorf("failed to add torrent to target: %w", err)
	}

	log.Info().
		Int64("id", t.ID).
		Str("hash", t.TorrentHash).
		Int("targetInstance", t.TargetInstanceID).
		Msg("[TRANSFER-SSH] Added torrent to target instance")

	return nil
}

// DeleteSource removes the torrent from the source instance.
func (e *SSHExecutor) DeleteSource(ctx context.Context, t *models.Transfer) error {
	if err := e.syncManager.DeleteTorrents(ctx, t.SourceInstanceID, []string{t.TorrentHash}, false); err != nil {
		return fmt.Errorf("failed to delete from source: %w", err)
	}

	log.Info().
		Int64("id", t.ID).
		Int("sourceInstance", t.SourceInstanceID).
		Msg("[TRANSFER-SSH] Deleted torrent from source instance")

	return nil
}

// Rollback cleans up any created files on failure.
func (e *SSHExecutor) Rollback(ctx context.Context, t *models.Transfer, prep *PrepareResult) error {
	if prep == nil || prep.LinkMode == "direct" || prep.TargetSavePath == "" {
		return nil
	}

	log.Debug().Int64("id", t.ID).Str("path", prep.TargetSavePath).Msg("[TRANSFER-SSH] Rolling back files")

	// Get target SSH connection
	targetSSH, err := e.connectionStore.GetSSHByInstance(ctx, t.TargetInstanceID)
	if err != nil {
		log.Warn().Err(err).Int64("id", t.ID).Msg("[TRANSFER-SSH] Cannot get SSH connection for rollback")
		return err
	}

	client, err := e.getSSHClient(targetSSH)
	if err != nil {
		return err
	}

	// Remove the files we created
	for _, f := range prep.Files {
		targetPath := filepath.Join(prep.TargetSavePath, f.RelPath)
		if err := client.Remove(ctx, targetPath); err != nil {
			log.Warn().Err(err).Str("path", targetPath).Msg("[TRANSFER-SSH] Failed to remove file during rollback")
		}
	}

	return nil
}

// getSSHClient gets or creates an SSH client from the pool.
func (e *SSHExecutor) getSSHClient(conn *models.InstanceConnection) (*sshclient.Client, error) {
	if conn == nil || !conn.Enabled {
		return nil, fmt.Errorf("SSH connection not configured or disabled")
	}

	cfg := &sshclient.Config{
		Host:           conn.Host,
		Port:           conn.Port,
		Username:       conn.Username,
		PrivateKeyPath: conn.PrivateKeyPath,
	}

	return e.sshPool.Get(cfg)
}

// Transfer method constants for internal use
const (
	transferMethodRsync = "rsync"
	transferMethodSFTP  = "sftp"
	transferMethodSCP   = "scp"
)

// resolveTransferMethod determines whether to use rsync or SFTP for a transfer.
func (e *SSHExecutor) resolveTransferMethod(sourceSSH, targetSSH *models.InstanceConnection) string {
	// Check connection type preference
	conn := targetSSH
	if conn == nil {
		conn = sourceSSH
	}
	if conn == nil {
		return transferMethodSFTP // Fallback to SFTP if no connection
	}

	// Connection type determines the transfer method
	switch conn.Type {
	case models.ConnectionTypeSSHRsync:
		return transferMethodRsync
	case models.ConnectionTypeSSHSFTP:
		return transferMethodSFTP
	case models.ConnectionTypeSSHSCP:
		return transferMethodSCP
	case models.ConnectionTypeSSHAuto:
		// Auto-detect: prefer rsync if available
		if conn.RsyncAvailable != nil && *conn.RsyncAvailable {
			return transferMethodRsync
		}
		return transferMethodSFTP
	default:
		return transferMethodSFTP
	}
}

// transferFiles transfers files via rsync or SFTP based on capabilities.
func (e *SSHExecutor) transferFiles(ctx context.Context, t *models.Transfer, prep *PrepareResult, sourceSSH, targetSSH *models.InstanceConnection) (int, error) {
	method := e.resolveTransferMethod(sourceSSH, targetSSH)

	log.Debug().
		Int64("id", t.ID).
		Str("method", method).
		Msg("[TRANSFER-SSH] Using transfer method")

	switch method {
	case transferMethodRsync:
		count, err := e.transferFilesRsync(ctx, t, prep, sourceSSH, targetSSH)
		if err != nil {
			// If rsync fails, try SFTP as fallback
			log.Warn().Err(err).Int64("id", t.ID).Msg("[TRANSFER-SSH] Rsync failed, falling back to SFTP")
			return e.transferFilesSFTP(ctx, t, prep, sourceSSH, targetSSH)
		}
		return count, nil

	case transferMethodSFTP:
		return e.transferFilesSFTP(ctx, t, prep, sourceSSH, targetSSH)

	case transferMethodSCP:
		// SCP is not yet implemented, fall back to SFTP
		log.Warn().Int64("id", t.ID).Msg("[TRANSFER-SSH] SCP not implemented, using SFTP")
		return e.transferFilesSFTP(ctx, t, prep, sourceSSH, targetSSH)

	default:
		return 0, fmt.Errorf("unsupported transfer method: %s", method)
	}
}

// transferFilesRsync transfers files via rsync.
func (e *SSHExecutor) transferFilesRsync(ctx context.Context, t *models.Transfer, prep *PrepareResult, sourceSSH, targetSSH *models.InstanceConnection) (int, error) {
	// Determine rsync direction based on which side has SSH
	sourceHasLocal := prep.SourceInstance.HasLocalFilesystemAccess
	targetHasLocal := prep.TargetInstance.HasLocalFilesystemAccess

	// Build source path (include torrent name for multi-file torrents)
	sourcePath := filepath.Join(prep.SourceSavePath, prep.TorrentName)
	if len(prep.Files) == 1 && prep.Files[0].RelPath == prep.TorrentName {
		// Single file torrent - just use the file path
		sourcePath = prep.Files[0].AbsPath
	} else {
		// Multi-file torrent - add trailing slash for rsync directory sync
		sourcePath += "/"
	}

	// Ensure target directory exists
	targetDir := prep.TargetSavePath
	if err := e.ensureTargetDir(ctx, targetDir, targetSSH, targetHasLocal); err != nil {
		return 0, fmt.Errorf("failed to create target directory: %w", err)
	}

	opts := &sshclient.TransferOptions{PreservePermissions: true}

	switch {
	case sourceHasLocal && targetSSH != nil:
		// Push from local to remote
		cfg := e.sshConfigFromConnection(targetSSH)
		_, err := sshclient.RsyncPush(ctx, cfg, sourcePath, targetDir, opts)
		if err != nil {
			return 0, fmt.Errorf("rsync push failed: %w", err)
		}

	case targetHasLocal && sourceSSH != nil:
		// Pull from remote to local
		cfg := e.sshConfigFromConnection(sourceSSH)
		_, err := sshclient.RsyncPull(ctx, cfg, sourcePath, targetDir, opts)
		if err != nil {
			return 0, fmt.Errorf("rsync pull failed: %w", err)
		}

	case sourceSSH != nil && targetSSH != nil:
		// Remote to remote transfer
		srcCfg := e.sshConfigFromConnection(sourceSSH)
		dstCfg := e.sshConfigFromConnection(targetSSH)
		_, err := sshclient.RsyncRemoteToRemote(ctx, srcCfg, dstCfg, sourcePath, targetDir, opts)
		if err != nil {
			return 0, fmt.Errorf("rsync remote-to-remote failed: %w", err)
		}

	default:
		return 0, fmt.Errorf("cannot determine rsync direction")
	}

	log.Info().
		Int64("id", t.ID).
		Int("files", len(prep.Files)).
		Str("targetDir", targetDir).
		Msg("[TRANSFER-SSH] Files transferred via rsync")

	return len(prep.Files), nil
}

// transferFilesSFTP transfers files via SFTP.
func (e *SSHExecutor) transferFilesSFTP(ctx context.Context, t *models.Transfer, prep *PrepareResult, sourceSSH, targetSSH *models.InstanceConnection) (int, error) {
	sourceHasLocal := prep.SourceInstance.HasLocalFilesystemAccess
	targetHasLocal := prep.TargetInstance.HasLocalFilesystemAccess

	// Build source path
	sourcePath := filepath.Join(prep.SourceSavePath, prep.TorrentName)
	if len(prep.Files) == 1 && prep.Files[0].RelPath == prep.TorrentName {
		sourcePath = prep.Files[0].AbsPath
	}

	targetDir := prep.TargetSavePath
	targetPath := filepath.Join(targetDir, prep.TorrentName)

	opts := sshclient.SFTPTransferOptions{
		PreservePermissions: true,
	}

	switch {
	case sourceHasLocal && targetSSH != nil:
		// Upload from local to remote
		sftpClient, err := e.getSFTPClient(targetSSH)
		if err != nil {
			return 0, fmt.Errorf("failed to create SFTP client: %w", err)
		}
		defer sftpClient.Close()

		// Check if source is a directory or file
		info, err := os.Stat(sourcePath)
		if err != nil {
			return 0, fmt.Errorf("failed to stat source: %w", err)
		}

		if info.IsDir() {
			if err := sftpClient.UploadTree(ctx, sourcePath, targetPath, opts); err != nil {
				return 0, fmt.Errorf("SFTP upload tree failed: %w", err)
			}
		} else {
			if err := sftpClient.Upload(ctx, sourcePath, targetPath, opts); err != nil {
				return 0, fmt.Errorf("SFTP upload failed: %w", err)
			}
		}

	case targetHasLocal && sourceSSH != nil:
		// Download from remote to local
		sftpClient, err := e.getSFTPClient(sourceSSH)
		if err != nil {
			return 0, fmt.Errorf("failed to create SFTP client: %w", err)
		}
		defer sftpClient.Close()

		// Check if source is a directory or file
		isDir, err := sftpClient.IsDir(sourcePath)
		if err != nil {
			return 0, fmt.Errorf("failed to check source: %w", err)
		}

		if isDir {
			if err := sftpClient.DownloadTree(ctx, sourcePath, targetPath, opts); err != nil {
				return 0, fmt.Errorf("SFTP download tree failed: %w", err)
			}
		} else {
			if err := sftpClient.Download(ctx, sourcePath, targetPath, opts); err != nil {
				return 0, fmt.Errorf("SFTP download failed: %w", err)
			}
		}

	case sourceSSH != nil && targetSSH != nil:
		// Remote to remote - relay through QUI
		srcClient, err := e.getSFTPClient(sourceSSH)
		if err != nil {
			return 0, fmt.Errorf("failed to create source SFTP client: %w", err)
		}
		defer srcClient.Close()

		dstClient, err := e.getSFTPClient(targetSSH)
		if err != nil {
			return 0, fmt.Errorf("failed to create target SFTP client: %w", err)
		}
		defer dstClient.Close()

		relayOpts := sshclient.SFTPRemoteToRemoteOptions{
			SFTPTransferOptions: opts,
			UseRelay:            true,
		}

		if err := sshclient.SFTPRelayTransfer(ctx, srcClient, dstClient, sourcePath, targetPath, relayOpts); err != nil {
			return 0, fmt.Errorf("SFTP relay transfer failed: %w", err)
		}

	default:
		return 0, fmt.Errorf("cannot determine SFTP direction")
	}

	log.Info().
		Int64("id", t.ID).
		Int("files", len(prep.Files)).
		Str("targetDir", targetDir).
		Msg("[TRANSFER-SSH] Files transferred via SFTP")

	return len(prep.Files), nil
}

// getSFTPClient creates an SFTP client from a connection.
func (e *SSHExecutor) getSFTPClient(conn *models.InstanceConnection) (*sshclient.SFTPClient, error) {
	if conn == nil || !conn.Enabled {
		return nil, fmt.Errorf("SSH connection not configured or disabled")
	}

	cfg := &sshclient.Config{
		Host:           conn.Host,
		Port:           conn.Port,
		Username:       conn.Username,
		PrivateKeyPath: conn.PrivateKeyPath,
	}

	return sshclient.NewSFTPClientFromConfig(cfg)
}

// createRemoteLinks creates hardlinks or reflinks on a remote machine via SSH.
func (e *SSHExecutor) createRemoteLinks(ctx context.Context, t *models.Transfer, prep *PrepareResult, targetSSH *models.InstanceConnection) (int, error) {
	client, err := e.getSSHClient(targetSSH)
	if err != nil {
		return 0, err
	}

	// Create target directory
	if err := client.MkdirAll(ctx, prep.TargetSavePath); err != nil {
		return 0, fmt.Errorf("failed to create target directory: %w", err)
	}

	linked := 0
	for _, f := range prep.Files {
		srcPath := f.AbsPath
		dstPath := filepath.Join(prep.TargetSavePath, f.RelPath)

		// Ensure parent directory exists
		dstDir := filepath.Dir(dstPath)
		if err := client.MkdirAll(ctx, dstDir); err != nil {
			return linked, fmt.Errorf("failed to create directory %s: %w", dstDir, err)
		}

		switch prep.LinkMode {
		case "hardlink":
			if err := client.Hardlink(ctx, srcPath, dstPath); err != nil {
				return linked, fmt.Errorf("failed to hardlink %s: %w", f.RelPath, err)
			}
		case "reflink":
			if err := client.Reflink(ctx, srcPath, dstPath); err != nil {
				return linked, fmt.Errorf("failed to reflink %s: %w", f.RelPath, err)
			}
		}
		linked++
	}

	log.Info().
		Int64("id", t.ID).
		Int("files", linked).
		Str("mode", prep.LinkMode).
		Msg("[TRANSFER-SSH] Created remote links")

	return linked, nil
}

// copyRemoteFiles copies files on a remote machine via SSH.
func (e *SSHExecutor) copyRemoteFiles(ctx context.Context, t *models.Transfer, prep *PrepareResult, targetSSH *models.InstanceConnection) (int, error) {
	client, err := e.getSSHClient(targetSSH)
	if err != nil {
		return 0, err
	}

	// Create target directory
	if err := client.MkdirAll(ctx, prep.TargetSavePath); err != nil {
		return 0, fmt.Errorf("failed to create target directory: %w", err)
	}

	copied := 0
	for _, f := range prep.Files {
		srcPath := f.AbsPath
		dstPath := filepath.Join(prep.TargetSavePath, f.RelPath)

		// Ensure parent directory exists
		dstDir := filepath.Dir(dstPath)
		if err := client.MkdirAll(ctx, dstDir); err != nil {
			return copied, fmt.Errorf("failed to create directory %s: %w", dstDir, err)
		}

		if err := client.Copy(ctx, srcPath, dstPath); err != nil {
			return copied, fmt.Errorf("failed to copy %s: %w", f.RelPath, err)
		}
		copied++
	}

	log.Info().
		Int64("id", t.ID).
		Int("files", copied).
		Msg("[TRANSFER-SSH] Copied remote files")

	return copied, nil
}

// ensureTargetDir creates the target directory.
func (e *SSHExecutor) ensureTargetDir(ctx context.Context, dir string, sshConn *models.InstanceConnection, isLocal bool) error {
	if isLocal {
		return os.MkdirAll(dir, 0o755)
	}

	client, err := e.getSSHClient(sshConn)
	if err != nil {
		return err
	}

	return client.MkdirAll(ctx, dir)
}

// sshConfigFromConnection creates an sshclient.Config from a connection model.
func (e *SSHExecutor) sshConfigFromConnection(conn *models.InstanceConnection) *sshclient.Config {
	return &sshclient.Config{
		Host:           conn.Host,
		Port:           conn.Port,
		Username:       conn.Username,
		PrivateKeyPath: conn.PrivateKeyPath,
	}
}

// determineLinkMode decides how files should be handled for SSH transfers.
// Returns: "transfer" if files need to be moved between machines,
// "hardlink"/"reflink"/"copy"/"direct" for same-machine operations.
func (e *SSHExecutor) determineLinkMode(source, target *models.Instance) string {
	sourceLocal := source.HasLocalFilesystemAccess
	targetLocal := target.HasLocalFilesystemAccess

	// If either side lacks local filesystem access, we need to transfer
	if !sourceLocal || !targetLocal {
		return "transfer"
	}

	// Both have local access - use link mode based on target settings
	if target.UseHardlinks {
		return "hardlink"
	}
	if target.UseReflinks {
		return "reflink"
	}
	if target.FallbackToRegularMode {
		return "copy"
	}

	return "direct"
}

// Close shuts down the SSH connection pool.
func (e *SSHExecutor) Close() error {
	return e.sshPool.Close()
}

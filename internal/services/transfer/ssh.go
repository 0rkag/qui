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
	// 1. Get instances
	sourceInstance, err := e.instanceStore.Get(ctx, t.SourceInstanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get source instance: %w", err)
	}

	targetInstance, err := e.instanceStore.Get(ctx, t.TargetInstanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get target instance: %w", err)
	}

	// 2. Get source torrent info (via qBittorrent API, not SSH)
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

	// 5. Build result early so that TorrentName is available even if we fail later
	result := &PrepareResult{
		TorrentName:    sourceTorrent.Name,
		SourceSavePath: props.SavePath,
		SourceInstance: sourceInstance,
		TargetInstance: targetInstance,
	}

	// 6. Build file list with path validation - only include complete files
	result.Files = make([]TorrentFile, 0, len(*files))
	var skippedFiles int
	for _, f := range *files {
		// Skip files that are not fully downloaded
		if f.Progress < 1.0 {
			skippedFiles++
			log.Debug().
				Str("file", f.Name).
				Float64("progress", float64(f.Progress)).
				Msg("[TRANSFER-SSH] Skipping incomplete file")
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
		return result, fmt.Errorf("no complete files to transfer (torrent is %.1f%% complete)", sourceTorrent.Progress*100)
	}

	// Log if we're doing a partial transfer
	if skippedFiles > 0 {
		log.Info().
			Int("completeFiles", len(result.Files)).
			Int("skippedFiles", skippedFiles).
			Float64("torrentProgress", sourceTorrent.Progress*100).
			Msg("[TRANSFER-SSH] Partial transfer - only transferring complete files")
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

	// 9. Compute target save path
	if e.pathResolver != nil {
		resolvedPath, err := e.pathResolver.ResolveTargetPath(
			ctx,
			props.SavePath,
			t.SourceInstanceID,
			t.TargetInstanceID,
			t.PathMappings,
		)
		if err != nil {
			log.Warn().Err(err).Msg("[TRANSFER-SSH] Path resolution failed, using source path")
			result.TargetSavePath = props.SavePath
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

	// 10. Determine link mode for remote transfers
	result.LinkMode = e.determineLinkMode(sourceInstance, targetInstance, props.SavePath, result.TargetSavePath)

	// 11. Export torrent for later use
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
		Msg("[TRANSFER-SSH] Prepared transfer")

	return result, nil
}

// CreateLinks creates files at the target location via SSH/rsync.
func (e *SSHExecutor) CreateLinks(ctx context.Context, t *models.Transfer, prep *PrepareResult) (int, error) {
	// Get SSH connections for source and target
	sourceSSH, _ := e.connectionStore.GetSSHByInstance(ctx, t.SourceInstanceID)
	targetSSH, _ := e.connectionStore.GetSSHByInstance(ctx, t.TargetInstanceID)

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

// resolveTransferMethod determines whether to use rsync or SFTP for a transfer.
func (e *SSHExecutor) resolveTransferMethod(sourceSSH, targetSSH *models.InstanceConnection) string {
	// Check user preference from the connection that will be used for transfer
	conn := targetSSH
	if conn == nil {
		conn = sourceSSH
	}
	if conn == nil {
		return models.TransferMethodSFTP // Fallback to SFTP if no connection
	}

	// Explicit user preference takes priority
	switch conn.TransferMethod {
	case models.TransferMethodRsync:
		return models.TransferMethodRsync
	case models.TransferMethodSFTP:
		return models.TransferMethodSFTP
	}

	// Auto-detect: prefer rsync if available
	if conn.RsyncAvailable != nil && *conn.RsyncAvailable {
		return models.TransferMethodRsync
	}

	// Default to SFTP (always available with SSH)
	return models.TransferMethodSFTP
}

// transferFiles transfers files via rsync or SFTP based on capabilities.
func (e *SSHExecutor) transferFiles(ctx context.Context, t *models.Transfer, prep *PrepareResult, sourceSSH, targetSSH *models.InstanceConnection) (int, error) {
	method := e.resolveTransferMethod(sourceSSH, targetSSH)

	log.Debug().
		Int64("id", t.ID).
		Str("method", method).
		Msg("[TRANSFER-SSH] Using transfer method")

	switch method {
	case models.TransferMethodRsync:
		count, err := e.transferFilesRsync(ctx, t, prep, sourceSSH, targetSSH)
		if err != nil {
			// If rsync fails, try SFTP as fallback
			log.Warn().Err(err).Int64("id", t.ID).Msg("[TRANSFER-SSH] Rsync failed, falling back to SFTP")
			return e.transferFilesSFTP(ctx, t, prep, sourceSSH, targetSSH)
		}
		return count, nil

	case models.TransferMethodSFTP:
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

// computeTargetPath determines where files should be placed on target.
func (e *SSHExecutor) computeTargetPath(sourcePath string, targetInstance *models.Instance, mappings map[string]string) string {
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

// determineLinkMode decides how files should be handled for SSH transfers.
func (e *SSHExecutor) determineLinkMode(source, target *models.Instance, sourcePath, targetPath string) string {
	sourceLocal := source.HasLocalFilesystemAccess
	targetLocal := target.HasLocalFilesystemAccess

	// If both are remote or on different machines, we need to transfer
	if !sourceLocal && !targetLocal {
		return "transfer"
	}

	// If source is local and target is remote, we need to transfer
	if sourceLocal && !targetLocal {
		return "transfer"
	}

	// If source is remote and target is local, we need to transfer
	if !sourceLocal && targetLocal {
		return "transfer"
	}

	// Both local - delegate to local executor logic
	// For now, return hardlink as default
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

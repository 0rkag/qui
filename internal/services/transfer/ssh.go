// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/checksum"
	"github.com/autobrr/qui/pkg/sshclient"
)

// fileExistsActionToMode converts FileExistsAction to sshclient.FileExistsMode.
func fileExistsActionToMode(action models.FileExistsAction) sshclient.FileExistsMode {
	switch action {
	case models.FileExistsSkip:
		return sshclient.FileExistsModeSkip
	case models.FileExistsOverwrite:
		return sshclient.FileExistsModeOverwrite
	default:
		return sshclient.FileExistsModeAbort
	}
}

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
	linkMode := e.determineLinkMode(ctx, common.SourceInstance, common.TargetInstance)

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
	// Get SSH connections for source and target
	// ErrConnectionNotFound is OK (means no SSH configured), but other errors should fail fast
	sourceSSH, err := e.connectionStore.GetSSHByInstance(ctx, t.SourceInstanceID)
	if err != nil && !errors.Is(err, models.ErrConnectionNotFound) {
		return 0, fmt.Errorf("failed to get source SSH connection: %w", err)
	}
	targetSSH, err := e.connectionStore.GetSSHByInstance(ctx, t.TargetInstanceID)
	if err != nil && !errors.Is(err, models.ErrConnectionNotFound) {
		return 0, fmt.Errorf("failed to get target SSH connection: %w", err)
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
// Files are deleted in all modes EXCEPT "direct" (shared storage).
// - hardlink: safe to delete - target hardlinks preserve the data via shared inodes
// - reflink: safe to delete - target has independent CoW copies
// - transfer/copy: safe to delete - files were copied
// - direct: must NOT delete - source and target are the same files
func (e *SSHExecutor) DeleteSource(ctx context.Context, t *models.Transfer) error {
	// Delete source files unless using direct mode (shared storage)
	deleteFiles := t.LinkMode != "direct"
	if err := e.syncManager.DeleteTorrents(ctx, t.SourceInstanceID, []string{t.TorrentHash}, deleteFiles); err != nil {
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

// VerifyTransfer verifies that transferred files match the source using checksums.
func (e *SSHExecutor) VerifyTransfer(ctx context.Context, t *models.Transfer, prep *PrepareResult) error {
	if prep == nil {
		return fmt.Errorf("no preparation result to verify")
	}

	// Direct mode - source and target are the same files, nothing to verify
	if prep.LinkMode == "direct" {
		log.Debug().Int64("id", t.ID).Msg("[TRANSFER-SSH] Direct mode - skipping verification")
		return nil
	}

	log.Info().
		Int64("id", t.ID).
		Str("mode", prep.LinkMode).
		Int("files", len(prep.Files)).
		Msg("[TRANSFER-SSH] Starting transfer verification")

	// Get SSH connections for source and target
	// ErrConnectionNotFound is OK, but other errors should fail fast
	sourceSSH, err := e.connectionStore.GetSSHByInstance(ctx, t.SourceInstanceID)
	if err != nil && !errors.Is(err, models.ErrConnectionNotFound) {
		return fmt.Errorf("failed to get source SSH connection for verification: %w", err)
	}
	targetSSH, err := e.connectionStore.GetSSHByInstance(ctx, t.TargetInstanceID)
	if err != nil && !errors.Is(err, models.ErrConnectionNotFound) {
		return fmt.Errorf("failed to get target SSH connection for verification: %w", err)
	}

	sourceHasLocal := prep.SourceInstance.HasLocalFilesystemAccess
	targetHasLocal := prep.TargetInstance.HasLocalFilesystemAccess

	for _, f := range prep.Files {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		srcPath := f.AbsPath
		dstPath := filepath.Join(prep.TargetSavePath, f.RelPath)

		// For hardlinks on the same host, verify inodes match
		if prep.LinkMode == "hardlink" && sourceSSH != nil && targetSSH != nil &&
			sourceSSH.Host == targetSSH.Host {
			client, err := e.getSSHClient(sourceSSH)
			if err != nil {
				return fmt.Errorf("get SSH client: %w", err)
			}
			if err := e.verifyRemoteHardlink(ctx, client, srcPath, dstPath); err != nil {
				return fmt.Errorf("hardlink verification failed for %s: %w", f.RelPath, err)
			}
			continue
		}

		// For other modes, compare checksums
		srcChecksum, err := e.getFileChecksum(ctx, srcPath, sourceSSH, sourceHasLocal)
		if err != nil {
			return fmt.Errorf("get source checksum for %s: %w", f.RelPath, err)
		}

		dstChecksum, err := e.getFileChecksum(ctx, dstPath, targetSSH, targetHasLocal)
		if err != nil {
			return fmt.Errorf("get target checksum for %s: %w", f.RelPath, err)
		}

		if srcChecksum != dstChecksum {
			return fmt.Errorf("checksum mismatch for %s: source=%s, target=%s", f.RelPath, srcChecksum, dstChecksum)
		}
	}

	log.Info().
		Int64("id", t.ID).
		Int("files", len(prep.Files)).
		Msg("[TRANSFER-SSH] Transfer verification complete")

	return nil
}

// getFileChecksum gets the checksum of a file, either locally or via SSH.
func (e *SSHExecutor) getFileChecksum(ctx context.Context, path string, sshConn *models.InstanceConnection, hasLocal bool) (string, error) {
	if hasLocal {
		return checksum.FileChecksum(path, checksum.AlgorithmSHA256)
	}

	if sshConn == nil {
		return "", fmt.Errorf("no SSH connection and no local access")
	}

	client, err := e.getSSHClient(sshConn)
	if err != nil {
		return "", err
	}

	return client.FileChecksum(ctx, path, checksum.AlgorithmSHA256)
}

// verifyRemoteHardlink verifies that two remote paths share the same inode.
func (e *SSHExecutor) verifyRemoteHardlink(ctx context.Context, client *sshclient.Client, src, dst string) error {
	// Use stat to get inode numbers
	cmd := fmt.Sprintf("stat -c %%i %s %s 2>/dev/null || stat -f %%i %s %s",
		sshclient.ShellQuote(src), sshclient.ShellQuote(dst),
		sshclient.ShellQuote(src), sshclient.ShellQuote(dst))

	result, err := client.Exec(ctx, cmd)
	if err != nil {
		return fmt.Errorf("stat command failed: %w", err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("stat command failed: %s", result.Stderr)
	}

	// Parse the two inode numbers
	lines := strings.Split(strings.TrimSpace(result.Stdout), "\n")
	if len(lines) != 2 {
		return fmt.Errorf("unexpected stat output: %s", result.Stdout)
	}

	if lines[0] != lines[1] {
		return fmt.Errorf("inode mismatch: source=%s, destination=%s", lines[0], lines[1])
	}

	return nil
}

// getSSHClient gets or creates an SSH client from the pool.
func (e *SSHExecutor) getSSHClient(conn *models.InstanceConnection) (*sshclient.Client, error) {
	if conn == nil || !conn.Enabled {
		return nil, fmt.Errorf("SSH connection not configured or disabled")
	}

	cfg := e.sshConfigFromConnection(conn)
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
		count, rsyncErr := e.transferFilesRsync(ctx, t, prep, sourceSSH, targetSSH)
		if rsyncErr != nil {
			// If rsync fails, try SFTP as fallback
			log.Warn().Err(rsyncErr).Int64("id", t.ID).Msg("[TRANSFER-SSH] Rsync failed, falling back to SFTP")
			count, sftpErr := e.transferFilesSFTP(ctx, t, prep, sourceSSH, targetSSH)
			if sftpErr != nil {
				// Both failed - return combined error
				return 0, fmt.Errorf("rsync failed: %v; sftp fallback also failed: %w", rsyncErr, sftpErr)
			}
			return count, nil
		}
		return count, nil

	case transferMethodSFTP:
		return e.transferFilesSFTP(ctx, t, prep, sourceSSH, targetSSH)

	case transferMethodSCP:
		// SCP is not yet implemented - return error so users know to use a different method
		return 0, fmt.Errorf("SCP transfer method is not yet implemented; please use ssh_auto, ssh_rsync, or ssh_sftp instead")

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
	// Note: Do NOT add trailing slash - we want rsync to copy the directory itself,
	// not just its contents. This preserves the torrent folder structure.
	sourcePath := filepath.Join(prep.SourceSavePath, prep.TorrentName)
	if len(prep.Files) == 1 && prep.Files[0].RelPath == prep.TorrentName {
		// Single file torrent - just use the file path
		sourcePath = prep.Files[0].AbsPath
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
		FileExistsMode:      fileExistsActionToMode(t.FileExistsAction),
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

	cfg := e.sshConfigFromConnection(conn)
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
			force := t.FileExistsAction == models.FileExistsOverwrite
			if err := client.Reflink(ctx, srcPath, dstPath, force); err != nil {
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

		force := t.FileExistsAction == models.FileExistsOverwrite
		if err := client.Copy(ctx, srcPath, dstPath, force); err != nil {
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
// Includes the expected host key fingerprint for verification if available.
func (e *SSHExecutor) sshConfigFromConnection(conn *models.InstanceConnection) *sshclient.Config {
	cfg := &sshclient.Config{
		Host:           conn.Host,
		Port:           conn.Port,
		Username:       conn.Username,
		PrivateKeyPath: conn.PrivateKeyPath,
	}

	// Include expected host key for verification if available (TOFU)
	if conn.HostKeyFingerprint != nil && *conn.HostKeyFingerprint != "" {
		alg := ""
		if conn.HostKeyAlgorithm != nil {
			alg = *conn.HostKeyAlgorithm
		}
		cfg.ExpectedHostKey = &sshclient.HostKeyInfo{
			Fingerprint: *conn.HostKeyFingerprint,
			Algorithm:   alg,
		}
	}

	return cfg
}

// determineLinkMode decides how files should be handled for SSH transfers.
// Returns: "transfer" if files need to be moved between machines,
// "hardlink"/"reflink"/"copy"/"direct" for same-machine operations.
func (e *SSHExecutor) determineLinkMode(ctx context.Context, source, target *models.Instance) string {
	sourceLocal := source.HasLocalFilesystemAccess
	targetLocal := target.HasLocalFilesystemAccess

	// If both have local access, use link mode based on target settings
	if sourceLocal && targetLocal {
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

	// Check if both instances have SSH to the same host (same machine scenario)
	if e.connectionStore != nil {
		sourceSSH, err := e.connectionStore.GetSSHByInstance(ctx, source.ID)
		if err != nil && !errors.Is(err, models.ErrConnectionNotFound) {
			log.Warn().Err(err).Int("instanceID", source.ID).Msg("[TRANSFER-SSH] Error getting source SSH for link mode detection")
		}
		targetSSH, err := e.connectionStore.GetSSHByInstance(ctx, target.ID)
		if err != nil && !errors.Is(err, models.ErrConnectionNotFound) {
			log.Warn().Err(err).Int("instanceID", target.ID).Msg("[TRANSFER-SSH] Error getting target SSH for link mode detection")
		}

		if sourceSSH != nil && targetSSH != nil && sourceSSH.Host == targetSSH.Host {
			// Same host via SSH - can use linking based on target settings
			if target.UseHardlinks {
				return "hardlink"
			}
			if target.UseReflinks {
				return "reflink"
			}
			if target.FallbackToRegularMode {
				return "copy"
			}
		}
	}

	// Different machines or no SSH configured - need to transfer
	return "transfer"
}

// Close shuts down the SSH connection pool.
func (e *SSHExecutor) Close() error {
	return e.sshPool.Close()
}

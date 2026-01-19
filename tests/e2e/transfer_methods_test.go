//go:build e2e

package e2e

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Transfer Method Tests - Verify each transfer protocol works correctly
// =============================================================================

// TestTransfer_Method_SFTP tests transfer using explicit SFTP protocol
func TestTransfer_Method_SFTP(t *testing.T) {
	require.NotNil(t, env, "test environment not initialized")

	qbit1 := env.GetQBitInstance("qbit1")
	qbit3 := env.GetQBitInstance("qbit3")
	require.NotNil(t, qbit1)
	require.NotNil(t, qbit3)

	// Update connection types to use SFTP explicitly
	env.UpdateSSHConnectionType(t, qbit1, "ssh_sftp")
	env.UpdateSSHConnectionType(t, qbit3, "ssh_sftp")
	defer func() {
		// Restore to auto after test
		env.UpdateSSHConnectionType(t, qbit1, "ssh_auto")
		env.UpdateSSHConnectionType(t, qbit3, "ssh_auto")
	}()

	// Add torrent to qbit1
	torrentPath := env.FixturePath("sintel.torrent")
	hash := env.AddTorrent(t, qbit1, torrentPath, "/downloads")
	env.WaitForDownload(t, qbit1, hash, 2*time.Minute)

	// Transfer to qbit3 using SFTP
	transfer := env.StartTransfer(t, qbit1.ID, qbit3.ID, hash, false)
	require.NotNil(t, transfer)

	result := env.WaitForTransfer(t, transfer.ID, 5*time.Minute)
	require.Equal(t, "completed", result.State, "SFTP transfer should complete, got error: %s", result.Error)

	// Verify transfer mode
	assert.Equal(t, "transfer", result.LinkMode, "SFTP should use transfer link mode")

	env.AssertTorrentExists(t, qbit3, hash)
	env.AssertTorrentExists(t, qbit1, hash)

	// Cleanup
	env.DeleteTorrent(t, qbit1, hash)
	env.DeleteTorrent(t, qbit3, hash)
}

// TestTransfer_Method_SCP tests transfer using explicit SCP protocol
func TestTransfer_Method_SCP(t *testing.T) {
	require.NotNil(t, env, "test environment not initialized")

	qbit1 := env.GetQBitInstance("qbit1")
	qbit3 := env.GetQBitInstance("qbit3")
	require.NotNil(t, qbit1)
	require.NotNil(t, qbit3)

	// Update connection types to use SCP explicitly
	env.UpdateSSHConnectionType(t, qbit1, "ssh_scp")
	env.UpdateSSHConnectionType(t, qbit3, "ssh_scp")
	defer func() {
		// Restore to auto after test
		env.UpdateSSHConnectionType(t, qbit1, "ssh_auto")
		env.UpdateSSHConnectionType(t, qbit3, "ssh_auto")
	}()

	// Add torrent to qbit1
	torrentPath := env.FixturePath("sintel.torrent")
	hash := env.AddTorrent(t, qbit1, torrentPath, "/downloads")
	env.WaitForDownload(t, qbit1, hash, 2*time.Minute)

	// Transfer to qbit3 using SCP
	transfer := env.StartTransfer(t, qbit1.ID, qbit3.ID, hash, false)
	require.NotNil(t, transfer)

	result := env.WaitForTransfer(t, transfer.ID, 5*time.Minute)
	require.Equal(t, "completed", result.State, "SCP transfer should complete, got error: %s", result.Error)

	// Verify transfer mode
	assert.Equal(t, "transfer", result.LinkMode, "SCP should use transfer link mode")

	env.AssertTorrentExists(t, qbit3, hash)
	env.AssertTorrentExists(t, qbit1, hash)

	// Cleanup
	env.DeleteTorrent(t, qbit1, hash)
	env.DeleteTorrent(t, qbit3, hash)
}

// TestTransfer_Method_Rsync_Explicit tests transfer using explicit rsync protocol
func TestTransfer_Method_Rsync_Explicit(t *testing.T) {
	require.NotNil(t, env, "test environment not initialized")

	qbit1 := env.GetQBitInstance("qbit1")
	qbit3 := env.GetQBitInstance("qbit3")
	require.NotNil(t, qbit1)
	require.NotNil(t, qbit3)

	// Update connection types to use rsync explicitly
	env.UpdateSSHConnectionType(t, qbit1, "ssh_rsync")
	env.UpdateSSHConnectionType(t, qbit3, "ssh_rsync")
	defer func() {
		// Restore to auto after test
		env.UpdateSSHConnectionType(t, qbit1, "ssh_auto")
		env.UpdateSSHConnectionType(t, qbit3, "ssh_auto")
	}()

	// Add torrent to qbit1
	torrentPath := env.FixturePath("wired-cd.torrent")
	hash := env.AddTorrent(t, qbit1, torrentPath, "/downloads")
	env.WaitForDownload(t, qbit1, hash, 2*time.Minute)

	// Transfer to qbit3 using rsync
	transfer := env.StartTransfer(t, qbit1.ID, qbit3.ID, hash, false)
	require.NotNil(t, transfer)

	result := env.WaitForTransfer(t, transfer.ID, 5*time.Minute)
	require.Equal(t, "completed", result.State, "rsync transfer should complete, got error: %s", result.Error)

	// Verify transfer mode
	assert.Equal(t, "transfer", result.LinkMode, "rsync should use transfer link mode")

	env.AssertTorrentExists(t, qbit3, hash)
	env.AssertTorrentExists(t, qbit1, hash)

	// Cleanup
	env.DeleteTorrent(t, qbit1, hash)
	env.DeleteTorrent(t, qbit3, hash)
}

// =============================================================================
// File Exists Action Tests
// =============================================================================

// TestTransfer_FileExistsAction_Skip tests that skip mode works correctly
func TestTransfer_FileExistsAction_Skip(t *testing.T) {
	require.NotNil(t, env, "test environment not initialized")

	qbit1 := env.GetQBitInstance("qbit1")
	qbit3 := env.GetQBitInstance("qbit3")
	require.NotNil(t, qbit1)
	require.NotNil(t, qbit3)

	// Add torrent to qbit1
	torrentPath := env.FixturePath("sintel.torrent")
	hash := env.AddTorrent(t, qbit1, torrentPath, "/downloads")
	env.WaitForDownload(t, qbit1, hash, 2*time.Minute)

	// First transfer
	transfer1 := env.StartTransferFull(t, TransferRequest{
		SourceInstanceID: qbit1.ID,
		TargetInstanceID: qbit3.ID,
		TorrentHash:      hash,
		FileExistsAction: "abort",
		SourceAction:     "keep",
	})
	result1 := env.WaitForTransfer(t, transfer1.ID, 5*time.Minute)
	require.Equal(t, "completed", result1.State, "first transfer should complete")

	// Delete the torrent from target but keep files
	env.DeleteTorrent(t, qbit3, hash)

	// Second transfer with skip mode - should succeed because files exist
	transfer2 := env.StartTransferFull(t, TransferRequest{
		SourceInstanceID: qbit1.ID,
		TargetInstanceID: qbit3.ID,
		TorrentHash:      hash,
		FileExistsAction: "skip",
		SourceAction:     "keep",
	})
	result2 := env.WaitForTransfer(t, transfer2.ID, 5*time.Minute)
	require.Equal(t, "completed", result2.State, "skip mode transfer should complete, got error: %s", result2.Error)

	env.AssertTorrentExists(t, qbit3, hash)

	// Cleanup
	env.DeleteTorrent(t, qbit1, hash)
	env.DeleteTorrent(t, qbit3, hash)
}

// TestTransfer_FileExistsAction_Overwrite tests that overwrite mode works
func TestTransfer_FileExistsAction_Overwrite(t *testing.T) {
	require.NotNil(t, env, "test environment not initialized")

	qbit1 := env.GetQBitInstance("qbit1")
	qbit3 := env.GetQBitInstance("qbit3")
	require.NotNil(t, qbit1)
	require.NotNil(t, qbit3)

	// Add torrent to qbit1
	torrentPath := env.FixturePath("sintel.torrent")
	hash := env.AddTorrent(t, qbit1, torrentPath, "/downloads")
	env.WaitForDownload(t, qbit1, hash, 2*time.Minute)

	// First transfer
	transfer1 := env.StartTransferFull(t, TransferRequest{
		SourceInstanceID: qbit1.ID,
		TargetInstanceID: qbit3.ID,
		TorrentHash:      hash,
		FileExistsAction: "abort",
		SourceAction:     "keep",
	})
	result1 := env.WaitForTransfer(t, transfer1.ID, 5*time.Minute)
	require.Equal(t, "completed", result1.State, "first transfer should complete")

	// Delete the torrent from target but keep files
	env.DeleteTorrent(t, qbit3, hash)

	// Second transfer with overwrite mode
	transfer2 := env.StartTransferFull(t, TransferRequest{
		SourceInstanceID: qbit1.ID,
		TargetInstanceID: qbit3.ID,
		TorrentHash:      hash,
		FileExistsAction: "overwrite",
		SourceAction:     "keep",
	})
	result2 := env.WaitForTransfer(t, transfer2.ID, 5*time.Minute)
	require.Equal(t, "completed", result2.State, "overwrite mode transfer should complete, got error: %s", result2.Error)

	env.AssertTorrentExists(t, qbit3, hash)

	// Cleanup
	env.DeleteTorrent(t, qbit1, hash)
	env.DeleteTorrent(t, qbit3, hash)
}

// =============================================================================
// SCP Specific Tests
// =============================================================================

// TestTransfer_SCP_WithDeleteSource tests SCP transfer with source deletion
func TestTransfer_SCP_WithDeleteSource(t *testing.T) {
	require.NotNil(t, env, "test environment not initialized")

	qbit1 := env.GetQBitInstance("qbit1")
	qbit3 := env.GetQBitInstance("qbit3")
	require.NotNil(t, qbit1)
	require.NotNil(t, qbit3)

	// Update connection types to use SCP explicitly
	env.UpdateSSHConnectionType(t, qbit1, "ssh_scp")
	env.UpdateSSHConnectionType(t, qbit3, "ssh_scp")
	defer func() {
		env.UpdateSSHConnectionType(t, qbit1, "ssh_auto")
		env.UpdateSSHConnectionType(t, qbit3, "ssh_auto")
	}()

	// Add torrent to qbit1
	torrentPath := env.FixturePath("wired-cd.torrent")
	hash := env.AddTorrent(t, qbit1, torrentPath, "/downloads")
	env.WaitForDownload(t, qbit1, hash, 2*time.Minute)

	// Transfer with delete source
	transfer := env.StartTransfer(t, qbit1.ID, qbit3.ID, hash, true)
	require.NotNil(t, transfer)
	assert.Equal(t, "delete", transfer.SourceAction)

	result := env.WaitForTransfer(t, transfer.ID, 5*time.Minute)
	require.Equal(t, "completed", result.State, "SCP transfer with delete should complete, got error: %s", result.Error)

	// Verify source is deleted and target exists
	env.AssertTorrentNotExists(t, qbit1, hash)
	env.AssertTorrentExists(t, qbit3, hash)

	// Cleanup
	env.DeleteTorrent(t, qbit3, hash)
}

// TestTransfer_SCP_PreserveMetadata tests SCP transfer with category and tags
func TestTransfer_SCP_PreserveMetadata(t *testing.T) {
	require.NotNil(t, env, "test environment not initialized")

	qbit1 := env.GetQBitInstance("qbit1")
	qbit3 := env.GetQBitInstance("qbit3")
	require.NotNil(t, qbit1)
	require.NotNil(t, qbit3)

	// Update connection types to use SCP explicitly
	env.UpdateSSHConnectionType(t, qbit1, "ssh_scp")
	env.UpdateSSHConnectionType(t, qbit3, "ssh_scp")
	defer func() {
		env.UpdateSSHConnectionType(t, qbit1, "ssh_auto")
		env.UpdateSSHConnectionType(t, qbit3, "ssh_auto")
	}()

	// Add torrent to qbit1
	torrentPath := env.FixturePath("sintel.torrent")
	hash := env.AddTorrent(t, qbit1, torrentPath, "/downloads")
	env.WaitForDownload(t, qbit1, hash, 2*time.Minute)

	// Set category and tags
	env.SetTorrentCategory(t, qbit1, hash, "movies")
	env.SetTorrentTags(t, qbit1, hash, []string{"hd", "creative-commons"})

	// Transfer with preserve metadata
	transfer := env.StartTransferFull(t, TransferRequest{
		SourceInstanceID: qbit1.ID,
		TargetInstanceID: qbit3.ID,
		TorrentHash:      hash,
		SourceAction:     "keep",
		PreserveCategory: true,
		PreserveTags:     true,
	})
	require.NotNil(t, transfer)

	result := env.WaitForTransfer(t, transfer.ID, 5*time.Minute)
	require.Equal(t, "completed", result.State, "SCP transfer should complete, got error: %s", result.Error)

	// Verify metadata is preserved
	env.AssertTorrentCategory(t, qbit3, hash, "movies")
	env.AssertTorrentTags(t, qbit3, hash, []string{"hd", "creative-commons"})

	// Cleanup
	env.DeleteTorrent(t, qbit1, hash)
	env.DeleteTorrent(t, qbit3, hash)
}

// TestTransfer_SCP_WithVerification tests SCP transfer with post-transfer verification
func TestTransfer_SCP_WithVerification(t *testing.T) {
	require.NotNil(t, env, "test environment not initialized")

	qbit1 := env.GetQBitInstance("qbit1")
	qbit3 := env.GetQBitInstance("qbit3")
	require.NotNil(t, qbit1)
	require.NotNil(t, qbit3)

	// Update connection types to use SCP explicitly
	env.UpdateSSHConnectionType(t, qbit1, "ssh_scp")
	env.UpdateSSHConnectionType(t, qbit3, "ssh_scp")
	defer func() {
		env.UpdateSSHConnectionType(t, qbit1, "ssh_auto")
		env.UpdateSSHConnectionType(t, qbit3, "ssh_auto")
	}()

	// Add torrent to qbit1
	torrentPath := env.FixturePath("sintel.torrent")
	hash := env.AddTorrent(t, qbit1, torrentPath, "/downloads")
	env.WaitForDownload(t, qbit1, hash, 2*time.Minute)

	// Transfer with verification enabled
	transfer := env.StartTransferFull(t, TransferRequest{
		SourceInstanceID: qbit1.ID,
		TargetInstanceID: qbit3.ID,
		TorrentHash:      hash,
		SourceAction:     "keep",
		VerifyTransfer:   true,
	})
	require.NotNil(t, transfer)

	result := env.WaitForTransfer(t, transfer.ID, 5*time.Minute)
	require.Equal(t, "completed", result.State, "SCP transfer with verification should complete, got error: %s", result.Error)

	env.AssertTorrentExists(t, qbit3, hash)

	// Cleanup
	env.DeleteTorrent(t, qbit1, hash)
	env.DeleteTorrent(t, qbit3, hash)
}

// =============================================================================
// SFTP Specific Tests
// =============================================================================

// TestTransfer_SFTP_WithDeleteSource tests SFTP transfer with source deletion
func TestTransfer_SFTP_WithDeleteSource(t *testing.T) {
	require.NotNil(t, env, "test environment not initialized")

	qbit1 := env.GetQBitInstance("qbit1")
	qbit3 := env.GetQBitInstance("qbit3")
	require.NotNil(t, qbit1)
	require.NotNil(t, qbit3)

	// Update connection types to use SFTP explicitly
	env.UpdateSSHConnectionType(t, qbit1, "ssh_sftp")
	env.UpdateSSHConnectionType(t, qbit3, "ssh_sftp")
	defer func() {
		env.UpdateSSHConnectionType(t, qbit1, "ssh_auto")
		env.UpdateSSHConnectionType(t, qbit3, "ssh_auto")
	}()

	// Add torrent to qbit1
	torrentPath := env.FixturePath("wired-cd.torrent")
	hash := env.AddTorrent(t, qbit1, torrentPath, "/downloads")
	env.WaitForDownload(t, qbit1, hash, 2*time.Minute)

	// Transfer with delete source
	transfer := env.StartTransfer(t, qbit1.ID, qbit3.ID, hash, true)
	require.NotNil(t, transfer)

	result := env.WaitForTransfer(t, transfer.ID, 5*time.Minute)
	require.Equal(t, "completed", result.State, "SFTP transfer with delete should complete, got error: %s", result.Error)

	// Verify source is deleted and target exists
	env.AssertTorrentNotExists(t, qbit1, hash)
	env.AssertTorrentExists(t, qbit3, hash)

	// Cleanup
	env.DeleteTorrent(t, qbit3, hash)
}

// TestTransfer_SFTP_ChainedTransfer tests chained transfer using SFTP (qbit1 -> qbit2 -> qbit3)
func TestTransfer_SFTP_ChainedTransfer(t *testing.T) {
	require.NotNil(t, env, "test environment not initialized")

	qbit1 := env.GetQBitInstance("qbit1")
	qbit2 := env.GetQBitInstance("qbit2")
	qbit3 := env.GetQBitInstance("qbit3")
	require.NotNil(t, qbit1)
	require.NotNil(t, qbit2)
	require.NotNil(t, qbit3)

	// Update connection types to use SFTP explicitly
	env.UpdateSSHConnectionType(t, qbit1, "ssh_sftp")
	env.UpdateSSHConnectionType(t, qbit2, "ssh_sftp")
	env.UpdateSSHConnectionType(t, qbit3, "ssh_sftp")
	defer func() {
		env.UpdateSSHConnectionType(t, qbit1, "ssh_auto")
		env.UpdateSSHConnectionType(t, qbit2, "ssh_auto")
		env.UpdateSSHConnectionType(t, qbit3, "ssh_auto")
	}()

	// Add torrent to qbit1 at /downloads (each instance has its own volume)
	torrentPath := env.FixturePath("sintel.torrent")
	hash := env.AddTorrent(t, qbit1, torrentPath, "/downloads")
	env.WaitForDownload(t, qbit1, hash, 2*time.Minute)

	// First transfer: qbit1 -> qbit2
	transfer1 := env.StartTransfer(t, qbit1.ID, qbit2.ID, hash, false)
	result1 := env.WaitForTransfer(t, transfer1.ID, 5*time.Minute)
	require.Equal(t, "completed", result1.State, "first transfer should complete, got error: %s", result1.Error)

	env.WaitForDownload(t, qbit2, hash, 2*time.Minute)

	// Second transfer: qbit2 -> qbit3
	transfer2 := env.StartTransfer(t, qbit2.ID, qbit3.ID, hash, false)
	result2 := env.WaitForTransfer(t, transfer2.ID, 5*time.Minute)
	require.Equal(t, "completed", result2.State, "second transfer should complete, got error: %s", result2.Error)

	// Verify all instances have the torrent
	env.AssertTorrentExists(t, qbit1, hash)
	env.AssertTorrentExists(t, qbit2, hash)
	env.AssertTorrentExists(t, qbit3, hash)

	// Cleanup
	env.DeleteTorrent(t, qbit1, hash)
	env.DeleteTorrent(t, qbit2, hash)
	env.DeleteTorrent(t, qbit3, hash)
}

// =============================================================================
// Multi-File Torrent Tests
// =============================================================================

// TestTransfer_SCP_MultiFileTorrent tests SCP transfer with multi-file torrents
func TestTransfer_SCP_MultiFileTorrent(t *testing.T) {
	require.NotNil(t, env, "test environment not initialized")

	qbit1 := env.GetQBitInstance("qbit1")
	qbit3 := env.GetQBitInstance("qbit3")
	require.NotNil(t, qbit1)
	require.NotNil(t, qbit3)

	// Update connection types to use SCP explicitly
	env.UpdateSSHConnectionType(t, qbit1, "ssh_scp")
	env.UpdateSSHConnectionType(t, qbit3, "ssh_scp")
	defer func() {
		env.UpdateSSHConnectionType(t, qbit1, "ssh_auto")
		env.UpdateSSHConnectionType(t, qbit3, "ssh_auto")
	}()

	// Use wired-cd which is a multi-file torrent (multiple files in a directory)
	torrentPath := env.FixturePath("wired-cd.torrent")
	hash := env.AddTorrent(t, qbit1, torrentPath, "/downloads")
	env.WaitForDownload(t, qbit1, hash, 2*time.Minute)

	// Transfer using SCP
	transfer := env.StartTransfer(t, qbit1.ID, qbit3.ID, hash, false)
	require.NotNil(t, transfer)

	result := env.WaitForTransfer(t, transfer.ID, 5*time.Minute)
	require.Equal(t, "completed", result.State, "SCP multi-file transfer should complete, got error: %s", result.Error)

	env.AssertTorrentExists(t, qbit3, hash)

	// Cleanup
	env.DeleteTorrent(t, qbit1, hash)
	env.DeleteTorrent(t, qbit3, hash)
}

// TestTransfer_SFTP_MultiFileTorrent tests SFTP transfer with multi-file torrents
func TestTransfer_SFTP_MultiFileTorrent(t *testing.T) {
	require.NotNil(t, env, "test environment not initialized")

	qbit1 := env.GetQBitInstance("qbit1")
	qbit3 := env.GetQBitInstance("qbit3")
	require.NotNil(t, qbit1)
	require.NotNil(t, qbit3)

	// Update connection types to use SFTP explicitly
	env.UpdateSSHConnectionType(t, qbit1, "ssh_sftp")
	env.UpdateSSHConnectionType(t, qbit3, "ssh_sftp")
	defer func() {
		env.UpdateSSHConnectionType(t, qbit1, "ssh_auto")
		env.UpdateSSHConnectionType(t, qbit3, "ssh_auto")
	}()

	// Use wired-cd which is a multi-file torrent
	torrentPath := env.FixturePath("wired-cd.torrent")
	hash := env.AddTorrent(t, qbit1, torrentPath, "/downloads")
	env.WaitForDownload(t, qbit1, hash, 2*time.Minute)

	// Transfer using SFTP
	transfer := env.StartTransfer(t, qbit1.ID, qbit3.ID, hash, false)
	require.NotNil(t, transfer)

	result := env.WaitForTransfer(t, transfer.ID, 5*time.Minute)
	require.Equal(t, "completed", result.State, "SFTP multi-file transfer should complete, got error: %s", result.Error)

	env.AssertTorrentExists(t, qbit3, hash)

	// Cleanup
	env.DeleteTorrent(t, qbit1, hash)
	env.DeleteTorrent(t, qbit3, hash)
}

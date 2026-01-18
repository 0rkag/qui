//go:build e2e

package e2e

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTransfer_Rsync tests transfer between instances with different volumes (requires rsync)
func TestTransfer_Rsync(t *testing.T) {
	require.NotNil(t, env, "test environment not initialized")

	qbit1 := env.GetQBitInstance("qbit1")
	qbit3 := env.GetQBitInstance("qbit3")
	require.NotNil(t, qbit1)
	require.NotNil(t, qbit3)

	// Add torrent to qbit1 at /downloads (not shared with qbit3)
	torrentPath := env.FixturePath("sintel.torrent")
	hash := env.AddTorrent(t, qbit1, torrentPath, "/downloads")

	// Wait for download to complete (metadata should be enough for small file)
	env.WaitForDownload(t, qbit1, hash, 2*time.Minute)

	// Transfer to qbit3 (different volume = rsync required)
	transfer := env.StartTransfer(t, qbit1.ID, qbit3.ID, hash, false)
	require.NotNil(t, transfer)

	// Wait for transfer to complete
	result := env.WaitForTransfer(t, transfer.ID, 5*time.Minute)
	require.Equal(t, "completed", result.State, "transfer should complete, got error: %s", result.Error)

	// Assert torrent exists on qbit3
	env.AssertTorrentExists(t, qbit3, hash)

	// Assert torrent still exists on qbit1 (deleteFromSource=false)
	env.AssertTorrentExists(t, qbit1, hash)

	// Cleanup
	env.DeleteTorrent(t, qbit1, hash)
	env.DeleteTorrent(t, qbit3, hash)
}

// TestTransfer_SharedVolume tests transfer between instances with shared storage (hardlinks)
func TestTransfer_SharedVolume(t *testing.T) {
	require.NotNil(t, env, "test environment not initialized")

	qbit1 := env.GetQBitInstance("qbit1")
	qbit2 := env.GetQBitInstance("qbit2")
	require.NotNil(t, qbit1)
	require.NotNil(t, qbit2)

	// Use wired-cd torrent for shared volume test (different from rsync test)
	torrentPath := env.FixturePath("wired-cd.torrent")
	hash := env.AddTorrent(t, qbit1, torrentPath, "/shared")

	// Wait for download to complete
	env.WaitForDownload(t, qbit1, hash, 2*time.Minute)

	// Transfer to qbit2 (same volume = can use hardlinks)
	transfer := env.StartTransfer(t, qbit1.ID, qbit2.ID, hash, false)
	require.NotNil(t, transfer)

	// Wait for transfer to complete
	result := env.WaitForTransfer(t, transfer.ID, 5*time.Minute)
	require.Equal(t, "completed", result.State, "transfer should complete, got error: %s", result.Error)

	// Assert torrent exists on qbit2
	env.AssertTorrentExists(t, qbit2, hash)

	// Assert torrent still exists on qbit1 (deleteFromSource=false)
	env.AssertTorrentExists(t, qbit1, hash)

	// Cleanup
	env.DeleteTorrent(t, qbit1, hash)
	env.DeleteTorrent(t, qbit2, hash)
}

// TestTransfer_DeleteSource tests that source torrent is deleted after transfer
func TestTransfer_DeleteSource(t *testing.T) {
	require.NotNil(t, env, "test environment not initialized")

	qbit1 := env.GetQBitInstance("qbit1")
	qbit2 := env.GetQBitInstance("qbit2")
	require.NotNil(t, qbit1)
	require.NotNil(t, qbit2)

	// Use sintel torrent at /downloads (same path as rsync test for file reuse)
	torrentPath := env.FixturePath("sintel.torrent")
	hash := env.AddTorrent(t, qbit1, torrentPath, "/downloads")

	// Wait for download to complete
	env.WaitForDownload(t, qbit1, hash, 2*time.Minute)

	// Transfer to qbit2 with sourceAction=delete
	transfer := env.StartTransfer(t, qbit1.ID, qbit2.ID, hash, true)
	require.NotNil(t, transfer)
	assert.Equal(t, "delete", transfer.SourceAction)

	// Wait for transfer to complete
	result := env.WaitForTransfer(t, transfer.ID, 5*time.Minute)
	require.Equal(t, "completed", result.State, "transfer should complete, got error: %s", result.Error)

	// Assert torrent exists on qbit2
	env.AssertTorrentExists(t, qbit2, hash)

	// Assert torrent was removed from qbit1
	env.AssertTorrentNotExists(t, qbit1, hash)

	// Cleanup
	env.DeleteTorrent(t, qbit2, hash)
}

// TestTransfer_KeepSource tests that source torrent is kept when sourceAction=keep
func TestTransfer_KeepSource(t *testing.T) {
	require.NotNil(t, env, "test environment not initialized")

	qbit1 := env.GetQBitInstance("qbit1")
	qbit2 := env.GetQBitInstance("qbit2")
	require.NotNil(t, qbit1)
	require.NotNil(t, qbit2)

	// Add torrent
	torrentPath := env.FixturePath("sintel.torrent")
	hash := env.AddTorrent(t, qbit1, torrentPath, "/downloads")

	// Wait for download
	env.WaitForDownload(t, qbit1, hash, 2*time.Minute)

	// Transfer with sourceAction=keep
	transfer := env.StartTransfer(t, qbit1.ID, qbit2.ID, hash, false)
	require.NotNil(t, transfer)
	assert.Equal(t, "keep", transfer.SourceAction)

	// Wait for completion
	result := env.WaitForTransfer(t, transfer.ID, 5*time.Minute)
	require.Equal(t, "completed", result.State, "transfer should complete, got error: %s", result.Error)

	// Assert torrent exists on both instances
	env.AssertTorrentExists(t, qbit1, hash)
	env.AssertTorrentExists(t, qbit2, hash)

	// Cleanup
	env.DeleteTorrent(t, qbit1, hash)
	env.DeleteTorrent(t, qbit2, hash)
}

// TestTransfer_ToThirdInstance tests chained transfer (qbit1 -> qbit2 -> qbit3)
func TestTransfer_ToThirdInstance(t *testing.T) {
	require.NotNil(t, env, "test environment not initialized")

	qbit1 := env.GetQBitInstance("qbit1")
	qbit2 := env.GetQBitInstance("qbit2")
	qbit3 := env.GetQBitInstance("qbit3")
	require.NotNil(t, qbit1)
	require.NotNil(t, qbit2)
	require.NotNil(t, qbit3)

	// Use wired-cd at /downloads (each instance has its own isolated /downloads volume)
	// This avoids the shared volume issue where qbit1 and qbit2 both mount /shared
	torrentPath := env.FixturePath("wired-cd.torrent")
	hash := env.AddTorrent(t, qbit1, torrentPath, "/downloads")

	// Wait for download
	env.WaitForDownload(t, qbit1, hash, 2*time.Minute)

	// Transfer from qbit1 to qbit2 (different /downloads volumes = rsync)
	transfer1 := env.StartTransfer(t, qbit1.ID, qbit2.ID, hash, false)
	result1 := env.WaitForTransfer(t, transfer1.ID, 5*time.Minute)
	require.Equal(t, "completed", result1.State, "first transfer should complete, got error: %s", result1.Error)

	// Wait for qbit2 to finish verifying the transferred files
	env.WaitForDownload(t, qbit2, hash, 2*time.Minute)

	// Now transfer from qbit2 to qbit3 (also different /downloads volumes = rsync)
	transfer2 := env.StartTransfer(t, qbit2.ID, qbit3.ID, hash, false)
	result2 := env.WaitForTransfer(t, transfer2.ID, 5*time.Minute)
	require.Equal(t, "completed", result2.State, "second transfer should complete, got error: %s", result2.Error)

	// Assert torrent exists on all instances
	env.AssertTorrentExists(t, qbit1, hash)
	env.AssertTorrentExists(t, qbit2, hash)
	env.AssertTorrentExists(t, qbit3, hash)

	// Cleanup
	env.DeleteTorrent(t, qbit1, hash)
	env.DeleteTorrent(t, qbit2, hash)
	env.DeleteTorrent(t, qbit3, hash)
}

// TestTransfer_WithVerification tests transfer with post-transfer checksum verification
func TestTransfer_WithVerification(t *testing.T) {
	require.NotNil(t, env, "test environment not initialized")

	qbit1 := env.GetQBitInstance("qbit1")
	qbit3 := env.GetQBitInstance("qbit3")
	require.NotNil(t, qbit1)
	require.NotNil(t, qbit3)

	// Add torrent to qbit1 at /downloads
	torrentPath := env.FixturePath("sintel.torrent")
	hash := env.AddTorrent(t, qbit1, torrentPath, "/downloads")

	// Wait for download to complete
	env.WaitForDownload(t, qbit1, hash, 2*time.Minute)

	// Transfer to qbit3 with verification enabled
	transfer := env.StartTransferFull(t, TransferRequest{
		SourceInstanceID: qbit1.ID,
		TargetInstanceID: qbit3.ID,
		TorrentHash:      hash,
		SourceAction:     "keep",
		VerifyTransfer:   true,
	})
	require.NotNil(t, transfer)

	// Wait for transfer to complete
	result := env.WaitForTransfer(t, transfer.ID, 5*time.Minute)
	require.Equal(t, "completed", result.State, "transfer with verification should complete, got error: %s", result.Error)

	// Assert torrent exists on both instances
	env.AssertTorrentExists(t, qbit1, hash)
	env.AssertTorrentExists(t, qbit3, hash)

	// Cleanup
	env.DeleteTorrent(t, qbit1, hash)
	env.DeleteTorrent(t, qbit3, hash)
}

// TestTransfer_SharedVolume_UsesHardlinks verifies that transfers between instances
// sharing the same volume use hardlinks instead of copying
func TestTransfer_SharedVolume_UsesHardlinks(t *testing.T) {
	require.NotNil(t, env, "test environment not initialized")

	qbit1 := env.GetQBitInstance("qbit1")
	qbit2 := env.GetQBitInstance("qbit2")
	require.NotNil(t, qbit1)
	require.NotNil(t, qbit2)

	// Use sintel torrent at /shared (qbit1 and qbit2 share this volume)
	torrentPath := env.FixturePath("sintel.torrent")
	hash := env.AddTorrent(t, qbit1, torrentPath, "/shared")

	// Wait for download to complete
	env.WaitForDownload(t, qbit1, hash, 2*time.Minute)

	// Transfer to qbit2 (same volume = should use hardlinks)
	transfer := env.StartTransfer(t, qbit1.ID, qbit2.ID, hash, false)
	require.NotNil(t, transfer)

	// Wait for transfer to complete
	result := env.WaitForTransfer(t, transfer.ID, 5*time.Minute)
	require.Equal(t, "completed", result.State, "transfer should complete, got error: %s", result.Error)

	// Verify the transfer used hardlinks (linkMode should be "hardlink")
	assert.Equal(t, "hardlink", result.LinkMode, "transfer between shared volumes should use hardlinks")

	// Assert torrent exists on both instances
	env.AssertTorrentExists(t, qbit1, hash)
	env.AssertTorrentExists(t, qbit2, hash)

	// Cleanup
	env.DeleteTorrent(t, qbit1, hash)
	env.DeleteTorrent(t, qbit2, hash)
}

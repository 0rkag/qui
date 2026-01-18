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

// TestTransfer_DeleteFromSource tests that source torrent is deleted after transfer
func TestTransfer_DeleteFromSource(t *testing.T) {
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

	// Transfer to qbit2 with deleteFromSource=true
	transfer := env.StartTransfer(t, qbit1.ID, qbit2.ID, hash, true)
	require.NotNil(t, transfer)
	assert.True(t, transfer.DeleteFromSource)

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

// TestTransfer_KeepSource tests that source torrent is kept when deleteFromSource=false
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

	// Transfer with deleteFromSource=false
	transfer := env.StartTransfer(t, qbit1.ID, qbit2.ID, hash, false)
	require.NotNil(t, transfer)
	assert.False(t, transfer.DeleteFromSource)

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

// TestTransfer_ToThirdInstance tests chained transfer (qbit2 -> qbit3)
func TestTransfer_ToThirdInstance(t *testing.T) {
	require.NotNil(t, env, "test environment not initialized")

	qbit1 := env.GetQBitInstance("qbit1")
	qbit2 := env.GetQBitInstance("qbit2")
	qbit3 := env.GetQBitInstance("qbit3")
	require.NotNil(t, qbit1)
	require.NotNil(t, qbit2)
	require.NotNil(t, qbit3)

	// Use wired-cd at /shared (same as SharedVolume test for file reuse)
	torrentPath := env.FixturePath("wired-cd.torrent")
	hash := env.AddTorrent(t, qbit1, torrentPath, "/shared")

	// Wait for download
	env.WaitForDownload(t, qbit1, hash, 2*time.Minute)

	// Transfer from qbit1 to qbit2 (shared volume)
	transfer1 := env.StartTransfer(t, qbit1.ID, qbit2.ID, hash, false)
	result1 := env.WaitForTransfer(t, transfer1.ID, 5*time.Minute)
	require.Equal(t, "completed", result1.State, "first transfer should complete")

	// Now transfer from qbit2 to qbit3 (no shared volume = rsync)
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

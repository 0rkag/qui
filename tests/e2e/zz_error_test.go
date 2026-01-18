//go:build e2e

// Package e2e contains end-to-end tests for the transfer service.
// This file contains error handling tests that may cause IP bans due to container
// stops/restarts. They are placed in a separate file prefixed with "zz_" to ensure
// they run last (after transfer_test.go and transfer_options_test.go).
package e2e

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTransfer_ZZ_Cancellation tests cancelling a pending transfer
func TestTransfer_ZZ_Cancellation(t *testing.T) {
	require.NotNil(t, env, "test environment not initialized")

	qbit1 := env.GetQBitInstance("qbit1")
	qbit2 := env.GetQBitInstance("qbit2")
	require.NotNil(t, qbit1)
	require.NotNil(t, qbit2)

	// Add torrent
	torrentPath := env.FixturePath("sintel.torrent")
	hash := env.AddTorrent(t, qbit1, torrentPath, "/shared")
	env.WaitForDownload(t, qbit1, hash, 2*time.Minute)

	// Start a transfer
	transfer := env.StartTransferFull(t, TransferRequest{
		SourceInstanceID: qbit1.ID,
		TargetInstanceID: qbit2.ID,
		TorrentHash:      hash,
		DeleteFromSource: false,
		PreserveCategory: false,
		PreserveTags:     false,
	})
	require.NotNil(t, transfer)

	// Try to cancel it immediately
	// Due to race conditions, this may succeed (if still pending) or fail (if already processing)
	cancelled := env.TryCancelTransfer(t, transfer.ID)

	if cancelled {
		// Verify it's cancelled
		result := env.GetTransfer(t, transfer.ID)
		assert.Equal(t, "cancelled", result.State, "transfer should be cancelled")
		t.Logf("Successfully cancelled transfer %d", transfer.ID)
	} else {
		// Transfer was already processing, wait for it to complete
		t.Logf("Transfer %d already processing, waiting for completion", transfer.ID)
		result := env.WaitForTransfer(t, transfer.ID, 5*time.Minute)
		assert.Equal(t, "completed", result.State, "transfer should complete if not cancelled")
		env.DeleteTorrent(t, qbit2, hash)
	}

	// Cleanup
	env.DeleteTorrent(t, qbit1, hash)
}

// TestTransfer_ZZ_TargetUnavailable tests transfer when target instance is unavailable
// Note: This test stops containers which may trigger IP bans in qBittorrent
func TestTransfer_ZZ_TargetUnavailable(t *testing.T) {
	require.NotNil(t, env, "test environment not initialized")

	qbit1 := env.GetQBitInstance("qbit1")
	qbit3 := env.GetQBitInstance("qbit3")
	require.NotNil(t, qbit1)
	require.NotNil(t, qbit3)

	// Add torrent to qbit1
	torrentPath := env.FixturePath("sintel.torrent")
	hash := env.AddTorrent(t, qbit1, torrentPath, "/downloads")
	env.WaitForDownload(t, qbit1, hash, 2*time.Minute)

	// Stop qbit3 container
	env.StopContainer(t, qbit3.ContainerName)

	// Attempt transfer to unavailable target
	transfer := env.StartTransferFull(t, TransferRequest{
		SourceInstanceID: qbit1.ID,
		TargetInstanceID: qbit3.ID,
		TorrentHash:      hash,
		DeleteFromSource: false,
		PreserveCategory: false,
		PreserveTags:     false,
	})
	require.NotNil(t, transfer)

	// Wait for transfer to fail
	result := env.WaitForTransfer(t, transfer.ID, 2*time.Minute)
	assert.Equal(t, "failed", result.State, "transfer should fail when target is unavailable")
	assert.NotEmpty(t, result.Error, "transfer error should contain message")
	t.Logf("Transfer failed with error: %s", result.Error)

	// Restart qbit3
	env.StartContainer(t, qbit3.ContainerName)
	env.WaitForInstance(t, qbit3, 1*time.Minute)

	// Cleanup
	env.DeleteTorrent(t, qbit1, hash)
}

// TestTransfer_ZZ_SourceUnavailable tests transfer when source instance becomes unavailable
// Note: This test stops containers which may trigger IP bans in qBittorrent
func TestTransfer_ZZ_SourceUnavailable(t *testing.T) {
	require.NotNil(t, env, "test environment not initialized")

	qbit1 := env.GetQBitInstance("qbit1")
	qbit2 := env.GetQBitInstance("qbit2")
	require.NotNil(t, qbit1)
	require.NotNil(t, qbit2)

	// Add torrent to qbit1
	torrentPath := env.FixturePath("wired-cd.torrent")
	hash := env.AddTorrent(t, qbit1, torrentPath, "/shared")
	env.WaitForDownload(t, qbit1, hash, 2*time.Minute)

	// Stop qbit1 container
	env.StopContainer(t, qbit1.ContainerName)

	// Attempt transfer from unavailable source
	transfer := env.StartTransferFull(t, TransferRequest{
		SourceInstanceID: qbit1.ID,
		TargetInstanceID: qbit2.ID,
		TorrentHash:      hash,
		DeleteFromSource: false,
		PreserveCategory: false,
		PreserveTags:     false,
	})
	require.NotNil(t, transfer)

	// Wait for transfer to fail
	result := env.WaitForTransfer(t, transfer.ID, 2*time.Minute)
	assert.Equal(t, "failed", result.State, "transfer should fail when source is unavailable")
	assert.NotEmpty(t, result.Error, "transfer error should contain message")
	t.Logf("Transfer failed with error: %s", result.Error)

	// Restart qbit1
	env.StartContainer(t, qbit1.ContainerName)
	env.WaitForInstance(t, qbit1, 1*time.Minute)

	// Cleanup
	env.DeleteTorrent(t, qbit1, hash)
}

// TestTransfer_ZZ_RollbackOnFailure tests that partial transfers are rolled back on failure
// Note: This test stops containers which may trigger IP bans in qBittorrent
func TestTransfer_ZZ_RollbackOnFailure(t *testing.T) {
	// This test verifies that failed transfers don't leave the torrent
	// in a bad state on the source.

	require.NotNil(t, env, "test environment not initialized")

	qbit1 := env.GetQBitInstance("qbit1")
	qbit3 := env.GetQBitInstance("qbit3")
	require.NotNil(t, qbit1)
	require.NotNil(t, qbit3)

	// Add torrent
	torrentPath := env.FixturePath("sintel.torrent")
	hash := env.AddTorrent(t, qbit1, torrentPath, "/downloads")
	env.WaitForDownload(t, qbit1, hash, 2*time.Minute)

	// Capture source state before transfer
	sourceInfoBefore := env.GetTorrentInfoFull(t, qbit1, hash)
	require.NotNil(t, sourceInfoBefore)

	// Stop qbit3 to cause failure during add torrent phase
	env.StopContainer(t, qbit3.ContainerName)

	// Start transfer (should fail when trying to add to target)
	transfer := env.StartTransferFull(t, TransferRequest{
		SourceInstanceID: qbit1.ID,
		TargetInstanceID: qbit3.ID,
		TorrentHash:      hash,
		DeleteFromSource: false,
		PreserveCategory: false,
		PreserveTags:     false,
	})
	require.NotNil(t, transfer)

	// Wait for transfer to fail
	result := env.WaitForTransfer(t, transfer.ID, 2*time.Minute)
	assert.Equal(t, "failed", result.State, "transfer should fail")

	// Verify source torrent is still intact
	sourceInfoAfter := env.GetTorrentInfoFull(t, qbit1, hash)
	require.NotNil(t, sourceInfoAfter, "source torrent should still exist")
	assert.Equal(t, sourceInfoBefore.Hash, sourceInfoAfter.Hash)
	assert.Equal(t, sourceInfoBefore.SavePath, sourceInfoAfter.SavePath)

	// Restart qbit3
	env.StartContainer(t, qbit3.ContainerName)
	env.WaitForInstance(t, qbit3, 1*time.Minute)

	// Cleanup
	env.DeleteTorrent(t, qbit1, hash)
}

//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// P0: Category/Tag Preservation Tests
// =============================================================================

// TestTransfer_PreserveCategory tests that category is preserved during transfer
func TestTransfer_PreserveCategory(t *testing.T) {
	require.NotNil(t, env, "test environment not initialized")

	qbit1 := env.GetQBitInstance("qbit1")
	qbit2 := env.GetQBitInstance("qbit2")
	require.NotNil(t, qbit1)
	require.NotNil(t, qbit2)

	// Add torrent
	torrentPath := env.FixturePath("sintel.torrent")
	hash := env.AddTorrent(t, qbit1, torrentPath, "/shared")
	env.WaitForDownload(t, qbit1, hash, 2*time.Minute)

	// Set category on source torrent
	env.SetTorrentCategory(t, qbit1, hash, "movies")

	// Verify category is set on source
	env.AssertTorrentCategory(t, qbit1, hash, "movies")

	// Transfer with preserveCategory=true
	transfer := env.StartTransferFull(t, TransferRequest{
		SourceInstanceID: qbit1.ID,
		TargetInstanceID: qbit2.ID,
		TorrentHash:      hash,
		SourceAction: "keep",
		PreserveCategory: true,
		PreserveTags:     false,
	})
	require.NotNil(t, transfer)

	// Wait for transfer to complete
	result := env.WaitForTransfer(t, transfer.ID, 5*time.Minute)
	require.Equal(t, "completed", result.State, "transfer should complete, got error: %s", result.Error)

	// Assert category is preserved on target
	env.AssertTorrentCategory(t, qbit2, hash, "movies")

	// Cleanup
	env.DeleteTorrent(t, qbit1, hash)
	env.DeleteTorrent(t, qbit2, hash)
}

// TestTransfer_PreserveTags tests that tags are preserved during transfer
func TestTransfer_PreserveTags(t *testing.T) {
	require.NotNil(t, env, "test environment not initialized")

	qbit1 := env.GetQBitInstance("qbit1")
	qbit2 := env.GetQBitInstance("qbit2")
	require.NotNil(t, qbit1)
	require.NotNil(t, qbit2)

	// Add torrent
	torrentPath := env.FixturePath("wired-cd.torrent")
	hash := env.AddTorrent(t, qbit1, torrentPath, "/shared")
	env.WaitForDownload(t, qbit1, hash, 2*time.Minute)

	// Set tags on source torrent
	env.SetTorrentTags(t, qbit1, hash, []string{"linux", "opensource"})

	// Verify tags are set on source
	env.AssertTorrentTags(t, qbit1, hash, []string{"linux", "opensource"})

	// Transfer with preserveTags=true
	transfer := env.StartTransferFull(t, TransferRequest{
		SourceInstanceID: qbit1.ID,
		TargetInstanceID: qbit2.ID,
		TorrentHash:      hash,
		SourceAction: "keep",
		PreserveCategory: false,
		PreserveTags:     true,
	})
	require.NotNil(t, transfer)

	// Wait for transfer to complete
	result := env.WaitForTransfer(t, transfer.ID, 5*time.Minute)
	require.Equal(t, "completed", result.State, "transfer should complete, got error: %s", result.Error)

	// Assert tags are preserved on target
	env.AssertTorrentTags(t, qbit2, hash, []string{"linux", "opensource"})

	// Cleanup
	env.DeleteTorrent(t, qbit1, hash)
	env.DeleteTorrent(t, qbit2, hash)
}

// TestTransfer_PreserveCategoryAndTags tests that both category and tags are preserved
func TestTransfer_PreserveCategoryAndTags(t *testing.T) {
	require.NotNil(t, env, "test environment not initialized")

	qbit1 := env.GetQBitInstance("qbit1")
	qbit2 := env.GetQBitInstance("qbit2")
	require.NotNil(t, qbit1)
	require.NotNil(t, qbit2)

	// Add torrent
	torrentPath := env.FixturePath("sintel.torrent")
	hash := env.AddTorrent(t, qbit1, torrentPath, "/shared")
	env.WaitForDownload(t, qbit1, hash, 2*time.Minute)

	// Set category and tags on source torrent
	env.SetTorrentCategory(t, qbit1, hash, "animation")
	env.SetTorrentTags(t, qbit1, hash, []string{"4k", "hdr", "creative-commons"})

	// Verify settings on source
	env.AssertTorrentCategory(t, qbit1, hash, "animation")
	env.AssertTorrentTags(t, qbit1, hash, []string{"4k", "hdr", "creative-commons"})

	// Transfer with both preserve options enabled
	transfer := env.StartTransferFull(t, TransferRequest{
		SourceInstanceID: qbit1.ID,
		TargetInstanceID: qbit2.ID,
		TorrentHash:      hash,
		SourceAction: "keep",
		PreserveCategory: true,
		PreserveTags:     true,
	})
	require.NotNil(t, transfer)

	// Wait for transfer to complete
	result := env.WaitForTransfer(t, transfer.ID, 5*time.Minute)
	require.Equal(t, "completed", result.State, "transfer should complete, got error: %s", result.Error)

	// Assert both category and tags are preserved on target
	env.AssertTorrentCategory(t, qbit2, hash, "animation")
	env.AssertTorrentTags(t, qbit2, hash, []string{"4k", "hdr", "creative-commons"})

	// Cleanup
	env.DeleteTorrent(t, qbit1, hash)
	env.DeleteTorrent(t, qbit2, hash)
}

// TestTransfer_NoPreserveCategory tests that category is NOT copied when preserveCategory=false
func TestTransfer_NoPreserveCategory(t *testing.T) {
	require.NotNil(t, env, "test environment not initialized")

	qbit1 := env.GetQBitInstance("qbit1")
	qbit2 := env.GetQBitInstance("qbit2")
	require.NotNil(t, qbit1)
	require.NotNil(t, qbit2)

	// Add torrent
	torrentPath := env.FixturePath("wired-cd.torrent")
	hash := env.AddTorrent(t, qbit1, torrentPath, "/shared")
	env.WaitForDownload(t, qbit1, hash, 2*time.Minute)

	// Set category on source torrent
	env.SetTorrentCategory(t, qbit1, hash, "music")

	// Transfer with preserveCategory=false
	transfer := env.StartTransferFull(t, TransferRequest{
		SourceInstanceID: qbit1.ID,
		TargetInstanceID: qbit2.ID,
		TorrentHash:      hash,
		SourceAction: "keep",
		PreserveCategory: false,
		PreserveTags:     false,
	})
	require.NotNil(t, transfer)

	// Wait for transfer to complete
	result := env.WaitForTransfer(t, transfer.ID, 5*time.Minute)
	require.Equal(t, "completed", result.State, "transfer should complete, got error: %s", result.Error)

	// Assert category is NOT set on target (should be empty)
	env.AssertTorrentCategory(t, qbit2, hash, "")

	// Cleanup
	env.DeleteTorrent(t, qbit1, hash)
	env.DeleteTorrent(t, qbit2, hash)
}

// TestTransfer_CreateMissingCategory tests that a category is created on target if it doesn't exist
func TestTransfer_CreateMissingCategory(t *testing.T) {
	require.NotNil(t, env, "test environment not initialized")

	qbit1 := env.GetQBitInstance("qbit1")
	qbit3 := env.GetQBitInstance("qbit3") // Use qbit3 which likely doesn't have the category
	require.NotNil(t, qbit1)
	require.NotNil(t, qbit3)

	// Add torrent
	torrentPath := env.FixturePath("sintel.torrent")
	hash := env.AddTorrent(t, qbit1, torrentPath, "/downloads")
	env.WaitForDownload(t, qbit1, hash, 2*time.Minute)

	// Set a unique category that likely doesn't exist on target
	uniqueCategory := "test-category-unique-12345"
	env.SetTorrentCategory(t, qbit1, hash, uniqueCategory)

	// Transfer with preserveCategory=true (should create category on target)
	transfer := env.StartTransferFull(t, TransferRequest{
		SourceInstanceID: qbit1.ID,
		TargetInstanceID: qbit3.ID,
		TorrentHash:      hash,
		SourceAction: "keep",
		PreserveCategory: true,
		PreserveTags:     false,
	})
	require.NotNil(t, transfer)

	// Wait for transfer to complete
	result := env.WaitForTransfer(t, transfer.ID, 5*time.Minute)
	require.Equal(t, "completed", result.State, "transfer should complete, got error: %s", result.Error)

	// Assert category is preserved on target (was created automatically)
	env.AssertTorrentCategory(t, qbit3, hash, uniqueCategory)

	// Cleanup
	env.DeleteTorrent(t, qbit1, hash)
	env.DeleteTorrent(t, qbit3, hash)
}

// =============================================================================
// P1: Path Mapping Tests
// =============================================================================

// TestTransfer_PathMapping_RequestOverride tests that path mappings in the request override defaults
func TestTransfer_PathMapping_RequestOverride(t *testing.T) {
	require.NotNil(t, env, "test environment not initialized")

	qbit1 := env.GetQBitInstance("qbit1")
	qbit3 := env.GetQBitInstance("qbit3")
	require.NotNil(t, qbit1)
	require.NotNil(t, qbit3)

	// Add torrent to /downloads on qbit1
	torrentPath := env.FixturePath("sintel.torrent")
	hash := env.AddTorrent(t, qbit1, torrentPath, "/downloads")
	env.WaitForDownload(t, qbit1, hash, 2*time.Minute)

	// Verify source save path
	sourceInfo := env.GetTorrentInfoFull(t, qbit1, hash)
	require.NotNil(t, sourceInfo)
	assert.Equal(t, "/downloads", sourceInfo.SavePath)

	// Transfer with custom path mapping: /downloads on source -> /downloads on target
	// Since qbit3 doesn't share volumes with qbit1, this will use rsync
	transfer := env.StartTransferFull(t, TransferRequest{
		SourceInstanceID: qbit1.ID,
		TargetInstanceID: qbit3.ID,
		TorrentHash:      hash,
		SourceAction: "keep",
		PreserveCategory: false,
		PreserveTags:     false,
		PathMappings: map[string]string{
			"/downloads": "/downloads", // Same path on target
		},
	})
	require.NotNil(t, transfer)

	// Wait for transfer to complete
	result := env.WaitForTransfer(t, transfer.ID, 5*time.Minute)
	require.Equal(t, "completed", result.State, "transfer should complete, got error: %s", result.Error)

	// Assert torrent exists on target with correct save path
	targetInfo := env.GetTorrentInfoFull(t, qbit3, hash)
	require.NotNil(t, targetInfo)
	assert.Equal(t, "/downloads", targetInfo.SavePath, "target save path should match path mapping")

	// Cleanup
	env.DeleteTorrent(t, qbit1, hash)
	env.DeleteTorrent(t, qbit3, hash)
}

// TestTransfer_DuplicateTransfer tests that duplicate transfers are rejected
func TestTransfer_DuplicateTransfer(t *testing.T) {
	require.NotNil(t, env, "test environment not initialized")

	qbit1 := env.GetQBitInstance("qbit1")
	qbit2 := env.GetQBitInstance("qbit2")
	require.NotNil(t, qbit1)
	require.NotNil(t, qbit2)

	// Add torrent
	torrentPath := env.FixturePath("sintel.torrent")
	hash := env.AddTorrent(t, qbit1, torrentPath, "/shared")
	env.WaitForDownload(t, qbit1, hash, 2*time.Minute)

	// Start first transfer
	transfer1 := env.StartTransferFull(t, TransferRequest{
		SourceInstanceID: qbit1.ID,
		TargetInstanceID: qbit2.ID,
		TorrentHash:      hash,
		SourceAction: "keep",
		PreserveCategory: false,
		PreserveTags:     false,
	})
	require.NotNil(t, transfer1)

	// Try to start duplicate transfer immediately (should fail)
	req := TransferRequest{
		SourceInstanceID: qbit1.ID,
		TargetInstanceID: qbit2.ID,
		TorrentHash:      hash,
		SourceAction: "keep",
		PreserveCategory: false,
		PreserveTags:     false,
	}
	jsonBody, _ := json.Marshal(req)

	httpReq, err := http.NewRequest("POST", env.QUIURL+"/api/transfers", bytes.NewReader(jsonBody))
	require.NoError(t, err)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := env.HTTPClient.Do(httpReq)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Should get a conflict or bad request response
	assert.True(t, resp.StatusCode >= 400, "duplicate transfer should be rejected, got status %d", resp.StatusCode)

	// Wait for first transfer to complete
	result := env.WaitForTransfer(t, transfer1.ID, 5*time.Minute)
	require.Equal(t, "completed", result.State)

	// Cleanup
	env.DeleteTorrent(t, qbit1, hash)
	env.DeleteTorrent(t, qbit2, hash)
}

// TestTransfer_TorrentNotFound tests transfer when torrent doesn't exist on source
func TestTransfer_TorrentNotFound(t *testing.T) {
	require.NotNil(t, env, "test environment not initialized")

	qbit1 := env.GetQBitInstance("qbit1")
	qbit2 := env.GetQBitInstance("qbit2")
	require.NotNil(t, qbit1)
	require.NotNil(t, qbit2)

	// Try to transfer a non-existent torrent (using a fake hash)
	fakeHash := "0000000000000000000000000000000000000000"

	transfer := env.StartTransferFull(t, TransferRequest{
		SourceInstanceID: qbit1.ID,
		TargetInstanceID: qbit2.ID,
		TorrentHash:      fakeHash,
		SourceAction: "keep",
		PreserveCategory: false,
		PreserveTags:     false,
	})
	require.NotNil(t, transfer)

	// Wait for transfer to fail
	result := env.WaitForTransfer(t, transfer.ID, 2*time.Minute)
	assert.Equal(t, "failed", result.State, "transfer should fail when torrent not found")
	assert.NotEmpty(t, result.Error, "transfer error should contain message")
	t.Logf("Transfer failed with error: %s", result.Error)
}

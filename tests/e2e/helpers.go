//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"
)

// qbitClients stores authenticated go-qbittorrent clients per instance
var (
	qbitClients   = make(map[string]*qbt.Client)
	qbitClientsMu sync.Mutex
)

// TorrentInfo represents qBittorrent torrent info from API
type TorrentInfo struct {
	Hash       string  `json:"hash"`
	Name       string  `json:"name"`
	SavePath   string  `json:"save_path"`
	State      string  `json:"state"`
	Progress   float64 `json:"progress"`
	Size       int64   `json:"size"`
	Downloaded int64   `json:"downloaded"`
}

// Transfer represents a QUI transfer from the API
type Transfer struct {
	ID               int64  `json:"id"`
	SourceInstanceID int    `json:"sourceInstanceId"`
	TargetInstanceID int    `json:"targetInstanceId"`
	TorrentHash      string `json:"torrentHash"`
	TorrentName      string `json:"torrentName"`
	State            string `json:"state"`
	LinkMode         string `json:"linkMode,omitempty"`
	DeleteFromSource bool   `json:"deleteFromSource"`
	Error            string `json:"error,omitempty"`
}

// GetQBitInstance returns an instance by name (qbit1, qbit2, qbit3)
func (e *TestEnv) GetQBitInstance(name string) *QBitInstance {
	for _, inst := range e.Instances {
		if inst.Name == name {
			return inst
		}
	}
	return nil
}

// AddTorrent adds a torrent to a qBittorrent instance and returns the hash
func (e *TestEnv) AddTorrent(t *testing.T, inst *QBitInstance, torrentPath, savePath string) string {
	t.Helper()

	// Get authenticated client
	client := e.getQBitClient(t, inst)

	// Read torrent file
	torrentData, err := os.ReadFile(torrentPath)
	require.NoError(t, err, "failed to read torrent file")

	// Add torrent using go-qbittorrent library
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	options := map[string]string{
		"savepath": savePath,
	}

	err = client.AddTorrentFromMemoryCtx(ctx, torrentData, options)
	require.NoError(t, err, "add torrent failed")

	// Extract hash from torrent filename (for known fixtures)
	hash := extractHashFromTorrent(torrentPath)

	// Wait a moment for qBittorrent to process the torrent
	time.Sleep(1 * time.Second)

	// Force resume the torrent to ensure it starts downloading
	e.resumeTorrent(t, inst, hash)

	t.Logf("Added torrent %s to %s (save path: %s)", hash, inst.Name, savePath)

	return hash
}

// resumeTorrent resumes/starts a torrent (library auto-detects API version)
func (e *TestEnv) resumeTorrent(t *testing.T, inst *QBitInstance, hash string) {
	t.Helper()

	client := e.getQBitClient(t, inst)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// go-qbittorrent library auto-detects API version and uses /start or /resume
	err := client.ResumeCtx(ctx, []string{hash})
	require.NoError(t, err, "resume torrent failed")
}

// getQBitClient returns an authenticated go-qbittorrent client for a qBittorrent instance
func (e *TestEnv) getQBitClient(t *testing.T, inst *QBitInstance) *qbt.Client {
	t.Helper()

	qbitClientsMu.Lock()
	defer qbitClientsMu.Unlock()

	// Return existing client if available
	if client, ok := qbitClients[inst.Name]; ok {
		return client
	}

	// Create new go-qbittorrent client
	cfg := qbt.Config{
		Host:     fmt.Sprintf("http://127.0.0.1:%d", inst.Port),
		Username: "admin",
		Password: inst.Password,
	}

	client := qbt.NewClient(cfg)

	// Login
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := client.LoginCtx(ctx)
	require.NoError(t, err, "qBittorrent login failed for %s", inst.Name)

	qbitClients[inst.Name] = client
	return client
}

// WaitForDownload waits for a torrent to complete downloading
func (e *TestEnv) WaitForDownload(t *testing.T, inst *QBitInstance, hash string, timeout time.Duration) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	errorRetries := 0
	maxErrorRetries := 3

	for {
		select {
		case <-ctx.Done():
			t.Fatalf("timeout waiting for torrent %s to download on %s", hash, inst.Name)
		case <-ticker.C:
			info := e.GetTorrentInfo(t, inst, hash)
			if info == nil {
				continue
			}
			t.Logf("Torrent %s on %s: state=%s progress=%.1f%%", hash[:8], inst.Name, info.State, info.Progress*100)

			// Handle error state by attempting to recheck and resume
			if info.State == "error" || info.State == "missingFiles" {
				errorRetries++
				if errorRetries <= maxErrorRetries {
					t.Logf("Torrent in %s state, attempting recheck (retry %d/%d)", info.State, errorRetries, maxErrorRetries)
					e.recheckTorrent(t, inst, hash)
					time.Sleep(2 * time.Second)
					e.resumeTorrent(t, inst, hash)
					continue
				}
			}

			if info.Progress >= 1.0 || info.State == "stalledUP" || info.State == "pausedUP" || info.State == "uploading" {
				t.Logf("Torrent %s download complete on %s", hash[:8], inst.Name)
				return
			}
		}
	}
}

// recheckTorrent forces a recheck of a torrent
func (e *TestEnv) recheckTorrent(t *testing.T, inst *QBitInstance, hash string) {
	t.Helper()

	client := e.getQBitClient(t, inst)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := client.RecheckCtx(ctx, []string{hash})
	require.NoError(t, err, "recheck torrent failed")
}

// StartTransfer initiates a transfer via QUI API
func (e *TestEnv) StartTransfer(t *testing.T, sourceID, targetID int, hash string, deleteFromSource bool) *Transfer {
	t.Helper()

	payload := map[string]any{
		"sourceInstanceId": sourceID,
		"targetInstanceId": targetID,
		"torrentHash":      hash,
		"deleteFromSource": deleteFromSource,
	}
	jsonBody, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST", e.QUIURL+"/api/transfers", bytes.NewReader(jsonBody))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.HTTPClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	require.Equal(t, http.StatusCreated, resp.StatusCode, "create transfer failed: %s", body)

	var transfer Transfer
	err = json.Unmarshal(body, &transfer)
	require.NoError(t, err)

	t.Logf("Started transfer %d: %s -> instance %d (deleteFromSource=%v)", transfer.ID, hash[:8], targetID, deleteFromSource)
	return &transfer
}

// WaitForTransfer waits for a transfer to complete and returns final state
func (e *TestEnv) WaitForTransfer(t *testing.T, transferID int64, timeout time.Duration) *Transfer {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			t.Fatalf("timeout waiting for transfer %d to complete", transferID)
		case <-ticker.C:
			transfer := e.GetTransfer(t, transferID)
			if transfer == nil {
				continue
			}
			t.Logf("Transfer %d: state=%s", transferID, transfer.State)

			// Check for terminal states
			switch transfer.State {
			case "completed", "failed", "rolled_back", "cancelled":
				return transfer
			}
		}
	}
}

// GetTransfer retrieves a transfer by ID
func (e *TestEnv) GetTransfer(t *testing.T, transferID int64) *Transfer {
	t.Helper()

	url := fmt.Sprintf("%s/api/transfers/%d", e.QUIURL, transferID)
	req, err := http.NewRequest("GET", url, nil)
	require.NoError(t, err)

	resp, err := e.HTTPClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil
	}

	var transfer Transfer
	err = json.NewDecoder(resp.Body).Decode(&transfer)
	require.NoError(t, err)
	return &transfer
}

// GetTorrentInfo gets torrent info from qBittorrent
func (e *TestEnv) GetTorrentInfo(t *testing.T, inst *QBitInstance, hash string) *TorrentInfo {
	t.Helper()

	client := e.getQBitClient(t, inst)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	torrents, err := client.GetTorrentsCtx(ctx, qbt.TorrentFilterOptions{
		Hashes: []string{hash},
	})
	require.NoError(t, err)

	if len(torrents) == 0 {
		return nil
	}

	// Map library struct to test struct
	torrent := torrents[0]
	return &TorrentInfo{
		Hash:       torrent.Hash,
		Name:       torrent.Name,
		SavePath:   torrent.SavePath,
		State:      string(torrent.State),
		Progress:   torrent.Progress,
		Size:       torrent.Size,
		Downloaded: torrent.Downloaded,
	}
}

// AssertTorrentExists verifies a torrent exists on an instance
func (e *TestEnv) AssertTorrentExists(t *testing.T, inst *QBitInstance, hash string) {
	t.Helper()

	info := e.GetTorrentInfo(t, inst, hash)
	require.NotNil(t, info, "expected torrent %s to exist on %s", hash, inst.Name)
}

// AssertTorrentNotExists verifies a torrent does NOT exist on an instance
func (e *TestEnv) AssertTorrentNotExists(t *testing.T, inst *QBitInstance, hash string) {
	t.Helper()

	info := e.GetTorrentInfo(t, inst, hash)
	require.Nil(t, info, "expected torrent %s to NOT exist on %s, but it does", hash, inst.Name)
}

// DeleteTorrent removes a torrent from qBittorrent (keeps files for reuse by subsequent tests)
func (e *TestEnv) DeleteTorrent(t *testing.T, inst *QBitInstance, hash string) {
	t.Helper()

	client := e.getQBitClient(t, inst)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// deleteFiles=false to keep files for reuse
	err := client.DeleteTorrentsCtx(ctx, []string{hash}, false)
	require.NoError(t, err, "delete torrent failed")

	t.Logf("Deleted torrent %s from %s (files kept)", hash[:8], inst.Name)
}

// extractHashFromTorrent returns the known hash for fixture torrents
func extractHashFromTorrent(path string) string {
	// Known hashes for our fixtures
	switch filepath.Base(path) {
	case "sintel.torrent":
		return "08ada5a7a6183aae1e09d831df6748d566095a10"
	case "wired-cd.torrent":
		return "a88fda5954e89178c372716a6a78b8180ed4dad3"
	default:
		return ""
	}
}

// FixturePath returns the path to a fixture file
func (e *TestEnv) FixturePath(name string) string {
	return filepath.Join(e.TestDir, "fixtures", name)
}

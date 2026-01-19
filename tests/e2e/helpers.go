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
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
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
	FileExistsAction string `json:"fileExistsAction"`
	SourceAction     string `json:"sourceAction"`
	VerifyTransfer   bool   `json:"verifyTransfer"`
	PreserveCategory bool   `json:"preserveCategory"`
	PreserveTags     bool   `json:"preserveTags"`
	TargetCategory   string `json:"targetCategory,omitempty"`
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
	zeroProgressCount := 0
	recheckCount := 0
	maxRecheckCount := 3

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

			// If checking, reset counters and wait
			if info.State == "checkingDL" || info.State == "checkingUP" || info.State == "checkingResumeData" {
				zeroProgressCount = 0
				continue
			}

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

			// Handle stuck at 0% in downloading state (files pre-placed but not recognized)
			if info.Progress < 0.01 && (info.State == "stalledDL" || info.State == "downloading") {
				zeroProgressCount++
				// After 5 polls (~10 seconds) at 0%, trigger a recheck (allow up to 3 rechecks)
				if zeroProgressCount >= 5 && recheckCount < maxRecheckCount {
					recheckCount++
					t.Logf("Torrent stuck at 0%% in %s state, triggering recheck (%d/%d)", info.State, recheckCount, maxRecheckCount)
					e.recheckTorrent(t, inst, hash)
					zeroProgressCount = 0 // Reset counter after recheck
					continue
				}
			} else if info.Progress > 0.01 {
				zeroProgressCount = 0
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
// sourceAction: true = "delete", false = "keep"
func (e *TestEnv) StartTransfer(t *testing.T, sourceID, targetID int, hash string, deleteSource bool) *Transfer {
	t.Helper()

	sourceAction := "keep"
	if deleteSource {
		sourceAction = "delete"
	}

	payload := map[string]any{
		"sourceInstanceId": sourceID,
		"targetInstanceId": targetID,
		"torrentHash":      hash,
		"sourceAction":     sourceAction,
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

	t.Logf("Started transfer %d: %s -> instance %d (sourceAction=%s)", transfer.ID, hash[:8], targetID, sourceAction)
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

// TransferRequest represents a full transfer request with all options
type TransferRequest struct {
	SourceInstanceID int               `json:"sourceInstanceId"`
	TargetInstanceID int               `json:"targetInstanceId"`
	TorrentHash      string            `json:"torrentHash"`
	FileExistsAction string            `json:"fileExistsAction,omitempty"` // "abort", "skip", "overwrite"
	SourceAction     string            `json:"sourceAction,omitempty"`     // "keep", "pause", "delete"
	VerifyTransfer   bool              `json:"verifyTransfer,omitempty"`
	PreserveCategory bool              `json:"preserveCategory"`
	PreserveTags     bool              `json:"preserveTags"`
	PathMappings     map[string]string `json:"pathMappings,omitempty"`
}

// StartTransferFull initiates a transfer with all options via QUI API
func (e *TestEnv) StartTransferFull(t *testing.T, req TransferRequest) *Transfer {
	t.Helper()

	jsonBody, _ := json.Marshal(req)

	httpReq, err := http.NewRequest("POST", e.QUIURL+"/api/transfers", bytes.NewReader(jsonBody))
	require.NoError(t, err)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := e.HTTPClient.Do(httpReq)
	require.NoError(t, err)
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	require.Equal(t, http.StatusCreated, resp.StatusCode, "create transfer failed: %s", body)

	var transfer Transfer
	err = json.Unmarshal(body, &transfer)
	require.NoError(t, err)

	t.Logf("Started transfer %d: %s -> instance %d (sourceAction=%s, verify=%v)",
		transfer.ID, req.TorrentHash[:8], req.TargetInstanceID, req.SourceAction, req.VerifyTransfer)
	return &transfer
}

// SetTorrentCategory sets the category on a torrent
func (e *TestEnv) SetTorrentCategory(t *testing.T, inst *QBitInstance, hash, category string) {
	t.Helper()

	client := e.getQBitClient(t, inst)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create category first if it doesn't exist
	categories, err := client.GetCategoriesCtx(ctx)
	require.NoError(t, err)

	if _, exists := categories[category]; !exists && category != "" {
		err = client.CreateCategoryCtx(ctx, category, "")
		require.NoError(t, err)
	}

	err = client.SetCategoryCtx(ctx, []string{hash}, category)
	require.NoError(t, err)

	t.Logf("Set category %q on torrent %s at %s", category, hash[:8], inst.Name)
}

// SetTorrentTags sets tags on a torrent (replaces existing tags)
func (e *TestEnv) SetTorrentTags(t *testing.T, inst *QBitInstance, hash string, tags []string) {
	t.Helper()

	client := e.getQBitClient(t, inst)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create tags first if they don't exist
	if len(tags) > 0 {
		err := client.CreateTagsCtx(ctx, tags)
		require.NoError(t, err)
	}

	// Add tags to torrent
	tagsStr := strings.Join(tags, ",")
	err := client.AddTagsCtx(ctx, []string{hash}, tagsStr)
	require.NoError(t, err)

	t.Logf("Set tags %v on torrent %s at %s", tags, hash[:8], inst.Name)
}

// GetTorrentCategory returns the category of a torrent
func (e *TestEnv) GetTorrentCategory(t *testing.T, inst *QBitInstance, hash string) string {
	t.Helper()

	info := e.GetTorrentInfoFull(t, inst, hash)
	if info == nil {
		return ""
	}
	return info.Category
}

// GetTorrentTags returns the tags of a torrent
func (e *TestEnv) GetTorrentTags(t *testing.T, inst *QBitInstance, hash string) []string {
	t.Helper()

	info := e.GetTorrentInfoFull(t, inst, hash)
	if info == nil {
		return nil
	}
	if info.Tags == "" {
		return nil
	}
	return strings.Split(info.Tags, ", ")
}

// TorrentInfoFull contains all torrent info including category and tags
type TorrentInfoFull struct {
	Hash       string  `json:"hash"`
	Name       string  `json:"name"`
	SavePath   string  `json:"save_path"`
	State      string  `json:"state"`
	Progress   float64 `json:"progress"`
	Size       int64   `json:"size"`
	Downloaded int64   `json:"downloaded"`
	Category   string  `json:"category"`
	Tags       string  `json:"tags"`
}

// GetTorrentInfoFull gets full torrent info including category and tags
func (e *TestEnv) GetTorrentInfoFull(t *testing.T, inst *QBitInstance, hash string) *TorrentInfoFull {
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

	torrent := torrents[0]
	return &TorrentInfoFull{
		Hash:       torrent.Hash,
		Name:       torrent.Name,
		SavePath:   torrent.SavePath,
		State:      string(torrent.State),
		Progress:   torrent.Progress,
		Size:       torrent.Size,
		Downloaded: torrent.Downloaded,
		Category:   torrent.Category,
		Tags:       torrent.Tags,
	}
}

// AssertTorrentCategory verifies the torrent has the expected category
func (e *TestEnv) AssertTorrentCategory(t *testing.T, inst *QBitInstance, hash, expected string) {
	t.Helper()

	actual := e.GetTorrentCategory(t, inst, hash)
	require.Equal(t, expected, actual, "expected category %q but got %q on %s", expected, actual, inst.Name)
}

// AssertTorrentTags verifies the torrent has the expected tags
func (e *TestEnv) AssertTorrentTags(t *testing.T, inst *QBitInstance, hash string, expected []string) {
	t.Helper()

	actual := e.GetTorrentTags(t, inst, hash)

	// Sort both for comparison
	sortedExpected := make([]string, len(expected))
	copy(sortedExpected, expected)
	sort.Strings(sortedExpected)

	sortedActual := make([]string, len(actual))
	copy(sortedActual, actual)
	sort.Strings(sortedActual)

	require.Equal(t, sortedExpected, sortedActual, "expected tags %v but got %v on %s", expected, actual, inst.Name)
}

// ClearQBitClient removes the cached qBittorrent client for an instance
func (e *TestEnv) ClearQBitClient(inst *QBitInstance) {
	qbitClientsMu.Lock()
	defer qbitClientsMu.Unlock()
	delete(qbitClients, inst.Name)
}

// StopContainer stops a Docker container and clears the client cache
func (e *TestEnv) StopContainer(t *testing.T, containerName string) {
	t.Helper()

	// Find and clear the client for this container
	for _, inst := range e.Instances {
		if inst.ContainerName == containerName {
			e.ClearQBitClient(inst)
			break
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "stop", containerName)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "failed to stop container %s: %s", containerName, out)

	t.Logf("Stopped container %s", containerName)
}

// StartContainer starts a Docker container
func (e *TestEnv) StartContainer(t *testing.T, containerName string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "start", containerName)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "failed to start container %s: %s", containerName, out)

	t.Logf("Started container %s", containerName)

	// Wait for container to be ready
	time.Sleep(5 * time.Second)
}

// WaitForInstance waits for an instance to be accessible and refreshes the client cache.
// It also refreshes the password from container logs in case qBittorrent restarted.
func (e *TestEnv) WaitForInstance(t *testing.T, inst *QBitInstance, timeout time.Duration) {
	t.Helper()

	// Clear any cached client first
	e.ClearQBitClient(inst)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	// Regex to find password from logs
	pwRegex := regexp.MustCompile(`temporary password.*: (\S+)`)

	for {
		select {
		case <-ctx.Done():
			t.Fatalf("timeout waiting for instance %s to be ready", inst.Name)
		case <-ticker.C:
			// Get the latest password from container logs (in case it changed on restart)
			// Use full logs since password might be at the start
			cmd := exec.CommandContext(ctx, "docker", "logs", inst.ContainerName)
			out, _ := cmd.Output()
			// Find all password occurrences and use the last one (most recent)
			allMatches := pwRegex.FindAllStringSubmatch(string(out), -1)
			if len(allMatches) > 0 {
				newPassword := allMatches[len(allMatches)-1][1]
				if newPassword != inst.Password {
					t.Logf("Password changed for %s: %s -> %s", inst.Name, inst.Password, newPassword)
					inst.Password = newPassword
				}
			}

			// Try to login with a fresh client
			cfg := qbt.Config{
				Host:     fmt.Sprintf("http://127.0.0.1:%d", inst.Port),
				Username: "admin",
				Password: inst.Password,
			}
			client := qbt.NewClient(cfg)

			loginCtx, loginCancel := context.WithTimeout(ctx, 5*time.Second)
			err := client.LoginCtx(loginCtx)
			loginCancel()

			if err == nil {
				// Cache the working client
				qbitClientsMu.Lock()
				qbitClients[inst.Name] = client
				qbitClientsMu.Unlock()
				t.Logf("Instance %s is ready", inst.Name)
				return
			}
		}
	}
}

// CancelTransfer cancels a pending transfer
func (e *TestEnv) CancelTransfer(t *testing.T, transferID int64) {
	t.Helper()

	url := fmt.Sprintf("%s/api/transfers/%d", e.QUIURL, transferID)
	req, err := http.NewRequest("DELETE", url, nil)
	require.NoError(t, err)

	resp, err := e.HTTPClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode, "cancel transfer failed")
	t.Logf("Cancelled transfer %d", transferID)
}

// TryCancelTransfer attempts to cancel a transfer and returns true if successful.
// Returns false if the transfer cannot be cancelled (e.g., already processing).
func (e *TestEnv) TryCancelTransfer(t *testing.T, transferID int64) bool {
	t.Helper()

	url := fmt.Sprintf("%s/api/transfers/%d", e.QUIURL, transferID)
	req, err := http.NewRequest("DELETE", url, nil)
	require.NoError(t, err)

	resp, err := e.HTTPClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}

// GetTransferError retrieves the error message from a failed transfer
func (e *TestEnv) GetTransferError(t *testing.T, transferID int64) string {
	t.Helper()

	transfer := e.GetTransfer(t, transferID)
	if transfer == nil {
		return ""
	}
	return transfer.Error
}

// UpdateSSHConnectionType updates the SSH connection type for an instance.
// Valid types: ssh_auto, ssh_rsync, ssh_sftp, ssh_scp
func (e *TestEnv) UpdateSSHConnectionType(t *testing.T, inst *QBitInstance, connType string) {
	t.Helper()

	// First, get the existing connection ID
	url := fmt.Sprintf("%s/api/instances/%d/connections", e.QUIURL, inst.ID)
	req, err := http.NewRequest("GET", url, nil)
	require.NoError(t, err)

	resp, err := e.HTTPClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	var connections []struct {
		ID   int    `json:"id"`
		Type string `json:"type"`
	}
	err = json.NewDecoder(resp.Body).Decode(&connections)
	require.NoError(t, err)

	if len(connections) == 0 {
		t.Fatalf("No connections found for instance %s", inst.Name)
	}

	// Update the connection type
	connID := connections[0].ID
	updateURL := fmt.Sprintf("%s/api/instances/%d/connections/%d", e.QUIURL, inst.ID, connID)
	body := fmt.Sprintf(`{"type": "%s"}`, connType)

	patchReq, err := http.NewRequest("PATCH", updateURL, strings.NewReader(body))
	require.NoError(t, err)
	patchReq.Header.Set("Content-Type", "application/json")

	patchResp, err := e.HTTPClient.Do(patchReq)
	require.NoError(t, err)
	defer patchResp.Body.Close()

	require.Equal(t, http.StatusOK, patchResp.StatusCode, "update connection type failed")
	t.Logf("Updated SSH connection type for %s to %s", inst.Name, connType)
}

// GetInstanceConnections returns the connections for an instance
func (e *TestEnv) GetInstanceConnections(t *testing.T, inst *QBitInstance) []map[string]interface{} {
	t.Helper()

	url := fmt.Sprintf("%s/api/instances/%d/connections", e.QUIURL, inst.ID)
	req, err := http.NewRequest("GET", url, nil)
	require.NoError(t, err)

	resp, err := e.HTTPClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	var connections []map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&connections)
	require.NoError(t, err)

	return connections
}

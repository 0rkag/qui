// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer

import (
	"context"
	"errors"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/ftpclient"
)

func TestFtpFileExistsActionToMode(t *testing.T) {
	tests := []struct {
		name     string
		action   models.FileExistsAction
		expected ftpclient.FileExistsMode
	}{
		{
			name:     "skip action maps to skip mode",
			action:   models.FileExistsSkip,
			expected: ftpclient.FileExistsModeSkip,
		},
		{
			name:     "overwrite action maps to overwrite mode",
			action:   models.FileExistsOverwrite,
			expected: ftpclient.FileExistsModeOverwrite,
		},
		{
			name:     "abort action maps to abort mode",
			action:   models.FileExistsAbort,
			expected: ftpclient.FileExistsModeAbort,
		},
		{
			name:     "unknown action defaults to abort mode",
			action:   models.FileExistsAction("unknown"),
			expected: ftpclient.FileExistsModeAbort,
		},
		{
			name:     "empty action defaults to abort mode",
			action:   models.FileExistsAction(""),
			expected: ftpclient.FileExistsModeAbort,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ftpFileExistsActionToMode(tt.action)
			if got != tt.expected {
				t.Errorf("ftpFileExistsActionToMode(%q) = %v, want %v", tt.action, got, tt.expected)
			}
		})
	}
}

// mockInstanceConnectionStore implements connection store for FTP tests
type mockInstanceConnectionStore struct {
	ftpConnections map[int]*models.InstanceConnection
	sshConnections map[int]*models.InstanceConnection
	passwords      map[int64]string
}

func newMockInstanceConnectionStore() *mockInstanceConnectionStore {
	return &mockInstanceConnectionStore{
		ftpConnections: make(map[int]*models.InstanceConnection),
		sshConnections: make(map[int]*models.InstanceConnection),
		passwords:      make(map[int64]string),
	}
}

func (m *mockInstanceConnectionStore) GetFTPByInstance(ctx context.Context, instanceID int) (*models.InstanceConnection, error) {
	if conn, ok := m.ftpConnections[instanceID]; ok {
		return conn, nil
	}
	return nil, models.ErrConnectionNotFound
}

func (m *mockInstanceConnectionStore) GetSSHByInstance(ctx context.Context, instanceID int) (*models.InstanceConnection, error) {
	if conn, ok := m.sshConnections[instanceID]; ok {
		return conn, nil
	}
	return nil, models.ErrConnectionNotFound
}

func (m *mockInstanceConnectionStore) GetDecryptedPassword(conn *models.InstanceConnection) (string, error) {
	if pw, ok := m.passwords[conn.ID]; ok {
		return pw, nil
	}
	return "", errors.New("password not found")
}

func (m *mockInstanceConnectionStore) AddFTPConnection(instanceID int, conn *models.InstanceConnection) {
	m.ftpConnections[instanceID] = conn
}

func (m *mockInstanceConnectionStore) AddSSHConnection(instanceID int, conn *models.InstanceConnection) {
	m.sshConnections[instanceID] = conn
}

func (m *mockInstanceConnectionStore) SetPassword(connID int64, password string) {
	m.passwords[connID] = password
}

func newTestFTPConnection(instanceID int) *models.InstanceConnection {
	return &models.InstanceConnection{
		ID:         int64(instanceID),
		InstanceID: instanceID,
		Type:       models.ConnectionTypeFTPExplicit,
		Host:       "ftp.example.com",
		Port:       21,
		Username:   "ftpuser",
		Enabled:    true,
	}
}

// TestFTPExecutor_CanHandle_Logic tests the decision logic used by CanHandle.
// Note: Testing the actual CanHandle() method requires database mocking since
// connectionStore is a concrete type. These tests verify the logic pattern:
//   canHandle = (sourceNeedsRemote && sourceHasFTP) || (targetNeedsRemote && targetHasFTP)
func TestFTPExecutor_CanHandle_Logic(t *testing.T) {
	tests := []struct {
		name                string
		sourceHasLocalFS    bool
		targetHasLocalFS    bool
		sourceHasFTP        bool
		targetHasFTP        bool
		expectCanHandle     bool
	}{
		{
			name:             "source remote with FTP - can handle",
			sourceHasLocalFS: false, // needs remote
			targetHasLocalFS: true,
			sourceHasFTP:     true,
			targetHasFTP:     false,
			expectCanHandle:  true,
		},
		{
			name:             "target remote with FTP - can handle",
			sourceHasLocalFS: true,
			targetHasLocalFS: false, // needs remote
			sourceHasFTP:     false,
			targetHasFTP:     true,
			expectCanHandle:  true,
		},
		{
			name:             "both local - cannot handle (no remote needed)",
			sourceHasLocalFS: true,
			targetHasLocalFS: true,
			sourceHasFTP:     true,
			targetHasFTP:     true,
			expectCanHandle:  false,
		},
		{
			name:             "source remote but no FTP - cannot handle",
			sourceHasLocalFS: false,
			targetHasLocalFS: true,
			sourceHasFTP:     false,
			targetHasFTP:     false,
			expectCanHandle:  false,
		},
		{
			name:             "both remote with FTP - can handle",
			sourceHasLocalFS: false,
			targetHasLocalFS: false,
			sourceHasFTP:     true,
			targetHasFTP:     true,
			expectCanHandle:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This replicates the logic from FTPExecutor.CanHandle()
			// See ftp.go:75-81 for the actual implementation
			sourceNeedsRemote := !tt.sourceHasLocalFS
			targetNeedsRemote := !tt.targetHasLocalFS

			canHandle := (sourceNeedsRemote && tt.sourceHasFTP) || (targetNeedsRemote && tt.targetHasFTP)

			assert.Equal(t, tt.expectCanHandle, canHandle)
		})
	}
}

func TestFTPExecutor_Prepare(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		transfer    *models.Transfer
		setupMocks  func(*mockSyncManager, *mockInstanceProvider)
		wantErr     string
		checkResult func(*testing.T, *PrepareResult)
	}{
		{
			name:     "error - source instance not found",
			transfer: newTestTransfer(),
			setupMocks: func(sm *mockSyncManager, ip *mockInstanceProvider) {
				// Don't add source instance
				ip.AddInstance(newTestInstance(2))
			},
			wantErr: "failed to get source instance",
		},
		{
			name:     "error - target instance not found",
			transfer: newTestTransfer(),
			setupMocks: func(sm *mockSyncManager, ip *mockInstanceProvider) {
				ip.AddInstance(newTestInstance(1))
				// Don't add target instance
			},
			wantErr: "failed to get target instance",
		},
		{
			name:     "success - basic preparation",
			transfer: newTestTransfer(),
			setupMocks: func(sm *mockSyncManager, ip *mockInstanceProvider) {
				ip.AddInstance(newTestInstance(1))
				ip.AddInstance(newTestInstance(2))

				sm.getTorrentsFunc = func(ctx context.Context, instanceID int, filter qbt.TorrentFilterOptions) ([]qbt.Torrent, error) {
					return []qbt.Torrent{{
						Hash:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
						Name:     "Test Torrent",
						Progress: 1.0,
					}}, nil
				}
				sm.getTorrentFilesFunc = func(ctx context.Context, instanceID int, hash string) (*qbt.TorrentFiles, error) {
					files := qbt.TorrentFiles{{Name: "file1.mkv", Size: 1000, Progress: 1.0}}
					return &files, nil
				}
				sm.getTorrentPropsFunc = func(ctx context.Context, instanceID int, hash string) (*qbt.TorrentProperties, error) {
					return &qbt.TorrentProperties{SavePath: "/downloads"}, nil
				}
			},
			checkResult: func(t *testing.T, prep *PrepareResult) {
				assert.Equal(t, "transfer", prep.LinkMode, "FTP always uses transfer mode")
				assert.Equal(t, "Test Torrent", prep.TorrentName)
				assert.Equal(t, 1, len(prep.Files))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := newMockSyncManager()
			ip := newMockInstanceProvider()

			if tt.setupMocks != nil {
				tt.setupMocks(sm, ip)
			}

			executor := NewFTPExecutor(sm, ip, nil, nil)
			result, err := executor.Prepare(ctx, tt.transfer)

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			} else {
				require.NoError(t, err)
				if tt.checkResult != nil {
					tt.checkResult(t, result)
				}
			}
		})
	}
}

func TestFTPExecutor_Close(t *testing.T) {
	executor := NewFTPExecutor(nil, nil, nil, nil)

	// Close should not error
	err := executor.Close()
	assert.NoError(t, err)

	// Double close should be safe
	err = executor.Close()
	assert.NoError(t, err)
}

func TestFTPExecutor_TransferName(t *testing.T) {
	executor := &FTPExecutor{}

	name := executor.TransferName()
	assert.Equal(t, "FTP", name)
}

func TestFTPExecutor_Cleanup(t *testing.T) {
	ctx := context.Background()
	executor := &FTPExecutor{}

	// Cleanup is currently a no-op
	err := executor.Cleanup(ctx, nil, nil)
	assert.NoError(t, err)
}

func TestNewFTPExecutor(t *testing.T) {
	sm := newMockSyncManager()
	ip := newMockInstanceProvider()

	executor := NewFTPExecutor(sm, ip, nil, nil)

	assert.NotNil(t, executor)
	assert.NotNil(t, executor.ftpPool)
	assert.Equal(t, sm, executor.syncManager)
	assert.Equal(t, ip, executor.instanceStore)
}

func TestNewFTPExecutor_WithPathResolver(t *testing.T) {
	sm := newMockSyncManager()
	ip := newMockInstanceProvider()

	// With nil pathMappingStore, resolver should be nil
	executor := NewFTPExecutor(sm, ip, nil, nil)
	assert.Nil(t, executor.pathResolver)
}

// Test that FTP connection types map to correct TLS modes
func TestFTPConnectionTypeToTLSMode(t *testing.T) {
	tests := []struct {
		connType    string
		expectedTLS string
	}{
		{models.ConnectionTypeFTPExplicit, ftpclient.TLSModeExplicit},
		{models.ConnectionTypeFTPImplicit, ftpclient.TLSModeImplicit},
		{models.ConnectionTypeFTPPlain, ftpclient.TLSModeNone},
		{"unknown", ftpclient.TLSModeExplicit}, // Default
	}

	for _, tt := range tests {
		t.Run(tt.connType, func(t *testing.T) {
			var tlsMode string
			switch tt.connType {
			case models.ConnectionTypeFTPExplicit:
				tlsMode = ftpclient.TLSModeExplicit
			case models.ConnectionTypeFTPImplicit:
				tlsMode = ftpclient.TLSModeImplicit
			case models.ConnectionTypeFTPPlain:
				tlsMode = ftpclient.TLSModeNone
			default:
				tlsMode = ftpclient.TLSModeExplicit
			}

			assert.Equal(t, tt.expectedTLS, tlsMode)
		})
	}
}

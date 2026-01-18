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
)

// Test helpers for SSH tests

func newTestSSHConnection(instanceID int) *models.InstanceConnection {
	return &models.InstanceConnection{
		ID:             int64(instanceID),
		InstanceID:     instanceID,
		Type:           models.ConnectionTypeSSHAuto,
		Host:           "127.0.0.1",
		Port:           22,
		Username:       "root",
		PrivateKeyPath: "/path/to/key",
		Enabled:        true,
	}
}

func withRemoteAccess() func(*models.Instance) {
	return func(i *models.Instance) {
		i.HasLocalFilesystemAccess = false
	}
}

func TestSSHExecutor_CanHandle(t *testing.T) {
	executor := &SSHExecutor{}

	tests := []struct {
		name        string
		sourceLocal bool
		targetLocal bool
		expected    bool
	}{
		{
			name:        "both local - cannot handle (use LocalExecutor)",
			sourceLocal: true,
			targetLocal: true,
			expected:    false,
		},
		{
			name:        "source remote - can handle",
			sourceLocal: false,
			targetLocal: true,
			expected:    true,
		},
		{
			name:        "target remote - can handle",
			sourceLocal: true,
			targetLocal: false,
			expected:    true,
		},
		{
			name:        "both remote - can handle",
			sourceLocal: false,
			targetLocal: false,
			expected:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := newTestInstance(1, withLocalAccess(tt.sourceLocal))
			target := newTestInstance(2, withLocalAccess(tt.targetLocal))

			result := executor.CanHandle(source, target)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSSHExecutor_Prepare(t *testing.T) {
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
				ip.AddInstance(newTestInstance(2, withRemoteAccess()))
			},
			wantErr: "failed to get source instance",
		},
		{
			name:     "error - target instance not found",
			transfer: newTestTransfer(),
			setupMocks: func(sm *mockSyncManager, ip *mockInstanceProvider) {
				ip.AddInstance(newTestInstance(1, withRemoteAccess()))
				// Don't add target instance
			},
			wantErr: "failed to get target instance",
		},
		{
			name:     "error - torrent not found",
			transfer: newTestTransfer(),
			setupMocks: func(sm *mockSyncManager, ip *mockInstanceProvider) {
				ip.AddInstance(newTestInstance(1, withRemoteAccess()))
				ip.AddInstance(newTestInstance(2, withRemoteAccess()))

				sm.getTorrentsFunc = func(ctx context.Context, instanceID int, filter qbt.TorrentFilterOptions) ([]qbt.Torrent, error) {
					return []qbt.Torrent{}, nil // Empty - no torrent found
				}
			},
			wantErr: "torrent not found",
		},
		{
			name:     "error - no complete files",
			transfer: newTestTransfer(),
			setupMocks: func(sm *mockSyncManager, ip *mockInstanceProvider) {
				ip.AddInstance(newTestInstance(1, withRemoteAccess()))
				ip.AddInstance(newTestInstance(2, withRemoteAccess()))

				sm.getTorrentsFunc = func(ctx context.Context, instanceID int, filter qbt.TorrentFilterOptions) ([]qbt.Torrent, error) {
					return []qbt.Torrent{{
						Hash:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
						Name:     "Test Torrent",
						Progress: 0.5, // Partial progress is OK for SSH executor
					}}, nil
				}
				sm.getTorrentFilesFunc = func(ctx context.Context, instanceID int, hash string) (*qbt.TorrentFiles, error) {
					// All files incomplete
					files := qbt.TorrentFiles{{Name: "file1.mkv", Size: 1000, Progress: 0.5}}
					return &files, nil
				}
				sm.getTorrentPropsFunc = func(ctx context.Context, instanceID int, hash string) (*qbt.TorrentProperties, error) {
					return &qbt.TorrentProperties{SavePath: "/downloads"}, nil
				}
			},
			wantErr: "no complete files to transfer",
		},
		{
			name:     "success - partial transfer with some complete files",
			transfer: newTestTransfer(),
			setupMocks: func(sm *mockSyncManager, ip *mockInstanceProvider) {
				ip.AddInstance(newTestInstance(1, withRemoteAccess()))
				ip.AddInstance(newTestInstance(2, withRemoteAccess()))

				sm.getTorrentsFunc = func(ctx context.Context, instanceID int, filter qbt.TorrentFilterOptions) ([]qbt.Torrent, error) {
					return []qbt.Torrent{{
						Hash:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
						Name:     "Test Torrent",
						Progress: 0.5,
					}}, nil
				}
				sm.getTorrentFilesFunc = func(ctx context.Context, instanceID int, hash string) (*qbt.TorrentFiles, error) {
					files := qbt.TorrentFiles{
						{Name: "file1.mkv", Size: 1000, Progress: 1.0}, // Complete
						{Name: "file2.mkv", Size: 2000, Progress: 0.5}, // Incomplete
					}
					return &files, nil
				}
				sm.getTorrentPropsFunc = func(ctx context.Context, instanceID int, hash string) (*qbt.TorrentProperties, error) {
					return &qbt.TorrentProperties{SavePath: "/downloads"}, nil
				}
			},
			checkResult: func(t *testing.T, prep *PrepareResult) {
				assert.Equal(t, 1, len(prep.Files), "should only include complete files")
				assert.Equal(t, "file1.mkv", prep.Files[0].RelPath)
			},
		},
		{
			name: "success - preserves category and tags",
			transfer: func() *models.Transfer {
				t := newTestTransfer()
				t.PreserveCategory = true
				t.PreserveTags = true
				return t
			}(),
			setupMocks: func(sm *mockSyncManager, ip *mockInstanceProvider) {
				ip.AddInstance(newTestInstance(1, withRemoteAccess()))
				ip.AddInstance(newTestInstance(2, withRemoteAccess()))

				sm.getTorrentsFunc = func(ctx context.Context, instanceID int, filter qbt.TorrentFilterOptions) ([]qbt.Torrent, error) {
					return []qbt.Torrent{{
						Hash:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
						Name:     "Test Torrent",
						Progress: 1.0,
						Category: "movies",
						Tags:     "hd, remux",
					}}, nil
				}
				sm.getTorrentFilesFunc = func(ctx context.Context, instanceID int, hash string) (*qbt.TorrentFiles, error) {
					files := qbt.TorrentFiles{{Name: "movie.mkv", Size: 1000, Progress: 1.0}}
					return &files, nil
				}
				sm.getTorrentPropsFunc = func(ctx context.Context, instanceID int, hash string) (*qbt.TorrentProperties, error) {
					return &qbt.TorrentProperties{SavePath: "/downloads"}, nil
				}
			},
			checkResult: func(t *testing.T, prep *PrepareResult) {
				assert.Equal(t, "movies", prep.Category)
				assert.Equal(t, []string{"hd", "remux"}, prep.Tags)
			},
		},
		{
			name:     "error - path traversal in file name",
			transfer: newTestTransfer(),
			setupMocks: func(sm *mockSyncManager, ip *mockInstanceProvider) {
				ip.AddInstance(newTestInstance(1, withRemoteAccess()))
				ip.AddInstance(newTestInstance(2, withRemoteAccess()))

				sm.getTorrentsFunc = func(ctx context.Context, instanceID int, filter qbt.TorrentFilterOptions) ([]qbt.Torrent, error) {
					return []qbt.Torrent{{
						Hash:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
						Name:     "Test Torrent",
						Progress: 1.0,
					}}, nil
				}
				sm.getTorrentFilesFunc = func(ctx context.Context, instanceID int, hash string) (*qbt.TorrentFiles, error) {
					files := qbt.TorrentFiles{{Name: "../../../etc/passwd", Size: 1000, Progress: 1.0}}
					return &files, nil
				}
				sm.getTorrentPropsFunc = func(ctx context.Context, instanceID int, hash string) (*qbt.TorrentProperties, error) {
					return &qbt.TorrentProperties{SavePath: "/downloads"}, nil
				}
			},
			wantErr: "unsafe file path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := newMockSyncManager()
			ip := newMockInstanceProvider()

			if tt.setupMocks != nil {
				tt.setupMocks(sm, ip)
			}

			executor := NewSSHExecutor(sm, ip, nil, nil)
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

func TestSSHExecutor_determineLinkMode(t *testing.T) {
	tests := []struct {
		name     string
		source   *models.Instance
		target   *models.Instance
		expected string
	}{
		{
			name:     "both remote - transfer mode",
			source:   newTestInstance(1, withRemoteAccess()),
			target:   newTestInstance(2, withRemoteAccess()),
			expected: "transfer",
		},
		{
			name:     "source local, target remote - transfer mode",
			source:   newTestInstance(1, withLocalAccess(true)),
			target:   newTestInstance(2, withRemoteAccess()),
			expected: "transfer",
		},
		{
			name:     "source remote, target local - transfer mode",
			source:   newTestInstance(1, withRemoteAccess()),
			target:   newTestInstance(2, withLocalAccess(true)),
			expected: "transfer",
		},
		{
			name:     "both local with hardlinks enabled - hardlink mode",
			source:   newTestInstance(1, withLocalAccess(true)),
			target:   newTestInstance(2, withLocalAccess(true), withHardlinks(true)),
			expected: "hardlink",
		},
		{
			name:   "both local with reflinks enabled - reflink mode",
			source: newTestInstance(1, withLocalAccess(true)),
			target: func() *models.Instance {
				i := newTestInstance(2, withLocalAccess(true))
				i.UseHardlinks = false // Must disable hardlinks for reflink to be selected
				i.UseReflinks = true
				return i
			}(),
			expected: "reflink",
		},
		{
			name:   "both local with fallback mode - copy mode",
			source: newTestInstance(1, withLocalAccess(true)),
			target: func() *models.Instance {
				i := newTestInstance(2, withLocalAccess(true))
				i.UseHardlinks = false
				i.UseReflinks = false
				i.FallbackToRegularMode = true
				return i
			}(),
			expected: "copy",
		},
		{
			name:   "both local no linking - direct mode",
			source: newTestInstance(1, withLocalAccess(true)),
			target: func() *models.Instance {
				i := newTestInstance(2, withLocalAccess(true))
				i.UseHardlinks = false
				i.UseReflinks = false
				i.FallbackToRegularMode = false
				return i
			}(),
			expected: "direct",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executor := &SSHExecutor{}
			result := executor.determineLinkMode(context.Background(), tt.source, tt.target)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestComputeTargetPathCommon_SSH(t *testing.T) {
	tests := []struct {
		name           string
		sourcePath     string
		targetInstance *models.Instance
		mappings       map[string]string
		expected       string
	}{
		{
			name:           "no mappings - same path",
			sourcePath:     "/downloads/torrents",
			targetInstance: newTestInstance(2),
			mappings:       nil,
			expected:       "/downloads/torrents",
		},
		{
			name:           "explicit mapping - exact match",
			sourcePath:     "/downloads",
			targetInstance: newTestInstance(2),
			mappings:       map[string]string{"/downloads": "/storage"},
			expected:       "/storage",
		},
		{
			name:           "explicit mapping - prefix match",
			sourcePath:     "/downloads/tv/show",
			targetInstance: newTestInstance(2),
			mappings:       map[string]string{"/downloads": "/storage"},
			expected:       "/storage/tv/show",
		},
		{
			name:           "hardlink base dir when set and no mapping match",
			sourcePath:     "/downloads/torrents",
			targetInstance: newTestInstance(2, withHardlinkBaseDir("/links")),
			mappings:       nil,
			expected:       "/links",
		},
		{
			name:           "mapping takes precedence over hardlink base dir",
			sourcePath:     "/downloads/torrents",
			targetInstance: newTestInstance(2, withHardlinkBaseDir("/links")),
			mappings:       map[string]string{"/downloads": "/mapped"},
			expected:       "/mapped/torrents",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := computeTargetPathCommon(tt.sourcePath, tt.targetInstance, tt.mappings)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSSHExecutor_AddTorrent(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name          string
		transfer      *models.Transfer
		prep          *PrepareResult
		checkOptions  func(*testing.T, map[string]string)
		addTorrentErr error
		wantErr       bool
	}{
		{
			name:     "success - basic add",
			transfer: newTestTransfer(),
			prep: &PrepareResult{
				TargetSavePath: "/downloads/target",
				LinkMode:       "transfer",
				TorrentData:    []byte("torrent data"),
			},
			checkOptions: func(t *testing.T, opts map[string]string) {
				assert.Equal(t, "false", opts["autoTMM"])
				assert.Equal(t, "/downloads/target", opts["savepath"])
				assert.Equal(t, "true", opts["skip_checking"])
				assert.Equal(t, "true", opts["paused"])
				assert.Equal(t, "Original", opts["contentLayout"])
			},
			wantErr: false,
		},
		{
			name:     "success - direct mode skips contentLayout",
			transfer: newTestTransfer(),
			prep: &PrepareResult{
				TargetSavePath: "/downloads/target",
				LinkMode:       "direct",
				TorrentData:    []byte("torrent data"),
			},
			checkOptions: func(t *testing.T, opts map[string]string) {
				_, hasContentLayout := opts["contentLayout"]
				assert.False(t, hasContentLayout, "direct mode should not set contentLayout")
			},
			wantErr: false,
		},
		{
			name:     "success - with category and tags",
			transfer: newTestTransfer(),
			prep: &PrepareResult{
				TargetSavePath: "/downloads/target",
				LinkMode:       "transfer",
				TorrentData:    []byte("torrent data"),
				Category:       "movies",
				Tags:           []string{"hd", "remux"},
			},
			checkOptions: func(t *testing.T, opts map[string]string) {
				assert.Equal(t, "movies", opts["category"])
				assert.Equal(t, "hd,remux", opts["tags"])
			},
			wantErr: false,
		},
		{
			name:          "error - add fails",
			transfer:      newTestTransfer(),
			prep:          newTestPrepareResult(),
			addTorrentErr: errors.New("add failed"),
			wantErr:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := newMockSyncManager()
			if tt.addTorrentErr != nil {
				sm.addTorrentFunc = func(ctx context.Context, instanceID int, data []byte, opts map[string]string) error {
					return tt.addTorrentErr
				}
			}

			executor := NewSSHExecutor(sm, nil, nil, nil)
			err := executor.AddTorrent(ctx, tt.transfer, tt.prep)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Len(t, sm.addTorrentCalls, 1)
				if tt.checkOptions != nil {
					tt.checkOptions(t, sm.addTorrentCalls[0].Options)
				}
			}
		})
	}
}

func TestSSHExecutor_DeleteSource(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name            string
		transfer        *models.Transfer
		deleteErr       error
		wantErr         bool
		wantDeleteFiles bool
	}{
		{
			name:            "transfer mode - deletes files (copied over network)",
			transfer:        newTestTransfer(withLinkMode("transfer")),
			wantErr:         false,
			wantDeleteFiles: true,
		},
		{
			name:            "hardlink mode - deletes files (safe via shared inodes)",
			transfer:        newTestTransfer(withLinkMode("hardlink")),
			wantErr:         false,
			wantDeleteFiles: true,
		},
		{
			name:            "reflink mode - deletes files (safe via CoW copies)",
			transfer:        newTestTransfer(withLinkMode("reflink")),
			wantErr:         false,
			wantDeleteFiles: true,
		},
		{
			name:            "copy mode - deletes files (files were duplicated)",
			transfer:        newTestTransfer(withLinkMode("copy")),
			wantErr:         false,
			wantDeleteFiles: true,
		},
		{
			name:            "direct mode - keeps files (shared storage)",
			transfer:        newTestTransfer(withLinkMode("direct")),
			wantErr:         false,
			wantDeleteFiles: false,
		},
		{
			name:      "error - delete fails",
			transfer:  newTestTransfer(),
			deleteErr: errors.New("delete failed"),
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := newMockSyncManager()
			if tt.deleteErr != nil {
				sm.deleteTorrentsFunc = func(ctx context.Context, instanceID int, hashes []string, deleteFiles bool) error {
					return tt.deleteErr
				}
			}

			executor := NewSSHExecutor(sm, nil, nil, nil)
			err := executor.DeleteSource(ctx, tt.transfer)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Len(t, sm.deleteTorrentCalls, 1)
				assert.Equal(t, tt.wantDeleteFiles, sm.deleteTorrentCalls[0].DeleteFiles)
			}
		})
	}
}

func TestSSHExecutor_sshConfigFromConnection(t *testing.T) {
	executor := &SSHExecutor{}

	conn := &models.InstanceConnection{
		Host:           "192.168.1.100",
		Port:           2222,
		Username:       "admin",
		PrivateKeyPath: "/home/user/.ssh/id_rsa",
	}

	cfg := executor.sshConfigFromConnection(conn)

	assert.Equal(t, "192.168.1.100", cfg.Host)
	assert.Equal(t, 2222, cfg.Port)
	assert.Equal(t, "admin", cfg.Username)
	assert.Equal(t, "/home/user/.ssh/id_rsa", cfg.PrivateKeyPath)
}

// Note: CreateLinks tests require actual SSH connections and are covered by E2E tests.
// The connectionStore is accessed before checking link mode, so unit tests would need
// a mock database setup which adds complexity. E2E tests in scripts/testing/run-test.sh
// provide better coverage for these scenarios.

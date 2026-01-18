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

func TestLocalExecutor_CanHandle(t *testing.T) {
	executor := NewLocalExecutor(nil, nil)

	tests := []struct {
		name        string
		sourceLocal bool
		targetLocal bool
		expected    bool
	}{
		{
			name:        "both local - can handle",
			sourceLocal: true,
			targetLocal: true,
			expected:    true,
		},
		{
			name:        "source remote - cannot handle",
			sourceLocal: false,
			targetLocal: true,
			expected:    false,
		},
		{
			name:        "target remote - cannot handle",
			sourceLocal: true,
			targetLocal: false,
			expected:    false,
		},
		{
			name:        "both remote - cannot handle",
			sourceLocal: false,
			targetLocal: false,
			expected:    false,
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

func TestLocalExecutor_Prepare(t *testing.T) {
	ctx := context.Background()

	// Note: Full Prepare tests require filesystem access for determineLinkMode.
	// These tests focus on validation and early exit conditions.
	// Link mode determination is tested separately or via integration tests.

	tests := []struct {
		name        string
		transfer    *models.Transfer
		setupMocks  func(*mockSyncManager, *mockInstanceProvider)
		wantErr     error
		checkResult func(*testing.T, *PrepareResult)
	}{
		{
			name:     "error - source not accessible",
			transfer: newTestTransfer(),
			setupMocks: func(sm *mockSyncManager, ip *mockInstanceProvider) {
				ip.AddInstance(newTestInstance(1, withLocalAccess(false)))
				ip.AddInstance(newTestInstance(2, withLocalAccess(true)))
			},
			wantErr: ErrSourceNotAccessible,
		},
		{
			name:     "error - target not accessible",
			transfer: newTestTransfer(),
			setupMocks: func(sm *mockSyncManager, ip *mockInstanceProvider) {
				ip.AddInstance(newTestInstance(1, withLocalAccess(true)))
				ip.AddInstance(newTestInstance(2, withLocalAccess(false)))
			},
			wantErr: ErrTargetNotAccessible,
		},
		{
			name:     "error - torrent not found",
			transfer: newTestTransfer(),
			setupMocks: func(sm *mockSyncManager, ip *mockInstanceProvider) {
				ip.AddInstance(newTestInstance(1, withLocalAccess(true)))
				ip.AddInstance(newTestInstance(2, withLocalAccess(true)))

				sm.getTorrentsFunc = func(ctx context.Context, instanceID int, filter qbt.TorrentFilterOptions) ([]qbt.Torrent, error) {
					return []qbt.Torrent{}, nil // Empty - no torrent found
				}
			},
			wantErr: ErrTorrentNotFound,
		},
		{
			name:     "error - no complete files",
			transfer: newTestTransfer(),
			setupMocks: func(sm *mockSyncManager, ip *mockInstanceProvider) {
				ip.AddInstance(newTestInstance(1, withLocalAccess(true)))
				ip.AddInstance(newTestInstance(2, withLocalAccess(true)))

				sm.getTorrentsFunc = func(ctx context.Context, instanceID int, filter qbt.TorrentFilterOptions) ([]qbt.Torrent, error) {
					return []qbt.Torrent{{
						Hash:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
						Name:     "Test Torrent",
						Progress: 0.5, // Only 50% complete
					}}, nil
				}
				sm.getTorrentFilesFunc = func(ctx context.Context, instanceID int, hash string) (*qbt.TorrentFiles, error) {
					files := qbt.TorrentFiles{{Name: "file1.mkv", Size: 1000, Progress: 0.5}} // File not complete
					return &files, nil
				}
				sm.getTorrentPropsFunc = func(ctx context.Context, instanceID int, hash string) (*qbt.TorrentProperties, error) {
					return &qbt.TorrentProperties{SavePath: "/data/downloads"}, nil
				}
			},
			wantErr: errors.New("no complete files to transfer"),
		},
		{
			name:     "error - get files fails",
			transfer: newTestTransfer(),
			setupMocks: func(sm *mockSyncManager, ip *mockInstanceProvider) {
				ip.AddInstance(newTestInstance(1, withLocalAccess(true)))
				ip.AddInstance(newTestInstance(2, withLocalAccess(true)))

				sm.getTorrentsFunc = func(ctx context.Context, instanceID int, filter qbt.TorrentFilterOptions) ([]qbt.Torrent, error) {
					return []qbt.Torrent{{
						Hash:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
						Name:     "Test Torrent",
						Progress: 1.0,
					}}, nil
				}
				sm.getTorrentFilesFunc = func(ctx context.Context, instanceID int, hash string) (*qbt.TorrentFiles, error) {
					return nil, errors.New("failed to get files")
				}
			},
			wantErr: errors.New("failed to get source files"),
		},
		{
			name:     "error - get properties fails",
			transfer: newTestTransfer(),
			setupMocks: func(sm *mockSyncManager, ip *mockInstanceProvider) {
				ip.AddInstance(newTestInstance(1, withLocalAccess(true)))
				ip.AddInstance(newTestInstance(2, withLocalAccess(true)))

				sm.getTorrentsFunc = func(ctx context.Context, instanceID int, filter qbt.TorrentFilterOptions) ([]qbt.Torrent, error) {
					return []qbt.Torrent{{
						Hash:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
						Name:     "Test Torrent",
						Progress: 1.0,
					}}, nil
				}
				sm.getTorrentFilesFunc = func(ctx context.Context, instanceID int, hash string) (*qbt.TorrentFiles, error) {
					files := qbt.TorrentFiles{{Name: "file1.mkv", Size: 1000}}
					return &files, nil
				}
				sm.getTorrentPropsFunc = func(ctx context.Context, instanceID int, hash string) (*qbt.TorrentProperties, error) {
					return nil, errors.New("failed to get properties")
				}
			},
			wantErr: errors.New("failed to get source properties"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := newMockSyncManager()
			ip := newMockInstanceProvider()

			if tt.setupMocks != nil {
				tt.setupMocks(sm, ip)
			}

			executor := NewLocalExecutor(sm, ip)
			result, err := executor.Prepare(ctx, tt.transfer)

			if tt.wantErr != nil {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr.Error())
			} else {
				require.NoError(t, err)
				if tt.checkResult != nil {
					tt.checkResult(t, result)
				}
			}
		})
	}
}

func Test_computeTargetPathCommon(t *testing.T) {
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
			name:           "longest prefix match wins",
			sourcePath:     "/downloads/tv/show",
			targetInstance: newTestInstance(2),
			mappings: map[string]string{
				"/downloads":    "/storage",
				"/downloads/tv": "/tv-storage",
			},
			expected: "/tv-storage/show",
		},
		{
			name:           "hardlink base dir when set",
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
		{
			name:           "no partial path component match",
			sourcePath:     "/downloads-extra/torrents",
			targetInstance: newTestInstance(2),
			mappings:       map[string]string{"/downloads": "/storage"},
			expected:       "/downloads-extra/torrents", // Should NOT match because "downloads-extra" != "downloads"
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := computeTargetPathCommon(tt.sourcePath, tt.targetInstance, tt.mappings)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestLocalExecutor_buildDestDir(t *testing.T) {
	executor := &LocalExecutor{}

	tests := []struct {
		name     string
		transfer *models.Transfer
		prep     *PrepareResult
		expected string
	}{
		{
			name:     "default - uses target save path",
			transfer: newTestTransfer(),
			prep: &PrepareResult{
				TorrentName:    "Test Torrent",
				TargetSavePath: "/downloads/target",
				TargetInstance: newTestInstance(2),
			},
			expected: "/downloads/target",
		},
		{
			name:     "uses hardlink base dir when set",
			transfer: newTestTransfer(),
			prep: &PrepareResult{
				TorrentName:    "Test Torrent",
				TargetSavePath: "/downloads/target",
				TargetInstance: newTestInstance(2, withHardlinkBaseDir("/links")),
			},
			expected: "/links",
		},
		{
			name:     "flat preset",
			transfer: newTestTransfer(),
			prep: &PrepareResult{
				TorrentName:    "Test Torrent",
				TargetSavePath: "/downloads/target",
				TargetInstance: func() *models.Instance {
					i := newTestInstance(2, withHardlinkBaseDir("/links"))
					i.HardlinkDirPreset = "flat"
					return i
				}(),
			},
			expected: "/links/Test Torrent--aaaaaaaa",
		},
		{
			name:     "by-instance preset",
			transfer: newTestTransfer(),
			prep: &PrepareResult{
				TorrentName:    "Test Torrent",
				TargetSavePath: "/downloads/target",
				TargetInstance: func() *models.Instance {
					i := newTestInstance(2, withHardlinkBaseDir("/links"))
					i.HardlinkDirPreset = "by-instance"
					return i
				}(),
			},
			expected: "/links/instance-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := executor.buildDestDir(tt.transfer, tt.prep)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestLocalExecutor_AddTorrent(t *testing.T) {
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
				LinkMode:       "hardlink",
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
			name:     "success - with category",
			transfer: newTestTransfer(),
			prep: &PrepareResult{
				TargetSavePath: "/downloads/target",
				LinkMode:       "hardlink",
				TorrentData:    []byte("torrent data"),
				Category:       "movies",
			},
			checkOptions: func(t *testing.T, opts map[string]string) {
				assert.Equal(t, "movies", opts["category"])
			},
			wantErr: false,
		},
		{
			name:     "success - with tags",
			transfer: newTestTransfer(),
			prep: &PrepareResult{
				TargetSavePath: "/downloads/target",
				LinkMode:       "hardlink",
				TorrentData:    []byte("torrent data"),
				Tags:           []string{"tag1", "tag2"},
			},
			checkOptions: func(t *testing.T, opts map[string]string) {
				assert.Equal(t, "tag1,tag2", opts["tags"])
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

			executor := NewLocalExecutor(sm, nil)
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

func TestLocalExecutor_DeleteSource(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name            string
		transfer        *models.Transfer
		deleteErr       error
		wantErr         bool
		wantDeleteFiles bool
	}{
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

			executor := NewLocalExecutor(sm, nil)
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

func TestSanitizeName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"normal name", "normal name"},
		{"name/with/slashes", "name_with_slashes"},
		{"name\\with\\backslashes", "name_with_backslashes"},
		{"name:with:colons", "name_with_colons"},
		{"name*with*asterisks", "name_with_asterisks"},
		{"name?with?questions", "name_with_questions"},
		{"name\"with\"quotes", "name_with_quotes"},
		{"name<with>brackets", "name_with_brackets"},
		{"name|with|pipes", "name_with_pipes"},
		{"all/\\:*?\"<>|chars", "all_________chars"}, // 9 special chars = 9 underscores
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := sanitizeName(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

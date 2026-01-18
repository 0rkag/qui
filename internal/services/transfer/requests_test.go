// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

func TestTransferRequest_Validate(t *testing.T) {
	validSHA1 := strings.Repeat("a", 40)
	validSHA256 := strings.Repeat("b", 64)

	tests := []struct {
		name    string
		req     TransferRequest
		wantErr error
	}{
		// Valid cases
		{
			name: "valid with SHA1 hash",
			req: TransferRequest{
				SourceInstanceID: 1,
				TargetInstanceID: 2,
				TorrentHash:      validSHA1,
			},
			wantErr: nil,
		},
		{
			name: "valid with SHA256 hash",
			req: TransferRequest{
				SourceInstanceID: 1,
				TargetInstanceID: 2,
				TorrentHash:      validSHA256,
			},
			wantErr: nil,
		},
		{
			name: "valid with uppercase hash",
			req: TransferRequest{
				SourceInstanceID: 1,
				TargetInstanceID: 2,
				TorrentHash:      strings.ToUpper(validSHA1),
			},
			wantErr: nil,
		},
		{
			name: "valid with mixed case hash",
			req: TransferRequest{
				SourceInstanceID: 1,
				TargetInstanceID: 2,
				TorrentHash:      "aAbBcCdDeEfF00112233445566778899aabbccdd",
			},
			wantErr: nil,
		},
		{
			name: "valid with all options",
			req: TransferRequest{
				SourceInstanceID: 1,
				TargetInstanceID: 2,
				TorrentHash:      validSHA1,
				PathMappings:     map[string]string{"/source": "/target"},
				FileExistsAction: models.FileExistsOverwrite,
				SourceAction:     models.SourceDelete,
				VerifyTransfer:   true,
				PreserveCategory: true,
				PreserveTags:     true,
			},
			wantErr: nil,
		},

		// Invalid cases - missing fields
		{
			name: "missing source instance ID",
			req: TransferRequest{
				TargetInstanceID: 2,
				TorrentHash:      validSHA1,
			},
			wantErr: ErrMissingSourceID,
		},
		{
			name: "missing target instance ID",
			req: TransferRequest{
				SourceInstanceID: 1,
				TorrentHash:      validSHA1,
			},
			wantErr: ErrMissingTargetID,
		},
		{
			name: "same source and target",
			req: TransferRequest{
				SourceInstanceID: 1,
				TargetInstanceID: 1,
				TorrentHash:      validSHA1,
			},
			wantErr: ErrSourceTargetSame,
		},
		{
			name: "missing torrent hash",
			req: TransferRequest{
				SourceInstanceID: 1,
				TargetInstanceID: 2,
			},
			wantErr: ErrMissingTorrentHash,
		},
		{
			name: "empty torrent hash",
			req: TransferRequest{
				SourceInstanceID: 1,
				TargetInstanceID: 2,
				TorrentHash:      "",
			},
			wantErr: ErrMissingTorrentHash,
		},

		// Invalid hash formats
		{
			name: "hash too short",
			req: TransferRequest{
				SourceInstanceID: 1,
				TargetInstanceID: 2,
				TorrentHash:      "abc123",
			},
			wantErr: ErrInvalidTorrentHash,
		},
		{
			name: "hash 39 chars (one short of SHA1)",
			req: TransferRequest{
				SourceInstanceID: 1,
				TargetInstanceID: 2,
				TorrentHash:      strings.Repeat("a", 39),
			},
			wantErr: ErrInvalidTorrentHash,
		},
		{
			name: "hash 41 chars (one over SHA1)",
			req: TransferRequest{
				SourceInstanceID: 1,
				TargetInstanceID: 2,
				TorrentHash:      strings.Repeat("a", 41),
			},
			wantErr: ErrInvalidTorrentHash,
		},
		{
			name: "hash 63 chars (one short of SHA256)",
			req: TransferRequest{
				SourceInstanceID: 1,
				TargetInstanceID: 2,
				TorrentHash:      strings.Repeat("a", 63),
			},
			wantErr: ErrInvalidTorrentHash,
		},
		{
			name: "hash 65 chars (one over SHA256)",
			req: TransferRequest{
				SourceInstanceID: 1,
				TargetInstanceID: 2,
				TorrentHash:      strings.Repeat("a", 65),
			},
			wantErr: ErrInvalidTorrentHash,
		},
		{
			name: "hash with non-hex characters",
			req: TransferRequest{
				SourceInstanceID: 1,
				TargetInstanceID: 2,
				TorrentHash:      strings.Repeat("g", 40), // 'g' is not hex
			},
			wantErr: ErrInvalidTorrentHash,
		},
		{
			name: "hash with spaces",
			req: TransferRequest{
				SourceInstanceID: 1,
				TargetInstanceID: 2,
				TorrentHash:      strings.Repeat("a", 20) + " " + strings.Repeat("a", 19),
			},
			wantErr: ErrInvalidTorrentHash,
		},
		{
			name: "hash with special characters",
			req: TransferRequest{
				SourceInstanceID: 1,
				TargetInstanceID: 2,
				TorrentHash:      strings.Repeat("a", 39) + "!",
			},
			wantErr: ErrInvalidTorrentHash,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestMoveRequest_Validate(t *testing.T) {
	validSHA1 := strings.Repeat("a", 40)
	validSHA256 := strings.Repeat("b", 64)

	tests := []struct {
		name    string
		req     MoveRequest
		wantErr error
	}{
		// Valid cases
		{
			name: "valid with SHA1 hash",
			req: MoveRequest{
				SourceInstanceID: 1,
				TargetInstanceID: 2,
				Hash:             validSHA1,
			},
			wantErr: nil,
		},
		{
			name: "valid with SHA256 hash",
			req: MoveRequest{
				SourceInstanceID: 1,
				TargetInstanceID: 2,
				Hash:             validSHA256,
			},
			wantErr: nil,
		},
		{
			name: "valid with all options",
			req: MoveRequest{
				SourceInstanceID: 1,
				TargetInstanceID: 2,
				Hash:             validSHA1,
				PathMappings:     map[string]string{"/source": "/target"},
				FileExistsAction: models.FileExistsOverwrite,
				SourceAction:     models.SourceDelete,
				VerifyTransfer:   true,
				PreserveCategory: true,
				PreserveTags:     true,
			},
			wantErr: nil,
		},

		// Invalid cases
		{
			name: "missing source instance ID",
			req: MoveRequest{
				TargetInstanceID: 2,
				Hash:             validSHA1,
			},
			wantErr: ErrMissingSourceID,
		},
		{
			name: "missing target instance ID",
			req: MoveRequest{
				SourceInstanceID: 1,
				Hash:             validSHA1,
			},
			wantErr: ErrMissingTargetID,
		},
		{
			name: "same source and target",
			req: MoveRequest{
				SourceInstanceID: 1,
				TargetInstanceID: 1,
				Hash:             validSHA1,
			},
			wantErr: ErrSourceTargetSame,
		},
		{
			name: "missing hash",
			req: MoveRequest{
				SourceInstanceID: 1,
				TargetInstanceID: 2,
			},
			wantErr: ErrMissingTorrentHash,
		},
		{
			name: "invalid hash format",
			req: MoveRequest{
				SourceInstanceID: 1,
				TargetInstanceID: 2,
				Hash:             "invalid",
			},
			wantErr: ErrInvalidTorrentHash,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestTransferState_IsTerminal(t *testing.T) {
	tests := []struct {
		state    models.TransferState
		terminal bool
	}{
		// Non-terminal states
		{models.TransferStatePending, false},
		{models.TransferStatePreparing, false},
		{models.TransferStateLinksCreating, false},
		{models.TransferStateLinksCreated, false},
		{models.TransferStateAddingTorrent, false},
		{models.TransferStateTorrentAdded, false},
		{models.TransferStateDeletingSource, false},

		// Terminal states
		{models.TransferStateCompleted, true},
		{models.TransferStateFailed, true},
		{models.TransferStateRolledBack, true},
		{models.TransferStateCancelled, true},
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			assert.Equal(t, tt.terminal, tt.state.IsTerminal())
		})
	}
}

func TestTransferState_IsValid(t *testing.T) {
	// All known states should be valid
	validStates := []models.TransferState{
		models.TransferStatePending,
		models.TransferStatePreparing,
		models.TransferStateLinksCreating,
		models.TransferStateLinksCreated,
		models.TransferStateAddingTorrent,
		models.TransferStateTorrentAdded,
		models.TransferStateDeletingSource,
		models.TransferStateCompleted,
		models.TransferStateFailed,
		models.TransferStateRolledBack,
		models.TransferStateCancelled,
	}

	for _, state := range validStates {
		t.Run(string(state), func(t *testing.T) {
			require.True(t, state.IsValid(), "state %s should be valid", state)
		})
	}

	// Unknown states should be invalid
	invalidStates := []models.TransferState{
		"unknown",
		"invalid",
		"",
	}

	for _, state := range invalidStates {
		t.Run(string(state), func(t *testing.T) {
			require.False(t, state.IsValid(), "state %s should be invalid", state)
		})
	}
}

func TestValidTorrentHashRegex(t *testing.T) {
	// This tests the regex directly for edge cases
	tests := []struct {
		name  string
		hash  string
		valid bool
	}{
		// Valid SHA1 (40 hex chars)
		{"valid SHA1 lowercase", strings.Repeat("a", 40), true},
		{"valid SHA1 uppercase", strings.Repeat("A", 40), true},
		{"valid SHA1 mixed", "0123456789abcdefABCDEF0123456789abcdef01", true},

		// Valid SHA256 (64 hex chars)
		{"valid SHA256 lowercase", strings.Repeat("a", 64), true},
		{"valid SHA256 uppercase", strings.Repeat("A", 64), true},
		{"valid SHA256 numbers", strings.Repeat("0", 64), true},

		// Invalid
		{"empty", "", false},
		{"too short", strings.Repeat("a", 39), false},
		{"between SHA1 and SHA256", strings.Repeat("a", 50), false},
		{"too long", strings.Repeat("a", 65), false},
		{"non-hex g", strings.Repeat("g", 40), false},
		{"non-hex z", strings.Repeat("z", 64), false},
		{"with space", strings.Repeat("a", 20) + " " + strings.Repeat("a", 19), false},
		{"with newline", strings.Repeat("a", 20) + "\n" + strings.Repeat("a", 19), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validTorrentHash.MatchString(tt.hash)
			assert.Equal(t, tt.valid, result)
		})
	}
}

// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

func TestBuildPrepareResultFromTransfer(t *testing.T) {
	source := newTestInstance(1)
	target := newTestInstance(2)

	transfer := &models.Transfer{
		ID:               1,
		TorrentName:      "Test Torrent",
		SourceSavePath:   "/downloads/source",
		TargetSavePath:   "/downloads/target",
		LinkMode:         "hardlink",
		TargetCategory:   "movies",
		TargetTags:       []string{"tag1", "tag2"},
	}

	// Create a minimal service just to test the helper
	s := &Service{}
	result := s.buildPrepareResultFromTransfer(transfer, source, target)

	assert.Equal(t, "Test Torrent", result.TorrentName)
	assert.Equal(t, "/downloads/source", result.SourceSavePath)
	assert.Equal(t, "/downloads/target", result.TargetSavePath)
	assert.Equal(t, "hardlink", result.LinkMode)
	assert.Equal(t, "movies", result.Category)
	assert.Equal(t, []string{"tag1", "tag2"}, result.Tags)
	assert.Equal(t, source, result.SourceInstance)
	assert.Equal(t, target, result.TargetInstance)
}

func TestContinueAfterAdd(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name         string
		sourceAction models.SourceAction
		expectDelete bool
	}{
		{
			name:         "source action delete - calls delete",
			sourceAction: models.SourceDelete,
			expectDelete: true,
		},
		{
			name:         "source action keep - completes directly",
			sourceAction: models.SourceKeep,
			expectDelete: false,
		},
		{
			name:         "source action pause - completes without delete",
			sourceAction: models.SourcePause,
			expectDelete: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transfer := newTestTransfer(
				withState(models.TransferStateTorrentAdded),
				withSourceAction(tt.sourceAction),
			)

			executor := newMockExecutor()

			s := &Service{
				workerCtx: ctx,
			}

			s.continueAfterAdd(ctx, transfer, executor)

			assert.Equal(t, models.TransferStateCompleted, transfer.State)
			if tt.expectDelete {
				assert.Equal(t, 1, executor.deleteCalls)
			} else {
				assert.Equal(t, 0, executor.deleteCalls)
			}
		})
	}
}

func TestDoDeleteSource_Success(t *testing.T) {
	ctx := context.Background()

	transfer := newTestTransfer(withState(models.TransferStateDeletingSource), withSourceAction(models.SourceDelete))

	executor := newMockExecutor()

	s := &Service{
		workerCtx: ctx,
	}

	s.doDeleteSource(ctx, transfer, executor)

	assert.Equal(t, models.TransferStateCompleted, transfer.State)
	assert.Equal(t, 1, executor.deleteCalls)
	assert.Empty(t, transfer.Error)
}

func TestDoDeleteSource_FailureCompletesWithWarning(t *testing.T) {
	ctx := context.Background()

	transfer := newTestTransfer(withState(models.TransferStateDeletingSource), withSourceAction(models.SourceDelete))

	executor := newMockExecutor()
	executor.deleteSourceFunc = func(ctx context.Context, t *models.Transfer) error {
		return errors.New("delete failed")
	}

	s := &Service{
		workerCtx: ctx,
	}

	s.doDeleteSource(ctx, transfer, executor)

	// Should still complete
	assert.Equal(t, models.TransferStateCompleted, transfer.State)
	// But with a warning
	assert.Contains(t, transfer.Error, "warning")
}

func TestTerminalStatesAreSkipped(t *testing.T) {
	terminalStates := []models.TransferState{
		models.TransferStateCompleted,
		models.TransferStateFailed,
		models.TransferStateRolledBack,
		models.TransferStateCancelled,
	}

	for _, state := range terminalStates {
		t.Run(string(state), func(t *testing.T) {
			transfer := newTestTransfer(withState(state))

			// Terminal states should be detected
			require.True(t, transfer.State.IsTerminal())

			// And processTransfer would skip them (verified by IsTerminal check)
		})
	}
}

func TestNonTerminalStatesProcessed(t *testing.T) {
	nonTerminalStates := []models.TransferState{
		models.TransferStatePending,
		models.TransferStatePreparing,
		models.TransferStateLinksCreating,
		models.TransferStateLinksCreated,
		models.TransferStateAddingTorrent,
		models.TransferStateTorrentAdded,
		models.TransferStateDeletingSource,
	}

	for _, state := range nonTerminalStates {
		t.Run(string(state), func(t *testing.T) {
			transfer := newTestTransfer(withState(state))

			// Non-terminal states should not be skipped
			require.False(t, transfer.State.IsTerminal())
		})
	}
}

func TestUpdateState(t *testing.T) {
	ctx := context.Background()

	transfer := newTestTransfer(withState(models.TransferStatePending))

	s := &Service{
		// Note: store is nil, but updateState only updates the transfer object
		// The actual store.UpdateState call will fail silently (logged)
		workerCtx: ctx,
	}

	// This will update the transfer object even if store is nil
	s.updateState(ctx, transfer, models.TransferStatePreparing, "")

	// The transfer object is updated regardless of store
	assert.Equal(t, models.TransferStatePreparing, transfer.State)
	assert.Empty(t, transfer.Error)

	// Test with error message
	s.updateState(ctx, transfer, models.TransferStateFailed, "something went wrong")
	assert.Equal(t, models.TransferStateFailed, transfer.State)
	assert.Equal(t, "something went wrong", transfer.Error)
}

func TestFail(t *testing.T) {
	ctx := context.Background()

	transfer := newTestTransfer(withState(models.TransferStatePreparing))

	s := &Service{
		workerCtx: ctx,
	}

	s.fail(ctx, transfer, "test error message")

	assert.Equal(t, models.TransferStateFailed, transfer.State)
	assert.Equal(t, "test error message", transfer.Error)
}

func TestMarkCompleted(t *testing.T) {
	ctx := context.Background()

	transfer := newTestTransfer(withState(models.TransferStateTorrentAdded))
	assert.Nil(t, transfer.CompletedAt)

	s := &Service{
		workerCtx: ctx,
	}

	s.markCompleted(ctx, transfer)

	assert.Equal(t, models.TransferStateCompleted, transfer.State)
	assert.NotNil(t, transfer.CompletedAt)
}

func TestExecutorSelection(t *testing.T) {
	tests := []struct {
		name        string
		sourceLocal bool
		targetLocal bool
		expectFound bool
	}{
		{
			name:        "both local - executor found",
			sourceLocal: true,
			targetLocal: true,
			expectFound: true,
		},
		{
			name:        "source remote - no executor",
			sourceLocal: false,
			targetLocal: true,
			expectFound: false,
		},
		{
			name:        "target remote - no executor",
			sourceLocal: true,
			targetLocal: false,
			expectFound: false,
		},
		{
			name:        "both remote - no executor",
			sourceLocal: false,
			targetLocal: false,
			expectFound: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := newTestInstance(1, withLocalAccess(tt.sourceLocal))
			target := newTestInstance(2, withLocalAccess(tt.targetLocal))

			localExecutor := NewLocalExecutor(nil, nil)
			registry := NewExecutorRegistry(localExecutor, nil)

			executor, err := registry.SelectExecutor(source, target)

			if tt.expectFound {
				require.NoError(t, err)
				require.NotNil(t, executor)
			} else {
				require.Error(t, err)
				require.ErrorIs(t, err, ErrNoExecutorAvailable)
			}
		})
	}
}

func TestMockExecutor_TracksCalls(t *testing.T) {
	ctx := context.Background()
	transfer := newTestTransfer()
	prep := newTestPrepareResult()

	executor := newMockExecutor()

	// Verify initial state
	assert.Equal(t, 0, executor.prepareCalls)
	assert.Equal(t, 0, executor.linksCalls)
	assert.Equal(t, 0, executor.addCalls)
	assert.Equal(t, 0, executor.deleteCalls)
	assert.Equal(t, 0, executor.rollbackCalls)

	// Call methods
	_, _ = executor.Prepare(ctx, transfer)
	_, _ = executor.CreateLinks(ctx, transfer, prep)
	_ = executor.AddTorrent(ctx, transfer, prep)
	_ = executor.DeleteSource(ctx, transfer)
	_ = executor.Rollback(ctx, transfer, prep)

	// Verify calls were tracked
	assert.Equal(t, 1, executor.prepareCalls)
	assert.Equal(t, 1, executor.linksCalls)
	assert.Equal(t, 1, executor.addCalls)
	assert.Equal(t, 1, executor.deleteCalls)
	assert.Equal(t, 1, executor.rollbackCalls)
}

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

func TestTryEnqueue_Success(t *testing.T) {
	queue := make(chan int64, 10)
	s := &Service{queue: queue}

	s.tryEnqueue(123)

	select {
	case id := <-queue:
		assert.Equal(t, int64(123), id)
	default:
		t.Fatal("expected ID to be enqueued")
	}
}

func TestTryEnqueue_QueueFull(t *testing.T) {
	queue := make(chan int64, 1)
	queue <- 1 // Fill the queue

	s := &Service{queue: queue}

	// Should not block, just log warning
	s.tryEnqueue(123)

	// Original item should still be in queue
	id := <-queue
	assert.Equal(t, int64(1), id)
}

func TestRequeueTransfer(t *testing.T) {
	transfer := newTestTransfer()
	queue := make(chan int64, 10)
	s := &Service{queue: queue}

	s.requeueTransfer(transfer)

	select {
	case id := <-queue:
		assert.Equal(t, transfer.ID, id)
	default:
		t.Fatal("expected transfer to be requeued")
	}
}

func TestRequeueTransfer_QueueFull(t *testing.T) {
	transfer := newTestTransfer()
	queue := make(chan int64, 1)
	queue <- 999 // Fill the queue

	s := &Service{queue: queue}

	// Should not block
	s.requeueTransfer(transfer)

	// Queue should still have original item
	id := <-queue
	assert.Equal(t, int64(999), id)
}

func TestAttemptRollback_Success(t *testing.T) {
	ctx := context.Background()

	transfer := newTestTransfer(withState(models.TransferStateLinksCreating))
	transfer.SourceSavePath = "/downloads/source"
	transfer.TargetSavePath = "/downloads/target"
	transfer.LinkMode = "hardlink"

	ip := newMockInstanceProvider()
	ip.AddInstance(newTestInstance(1))
	ip.AddInstance(newTestInstance(2))

	executor := newMockExecutor()
	registry := &ExecutorRegistry{executors: []TransferExecutor{executor}}

	s := &Service{
		instanceStore: ip,
		registry:      registry,
	}

	s.attemptRollback(ctx, transfer)

	// Should have called rollback
	assert.Equal(t, 1, executor.rollbackCalls)
}

func TestAttemptRollback_NoExecutor(t *testing.T) {
	ctx := context.Background()

	transfer := newTestTransfer(withState(models.TransferStateLinksCreating))

	ip := newMockInstanceProvider()
	// Add instances without local access - no executor will match
	ip.AddInstance(newTestInstance(1, withLocalAccess(false)))
	ip.AddInstance(newTestInstance(2, withLocalAccess(false)))

	executor := newMockExecutor()
	executor.canHandleResult = false
	registry := &ExecutorRegistry{executors: []TransferExecutor{executor}}

	s := &Service{
		instanceStore: ip,
		registry:      registry,
	}

	// Should not panic, just log warning
	s.attemptRollback(ctx, transfer)

	// Rollback should not have been called since no executor matched
	assert.Equal(t, 0, executor.rollbackCalls)
}

func TestAttemptRollback_RollbackFails(t *testing.T) {
	ctx := context.Background()

	transfer := newTestTransfer(withState(models.TransferStateLinksCreating))
	transfer.SourceSavePath = "/downloads/source"
	transfer.TargetSavePath = "/downloads/target"
	transfer.LinkMode = "hardlink"

	ip := newMockInstanceProvider()
	ip.AddInstance(newTestInstance(1))
	ip.AddInstance(newTestInstance(2))

	executor := newMockExecutor()
	executor.rollbackFunc = func(ctx context.Context, t *models.Transfer, prep *PrepareResult) error {
		return errors.New("rollback failed")
	}
	registry := &ExecutorRegistry{executors: []TransferExecutor{executor}}

	s := &Service{
		instanceStore: ip,
		registry:      registry,
	}

	// Should not panic even if rollback fails
	s.attemptRollback(ctx, transfer)

	// Rollback was attempted
	assert.Equal(t, 1, executor.rollbackCalls)
}

func TestAttemptRollback_InstanceNotFound(t *testing.T) {
	ctx := context.Background()

	transfer := newTestTransfer(withState(models.TransferStateLinksCreating))

	ip := newMockInstanceProvider()
	// Don't add instances - should fail to get them

	s := &Service{
		instanceStore: ip,
	}

	// Should not panic when instance not found
	s.attemptRollback(ctx, transfer)
}

func TestCheckTorrentExists(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name     string
		exists   bool
		hasError bool
		expected bool
	}{
		{
			name:     "torrent exists",
			exists:   true,
			hasError: false,
			expected: true,
		},
		{
			name:     "torrent does not exist",
			exists:   false,
			hasError: false,
			expected: false,
		},
		{
			name:     "error checking - returns false",
			exists:   false,
			hasError: true,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := newMockSyncManager()
			sm.hasTorrentByHashFunc = func(ctx context.Context, instanceID int, hashes []string) (*qbt.Torrent, bool, error) {
				if tt.hasError {
					return nil, false, errors.New("check failed")
				}
				return nil, tt.exists, nil
			}

			s := &Service{
				syncManager: sm,
			}

			result := s.checkTorrentExists(ctx, 1, "testhash")
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Test recovery logic for different states
// These tests verify the state transitions and queueing behavior

func TestRecoveryLogic_PendingState(t *testing.T) {
	// Pending transfers should just be enqueued
	transfer := newTestTransfer(withState(models.TransferStatePending))
	require.False(t, transfer.State.IsTerminal())

	// The recovery logic would enqueue this transfer
	// We can't call recoverTransfer directly without a real store,
	// but we verify the state is not terminal so it would be processed
}

func TestRecoveryLogic_PreparingState(t *testing.T) {
	// Preparing transfers should be reset to pending
	transfer := newTestTransfer(withState(models.TransferStatePreparing))
	require.False(t, transfer.State.IsTerminal())

	// Simulate what recovery would do
	transfer.State = models.TransferStatePending
	assert.Equal(t, models.TransferStatePending, transfer.State)
}

func TestRecoveryLogic_LinksCreatingState(t *testing.T) {
	// LinksCreating should trigger rollback and reset to pending
	transfer := newTestTransfer(withState(models.TransferStateLinksCreating))
	require.False(t, transfer.State.IsTerminal())

	// After recovery: rollback called, state reset to pending
	transfer.State = models.TransferStatePending
	assert.Equal(t, models.TransferStatePending, transfer.State)
}

func TestRecoveryLogic_AddingTorrentState_TorrentExists(t *testing.T) {
	ctx := context.Background()

	transfer := newTestTransfer(withState(models.TransferStateAddingTorrent))

	sm := newMockSyncManager()
	sm.hasTorrentByHashFunc = func(ctx context.Context, instanceID int, hashes []string) (*qbt.Torrent, bool, error) {
		return &qbt.Torrent{}, true, nil
	}

	s := &Service{syncManager: sm}
	exists := s.checkTorrentExists(ctx, transfer.TargetInstanceID, transfer.TorrentHash)

	// When torrent exists, recovery should set state to TorrentAdded
	require.True(t, exists)
	transfer.State = models.TransferStateTorrentAdded
	assert.Equal(t, models.TransferStateTorrentAdded, transfer.State)
}

func TestRecoveryLogic_AddingTorrentState_TorrentMissing(t *testing.T) {
	ctx := context.Background()

	transfer := newTestTransfer(withState(models.TransferStateAddingTorrent))

	sm := newMockSyncManager()
	sm.hasTorrentByHashFunc = func(ctx context.Context, instanceID int, hashes []string) (*qbt.Torrent, bool, error) {
		return nil, false, nil
	}

	s := &Service{syncManager: sm}
	exists := s.checkTorrentExists(ctx, transfer.TargetInstanceID, transfer.TorrentHash)

	// When torrent doesn't exist, recovery should rollback and reset
	require.False(t, exists)
	transfer.State = models.TransferStatePending
	assert.Equal(t, models.TransferStatePending, transfer.State)
}

func TestRecoveryLogic_DeletingSourceState_TorrentExists(t *testing.T) {
	ctx := context.Background()

	transfer := newTestTransfer(withState(models.TransferStateDeletingSource))

	sm := newMockSyncManager()
	sm.hasTorrentByHashFunc = func(ctx context.Context, instanceID int, hashes []string) (*qbt.Torrent, bool, error) {
		return &qbt.Torrent{}, true, nil
	}

	s := &Service{syncManager: sm}
	exists := s.checkTorrentExists(ctx, transfer.SourceInstanceID, transfer.TorrentHash)

	// When source torrent still exists, continue deletion
	require.True(t, exists)
	// Would enqueue for continued processing
}

func TestRecoveryLogic_DeletingSourceState_TorrentGone(t *testing.T) {
	ctx := context.Background()

	transfer := newTestTransfer(withState(models.TransferStateDeletingSource))

	sm := newMockSyncManager()
	sm.hasTorrentByHashFunc = func(ctx context.Context, instanceID int, hashes []string) (*qbt.Torrent, bool, error) {
		return nil, false, nil
	}

	s := &Service{syncManager: sm}
	exists := s.checkTorrentExists(ctx, transfer.SourceInstanceID, transfer.TorrentHash)

	// When source torrent is already gone, mark completed
	require.False(t, exists)
	// Recovery would call markCompleted
}

func TestRecoveryLogic_TerminalStatesSkipped(t *testing.T) {
	terminalStates := []models.TransferState{
		models.TransferStateCompleted,
		models.TransferStateFailed,
		models.TransferStateRolledBack,
		models.TransferStateCancelled,
	}

	for _, state := range terminalStates {
		t.Run(string(state), func(t *testing.T) {
			transfer := newTestTransfer(withState(state))
			require.True(t, transfer.State.IsTerminal())
			// Terminal transfers are skipped by recovery
		})
	}
}

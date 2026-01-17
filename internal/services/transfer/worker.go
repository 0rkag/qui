// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
)

// worker processes transfers from the queue
func (s *Service) worker(id int) {
	log.Debug().Int("workerID", id).Msg("[TRANSFER] Worker started")

	for {
		select {
		case <-s.workerCtx.Done():
			log.Debug().Int("workerID", id).Msg("[TRANSFER] Worker stopping")
			return
		case transferID := <-s.queue:
			s.processTransfer(transferID)
		}
	}
}

// processTransfer executes the state machine for a single transfer
func (s *Service) processTransfer(id int64) {
	ctx := s.workerCtx

	t, err := s.store.Get(ctx, id)
	if err != nil {
		log.Error().Err(err).Int64("transferID", id).Msg("[TRANSFER] Failed to load transfer")
		return
	}

	// Skip if already in terminal state
	if t.State.IsTerminal() {
		log.Debug().
			Int64("id", t.ID).
			Str("state", string(t.State)).
			Msg("[TRANSFER] Skipping terminal transfer")
		return
	}

	log.Debug().
		Int64("id", t.ID).
		Str("hash", t.TorrentHash).
		Str("state", string(t.State)).
		Msg("[TRANSFER] Processing transfer")

	// Get instances to select executor
	sourceInstance, err := s.instanceStore.Get(ctx, t.SourceInstanceID)
	if err != nil {
		s.fail(ctx, t, fmt.Sprintf("failed to get source instance: %v", err))
		return
	}

	targetInstance, err := s.instanceStore.Get(ctx, t.TargetInstanceID)
	if err != nil {
		s.fail(ctx, t, fmt.Sprintf("failed to get target instance: %v", err))
		return
	}

	// Select executor based on instance configuration
	executor, err := s.registry.SelectExecutor(sourceInstance, targetInstance)
	if err != nil {
		s.fail(ctx, t, fmt.Sprintf("no executor available: %v", err))
		return
	}

	// State machine
	switch t.State {
	case models.TransferStatePending:
		s.doPrepare(ctx, t, executor)

	case models.TransferStatePreparing:
		// If we're still in preparing state, it means prepare was interrupted
		// Restart from pending
		s.updateState(ctx, t, models.TransferStatePending, "")
		s.doPrepare(ctx, t, executor)

	case models.TransferStateLinksCreating:
		// Links may be partial - attempt rollback and restart
		prep := s.buildPrepareResultFromTransfer(t, sourceInstance, targetInstance)
		_ = executor.Rollback(ctx, t, prep)
		s.updateState(ctx, t, models.TransferStatePending, "")
		s.doPrepare(ctx, t, executor)

	case models.TransferStateLinksCreated:
		prep := s.buildPrepareResultFromTransfer(t, sourceInstance, targetInstance)
		s.doAddTorrent(ctx, t, executor, prep)

	case models.TransferStateAddingTorrent:
		// Check if torrent was actually added
		exists := s.checkTorrentExists(ctx, t.TargetInstanceID, t.TorrentHash)
		if exists {
			s.updateState(ctx, t, models.TransferStateTorrentAdded, "")
			s.continueAfterAdd(ctx, t, executor)
		} else {
			// Torrent wasn't added - rollback and fail
			prep := s.buildPrepareResultFromTransfer(t, sourceInstance, targetInstance)
			_ = executor.Rollback(ctx, t, prep)
			s.fail(ctx, t, "interrupted during add - rolled back")
		}

	case models.TransferStateTorrentAdded:
		s.continueAfterAdd(ctx, t, executor)

	case models.TransferStateDeletingSource:
		s.doDeleteSource(ctx, t, executor)
	}
}

// doPrepare gathers source torrent info and prepares for transfer
func (s *Service) doPrepare(ctx context.Context, t *models.Transfer, executor TransferExecutor) {
	s.updateState(ctx, t, models.TransferStatePreparing, "")

	prep, err := executor.Prepare(ctx, t)
	if err != nil {
		s.fail(ctx, t, err.Error())
		return
	}

	// Update transfer with prepared info
	t.TorrentName = prep.TorrentName
	t.SourceSavePath = prep.SourceSavePath
	t.TargetSavePath = prep.TargetSavePath
	t.LinkMode = prep.LinkMode
	t.FilesTotal = len(prep.Files)
	t.TargetCategory = prep.Category
	t.TargetTags = prep.Tags

	// Save to database
	if err := s.store.Update(ctx, t); err != nil {
		s.fail(ctx, t, fmt.Sprintf("failed to save transfer: %v", err))
		return
	}

	// Continue to create links
	s.doCreateLinks(ctx, t, executor, prep)
}

// doCreateLinks creates hardlinks or reflinks for the torrent files
func (s *Service) doCreateLinks(ctx context.Context, t *models.Transfer, executor TransferExecutor, prep *PrepareResult) {
	s.updateState(ctx, t, models.TransferStateLinksCreating, "")

	// Direct mode - skip link creation
	if prep.LinkMode == "direct" {
		log.Debug().Int64("id", t.ID).Msg("[TRANSFER] Direct mode - skipping link creation")
		s.updateState(ctx, t, models.TransferStateLinksCreated, "")
		s.doAddTorrent(ctx, t, executor, prep)
		return
	}

	filesLinked, err := executor.CreateLinks(ctx, t, prep)
	if err != nil {
		_ = executor.Rollback(ctx, t, prep)
		s.fail(ctx, t, err.Error())
		return
	}

	// Update progress
	t.FilesLinked = filesLinked
	// Update target save path in case it changed during link creation
	t.TargetSavePath = prep.TargetSavePath

	if err := s.store.Update(ctx, t); err != nil {
		_ = executor.Rollback(ctx, t, prep)
		s.fail(ctx, t, fmt.Sprintf("failed to persist link progress: %v", err))
		return
	}

	s.updateState(ctx, t, models.TransferStateLinksCreated, "")
	s.doAddTorrent(ctx, t, executor, prep)
}

// doAddTorrent adds the torrent to the target instance
func (s *Service) doAddTorrent(ctx context.Context, t *models.Transfer, executor TransferExecutor, prep *PrepareResult) {
	s.updateState(ctx, t, models.TransferStateAddingTorrent, "")

	// If we don't have torrent data (recovery scenario), re-export it
	if len(prep.TorrentData) == 0 {
		torrentBytes, _, _, err := s.syncManager.ExportTorrent(ctx, t.SourceInstanceID, t.TorrentHash)
		if err != nil {
			_ = executor.Rollback(ctx, t, prep)
			s.fail(ctx, t, fmt.Sprintf("failed to export torrent: %v", err))
			return
		}
		prep.TorrentData = torrentBytes
	}

	// Ensure category exists on target
	if prep.Category != "" {
		if err := s.ensureCategory(ctx, t.TargetInstanceID, prep.Category, prep.TargetSavePath); err != nil {
			log.Warn().
				Err(err).
				Int64("id", t.ID).
				Str("category", prep.Category).
				Msg("[TRANSFER] Failed to create category, continuing without")
			prep.Category = ""
			t.TargetCategory = ""
		}
	}

	if err := executor.AddTorrent(ctx, t, prep); err != nil {
		_ = executor.Rollback(ctx, t, prep)
		s.fail(ctx, t, err.Error())
		return
	}

	// Wait for torrent to be visible, then resume
	if s.waitForTorrent(ctx, t.TargetInstanceID, t.TorrentHash, 15*time.Second) {
		// Trigger recheck and resume
		if err := s.syncManager.BulkAction(ctx, t.TargetInstanceID, []string{t.TorrentHash}, "recheck"); err != nil {
			log.Warn().Err(err).Int64("id", t.ID).Msg("[TRANSFER] Failed to trigger recheck")
		}
		// Resume after a brief delay for recheck to start (context-aware)
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
		if err := s.syncManager.BulkAction(ctx, t.TargetInstanceID, []string{t.TorrentHash}, "resume"); err != nil {
			log.Warn().Err(err).Int64("id", t.ID).Msg("[TRANSFER] Failed to resume torrent")
		}
	}

	s.updateState(ctx, t, models.TransferStateTorrentAdded, "")
	s.continueAfterAdd(ctx, t, executor)
}

// doDeleteSource removes the torrent from the source instance
func (s *Service) doDeleteSource(ctx context.Context, t *models.Transfer, executor TransferExecutor) {
	s.updateState(ctx, t, models.TransferStateDeletingSource, "")

	if err := executor.DeleteSource(ctx, t); err != nil {
		// Don't fail the whole transfer for this - log warning and complete with warning
		log.Warn().
			Err(err).
			Int64("id", t.ID).
			Msg("[TRANSFER] Failed to delete from source, completing with warning")
		// Store warning so user can see the transfer completed but source wasn't cleaned up
		t.Error = fmt.Sprintf("warning: %v", err)
	}

	s.markCompleted(ctx, t)
}

// continueAfterAdd handles post-add logic
func (s *Service) continueAfterAdd(ctx context.Context, t *models.Transfer, executor TransferExecutor) {
	if t.DeleteFromSource {
		s.doDeleteSource(ctx, t, executor)
	} else {
		s.markCompleted(ctx, t)
	}
}

// buildPrepareResultFromTransfer reconstructs a PrepareResult from a Transfer for recovery.
func (s *Service) buildPrepareResultFromTransfer(t *models.Transfer, source, target *models.Instance) *PrepareResult {
	return &PrepareResult{
		TorrentName:    t.TorrentName,
		SourceSavePath: t.SourceSavePath,
		TargetSavePath: t.TargetSavePath,
		LinkMode:       t.LinkMode,
		Category:       t.TargetCategory,
		Tags:           t.TargetTags,
		SourceInstance: source,
		TargetInstance: target,
		// Files and TorrentData will be fetched on demand if needed
	}
}

// requeueTransfer puts a transfer back in the queue for processing
func (s *Service) requeueTransfer(t *models.Transfer) {
	select {
	case s.queue <- t.ID:
		log.Debug().Int64("id", t.ID).Msg("[TRANSFER] Requeued transfer")
	default:
		log.Warn().Int64("id", t.ID).Msg("[TRANSFER] Queue full, transfer will be picked up on next recovery")
	}
}

// Helper methods that remain on Service

func (s *Service) updateState(ctx context.Context, t *models.Transfer, state models.TransferState, errorMsg string) {
	t.State = state
	t.Error = errorMsg
	if s.store != nil {
		if err := s.store.UpdateState(ctx, t.ID, state, errorMsg); err != nil {
			log.Error().Err(err).Int64("id", t.ID).Str("state", string(state)).Msg("[TRANSFER] Failed to update state")
		}
	}
}

func (s *Service) fail(ctx context.Context, t *models.Transfer, errorMsg string) {
	log.Error().Int64("id", t.ID).Str("error", errorMsg).Msg("[TRANSFER] Transfer failed")
	s.updateState(ctx, t, models.TransferStateFailed, errorMsg)
}

func (s *Service) markCompleted(ctx context.Context, t *models.Transfer) {
	now := time.Now().UTC()
	t.CompletedAt = &now
	t.State = models.TransferStateCompleted
	if s.store != nil {
		if err := s.store.Update(ctx, t); err != nil {
			log.Error().Err(err).Int64("id", t.ID).Msg("[TRANSFER] Failed to mark completed")
			return
		}
	}
	log.Info().
		Int64("id", t.ID).
		Str("name", t.TorrentName).
		Msg("[TRANSFER] Transfer completed successfully")
}

func (s *Service) checkTorrentExists(ctx context.Context, instanceID int, hash string) bool {
	_, exists, err := s.syncManager.HasTorrentByAnyHash(ctx, instanceID, []string{hash})
	if err != nil {
		return false
	}
	return exists
}

func (s *Service) waitForTorrent(ctx context.Context, instanceID int, hash string, timeout time.Duration) bool {
	// Create a context with the shorter of ctx deadline or timeout
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	// Check immediately first
	if s.checkTorrentExists(waitCtx, instanceID, hash) {
		return true
	}

	for {
		select {
		case <-waitCtx.Done():
			return false
		case <-ticker.C:
			if s.checkTorrentExists(waitCtx, instanceID, hash) {
				return true
			}
		}
	}
}

func (s *Service) ensureCategory(ctx context.Context, instanceID int, category, savePath string) error {
	if category == "" {
		return nil
	}

	key := fmt.Sprintf("%d:%s", instanceID, category)

	// Fast path: already created in this session
	if _, ok := s.createdCategories.Load(key); ok {
		return nil
	}

	// Use singleflight to deduplicate concurrent calls
	_, err, _ := s.categoryCreationGroup.Do(key, func() (any, error) {
		// Double-check
		if _, ok := s.createdCategories.Load(key); ok {
			return nil, nil
		}

		// Check if exists
		categories, err := s.syncManager.GetCategories(ctx, instanceID)
		if err != nil {
			return nil, err
		}

		if _, exists := categories[category]; exists {
			s.createdCategories.Store(key, true)
			return nil, nil
		}

		// Create category
		if err := s.syncManager.CreateCategory(ctx, instanceID, category, savePath); err != nil {
			return nil, err
		}

		s.createdCategories.Store(key, true)
		log.Debug().
			Int("instanceID", instanceID).
			Str("category", category).
			Msg("[TRANSFER] Created category")

		return nil, nil
	})

	return err
}

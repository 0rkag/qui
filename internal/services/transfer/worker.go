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
		s.rollbackWithLogging(ctx, t, executor, prep)
		s.updateState(ctx, t, models.TransferStatePending, "")
		s.doPrepare(ctx, t, executor)

	case models.TransferStateLinksCreated:
		prep := s.buildPrepareResultFromTransfer(t, sourceInstance, targetInstance)
		if t.VerifyTransfer {
			s.doVerify(ctx, t, executor, prep)
		} else {
			s.doAddTorrent(ctx, t, executor, prep)
		}

	case models.TransferStateVerifying:
		// Recovery: re-verify and continue
		prep := s.buildPrepareResultFromTransfer(t, sourceInstance, targetInstance)
		if err := executor.VerifyTransfer(ctx, t, prep); err != nil {
			s.rollbackWithLogging(ctx, t, executor, prep)
			s.fail(ctx, t, "verification failed: "+err.Error())
			return
		}
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
			s.rollbackWithLogging(ctx, t, executor, prep)
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

	// Even if prepare fails, save any info we got (like torrent name) so the UI is more helpful
	if prep != nil {
		if prep.TorrentName != "" {
			t.TorrentName = prep.TorrentName
		}
		if prep.SourceSavePath != "" {
			t.SourceSavePath = prep.SourceSavePath
		}
		if prep.TargetSavePath != "" {
			t.TargetSavePath = prep.TargetSavePath
		}
		if prep.LinkMode != "" {
			t.LinkMode = prep.LinkMode
		}
		if len(prep.Files) > 0 {
			t.FilesTotal = len(prep.Files)
			// Calculate total bytes
			var totalBytes int64
			for _, f := range prep.Files {
				totalBytes += f.Size
			}
			t.BytesTotal = totalBytes
		}
		if prep.Category != "" {
			t.TargetCategory = prep.Category
		}
		if len(prep.Tags) > 0 {
			t.TargetTags = prep.Tags
		}
	}

	if err != nil {
		// Save the partial info before failing
		if prep != nil {
			_ = s.store.Update(ctx, t)
		}
		s.fail(ctx, t, err.Error())
		return
	}

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
		s.rollbackWithLogging(ctx, t, executor, prep)
		s.fail(ctx, t, err.Error())
		return
	}

	// Update progress
	t.FilesLinked = filesLinked
	// Update target save path in case it changed during link creation
	t.TargetSavePath = prep.TargetSavePath
	// Mark all bytes as transferred since the operation completed
	t.BytesTransferred = t.BytesTotal

	if err := s.store.Update(ctx, t); err != nil {
		s.rollbackWithLogging(ctx, t, executor, prep)
		s.fail(ctx, t, fmt.Sprintf("failed to persist link progress: %v", err))
		return
	}

	s.updateState(ctx, t, models.TransferStateLinksCreated, "")

	// Continue to verification if enabled, otherwise add torrent
	if t.VerifyTransfer {
		s.doVerify(ctx, t, executor, prep)
	} else {
		s.doAddTorrent(ctx, t, executor, prep)
	}
}

// doVerify verifies transferred files match the source
func (s *Service) doVerify(ctx context.Context, t *models.Transfer, executor TransferExecutor, prep *PrepareResult) {
	s.updateState(ctx, t, models.TransferStateVerifying, "")

	if err := executor.VerifyTransfer(ctx, t, prep); err != nil {
		s.rollbackWithLogging(ctx, t, executor, prep)
		s.fail(ctx, t, fmt.Sprintf("verification failed: %v", err))
		return
	}

	log.Info().Int64("id", t.ID).Msg("[TRANSFER] Verification passed")
	s.doAddTorrent(ctx, t, executor, prep)
}

// doAddTorrent adds the torrent to the target instance
func (s *Service) doAddTorrent(ctx context.Context, t *models.Transfer, executor TransferExecutor, prep *PrepareResult) {
	s.updateState(ctx, t, models.TransferStateAddingTorrent, "")

	// If we don't have torrent data (recovery scenario), re-export it
	if len(prep.TorrentData) == 0 {
		torrentBytes, _, _, err := s.syncManager.ExportTorrent(ctx, t.SourceInstanceID, t.TorrentHash)
		if err != nil {
			s.rollbackWithLogging(ctx, t, executor, prep)
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
			// Store warning so user knows category wasn't preserved
			t.Error = fmt.Sprintf("warning: failed to create category %q: %v", prep.Category, err)
			prep.Category = ""
			t.TargetCategory = ""
		}
	}

	if err := executor.AddTorrent(ctx, t, prep); err != nil {
		s.rollbackWithLogging(ctx, t, executor, prep)
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

// continueAfterAdd handles post-add logic based on SourceAction
func (s *Service) continueAfterAdd(ctx context.Context, t *models.Transfer, executor TransferExecutor) {
	switch t.SourceAction {
	case models.SourceDelete:
		s.doDeleteSource(ctx, t, executor)
	case models.SourcePause:
		s.doPauseSource(ctx, t)
		s.markCompleted(ctx, t)
	default: // SourceKeep
		s.markCompleted(ctx, t)
	}
}

// doPauseSource pauses the torrent on the source instance
func (s *Service) doPauseSource(ctx context.Context, t *models.Transfer) {
	if s.syncManager == nil {
		return
	}
	if err := s.syncManager.BulkAction(ctx, t.SourceInstanceID, []string{t.TorrentHash}, "pause"); err != nil {
		log.Warn().
			Err(err).
			Int64("id", t.ID).
			Msg("[TRANSFER] Failed to pause source torrent, completing with warning")
		t.Error = fmt.Sprintf("warning: failed to pause source: %v", err)
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
		log.Warn().Err(err).Int("instanceID", instanceID).Str("hash", hash).Msg("[TRANSFER] Error checking if torrent exists")
		return false
	}
	return exists
}

// rollbackWithLogging attempts rollback and logs any failure.
// Returns the error for callers that need to include it in failure messages.
func (s *Service) rollbackWithLogging(ctx context.Context, t *models.Transfer, executor TransferExecutor, prep *PrepareResult) error {
	if err := executor.Rollback(ctx, t, prep); err != nil {
		log.Warn().Err(err).Int64("id", t.ID).Msg("[TRANSFER] Rollback failed - orphaned files may remain on target")
		return err
	}
	return nil
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

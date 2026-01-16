// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
)

// InstanceLogStreamManager manages log streaming hubs for multiple qBittorrent instances.
// It provides lazy initialization of hubs and automatic cleanup when all subscribers disconnect.
type InstanceLogStreamManager struct {
	instanceStore *models.InstanceStore
	hubs          map[int]*InstanceLogHub
	mu            sync.RWMutex
	cleanupTicker *time.Ticker
	stopCleanup   chan struct{}
}

// NewInstanceLogStreamManager creates a new InstanceLogStreamManager.
func NewInstanceLogStreamManager(instanceStore *models.InstanceStore) *InstanceLogStreamManager {
	m := &InstanceLogStreamManager{
		instanceStore: instanceStore,
		hubs:          make(map[int]*InstanceLogHub),
		cleanupTicker: time.NewTicker(5 * time.Minute),
		stopCleanup:   make(chan struct{}),
	}

	// Start cleanup routine
	go m.cleanupLoop()

	return m
}

// GetHub returns or creates a log hub for the given instance ID.
func (m *InstanceLogStreamManager) GetHub(ctx context.Context, instanceID int) (*InstanceLogHub, error) {
	// Try to get existing hub
	m.mu.RLock()
	hub, exists := m.hubs[instanceID]
	m.mu.RUnlock()

	if exists {
		return hub, nil
	}

	// Create new hub
	return m.createHub(ctx, instanceID)
}

// createHub creates a new log hub for the given instance.
func (m *InstanceLogStreamManager) createHub(ctx context.Context, instanceID int) (*InstanceLogHub, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check if hub was created while waiting for lock
	if hub, exists := m.hubs[instanceID]; exists {
		return hub, nil
	}

	// Get instance details
	instance, err := m.instanceStore.Get(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get instance: %w", err)
	}

	if !instance.IsActive {
		return nil, fmt.Errorf("instance %d is disabled", instanceID)
	}

	// Decrypt password
	password, err := m.instanceStore.GetDecryptedPassword(instance)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt password: %w", err)
	}

	// Decrypt basic auth password if present
	var basicPassword *string
	if instance.BasicPasswordEncrypted != nil {
		basicPassword, err = m.instanceStore.GetDecryptedBasicPassword(instance)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt basic auth password: %w", err)
		}
	}

	// Create hub
	hub := NewInstanceLogHub(InstanceLogHubConfig{
		InstanceID:    instanceID,
		Host:          instance.Host,
		Username:      instance.Username,
		Password:      password,
		BasicUsername: instance.BasicUsername,
		BasicPassword: basicPassword,
		TLSSkipVerify: instance.TLSSkipVerify,
	})

	m.hubs[instanceID] = hub

	log.Debug().Int("instanceID", instanceID).Msg("Created log streaming hub for instance")

	return hub, nil
}

// RemoveHub removes and closes the hub for the given instance.
func (m *InstanceLogStreamManager) RemoveHub(instanceID int) {
	m.mu.Lock()
	hub, exists := m.hubs[instanceID]
	if exists {
		delete(m.hubs, instanceID)
	}
	m.mu.Unlock()

	if exists {
		hub.Close()
		log.Debug().Int("instanceID", instanceID).Msg("Removed log streaming hub for instance")
	}
}

// cleanupLoop periodically removes hubs with no subscribers.
func (m *InstanceLogStreamManager) cleanupLoop() {
	for {
		select {
		case <-m.cleanupTicker.C:
			m.cleanup()
		case <-m.stopCleanup:
			return
		}
	}
}

// cleanup removes hubs that have no subscribers.
func (m *InstanceLogStreamManager) cleanup() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for instanceID, hub := range m.hubs {
		if hub.SubscriberCount() == 0 {
			hub.Close()
			delete(m.hubs, instanceID)
			log.Debug().Int("instanceID", instanceID).Msg("Cleaned up idle log streaming hub")
		}
	}
}

// Close stops all hubs and releases resources.
func (m *InstanceLogStreamManager) Close() {
	close(m.stopCleanup)
	m.cleanupTicker.Stop()

	m.mu.Lock()
	defer m.mu.Unlock()

	for instanceID, hub := range m.hubs {
		hub.Close()
		delete(m.hubs, instanceID)
	}

	log.Info().Msg("Instance log stream manager closed")
}

// HubCount returns the number of active hubs.
func (m *InstanceLogStreamManager) HubCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.hubs)
}

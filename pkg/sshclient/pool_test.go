// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sshclient

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestPool_ConfigKey(t *testing.T) {
	p := NewPool(time.Minute)
	defer p.Close()

	tests := []struct {
		cfg      *Config
		expected string
	}{
		{
			cfg:      &Config{Username: "user", Host: "host", Port: 22},
			expected: "user@host:22",
		},
		{
			cfg:      &Config{Username: "admin", Host: "192.168.1.1", Port: 2222},
			expected: "admin@192.168.1.1:2222",
		},
	}

	for _, tt := range tests {
		got := p.configKey(tt.cfg)
		if got != tt.expected {
			t.Errorf("configKey() = %q, want %q", got, tt.expected)
		}
	}
}

func TestPool_Stats(t *testing.T) {
	p := NewPool(time.Minute)
	defer p.Close()

	stats := p.Stats()
	if stats.ActiveConnections != 0 {
		t.Errorf("expected 0 active connections, got %d", stats.ActiveConnections)
	}
	if len(stats.Hosts) != 0 {
		t.Errorf("expected 0 hosts, got %d", len(stats.Hosts))
	}
}

func TestPool_Close(t *testing.T) {
	p := NewPool(time.Minute)

	// Close should work even with no connections
	err := p.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}

	// Stats after close should show 0 connections
	stats := p.Stats()
	if stats.ActiveConnections != 0 {
		t.Errorf("expected 0 connections after close, got %d", stats.ActiveConnections)
	}
}

func newTestPool(maxIdleTime time.Duration) *Pool {
	cfg := PoolConfig{
		MaxIdleTime:         maxIdleTime,
		CleanupInterval:     time.Hour, // Don't auto-cleanup during tests
		MaxSize:             100,
		HealthCheckInterval: time.Hour, // Don't auto-health-check during tests
	}
	return NewPoolWithConfig(context.Background(), cfg)
}

func TestPool_CleanupIdleConnections(t *testing.T) {
	// Create pool with very short idle time
	p := newTestPool(10 * time.Millisecond)
	defer p.Close()

	// Add a mock entry directly (simulating a cached connection)
	key := "test@host:22"
	p.mu.Lock()
	p.clients[key] = &pooledClient{
		client:   nil, // Can't create real client without SSH server
		lastUsed: time.Now().Add(-time.Hour), // Already expired
	}
	p.mu.Unlock()

	p.mu.Lock()
	count := len(p.clients)
	p.mu.Unlock()
	if count != 1 {
		t.Fatalf("expected 1 client before cleanup")
	}

	// Run cleanup
	p.cleanup()

	p.mu.Lock()
	count = len(p.clients)
	p.mu.Unlock()
	if count != 0 {
		t.Errorf("expected 0 clients after cleanup, got %d", count)
	}
}

func TestPool_CleanupKeepsActiveConnections(t *testing.T) {
	p := newTestPool(time.Hour)
	defer p.Close()

	// Add a mock entry that was just used
	key := "test@host:22"
	p.mu.Lock()
	p.clients[key] = &pooledClient{
		client:   nil,
		lastUsed: time.Now(), // Just used
	}
	p.mu.Unlock()

	// Run cleanup
	p.cleanup()

	p.mu.Lock()
	count := len(p.clients)
	p.mu.Unlock()
	if count != 1 {
		t.Errorf("expected 1 client after cleanup (still active), got %d", count)
	}
}

func TestPool_Remove(t *testing.T) {
	p := newTestPool(time.Hour)
	defer p.Close()

	cfg := &Config{Username: "test", Host: "host", Port: 22}
	key := p.configKey(cfg)

	// Add a mock entry
	p.mu.Lock()
	p.clients[key] = &pooledClient{
		client:   &Client{}, // Empty client - Close() will be no-op
		lastUsed: time.Now(),
	}
	p.mu.Unlock()

	p.mu.Lock()
	count := len(p.clients)
	p.mu.Unlock()
	if count != 1 {
		t.Fatalf("expected 1 client before remove")
	}

	// Remove it
	err := p.Remove(cfg)
	if err != nil {
		t.Errorf("Remove() error = %v", err)
	}

	p.mu.Lock()
	count = len(p.clients)
	p.mu.Unlock()
	if count != 0 {
		t.Errorf("expected 0 clients after remove, got %d", count)
	}
}

func TestPool_RemoveNonExistent(t *testing.T) {
	p := NewPool(time.Minute)
	defer p.Close()

	cfg := &Config{Username: "test", Host: "host", Port: 22}

	// Remove non-existent should not error
	err := p.Remove(cfg)
	if err != nil {
		t.Errorf("Remove() error = %v, expected nil for non-existent", err)
	}
}

func TestPool_ConcurrentAccess(t *testing.T) {
	p := newTestPool(time.Hour)
	defer p.Close()

	// Test concurrent stats calls don't race
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = p.Stats()
			}
		}()
	}
	wg.Wait()
}

func TestPool_Release(t *testing.T) {
	p := NewPool(time.Minute)
	defer p.Close()

	// Release is currently a no-op, but shouldn't panic
	p.Release(nil)
	p.Release(&Client{})
}

func TestNewPool_DefaultIdleTime(t *testing.T) {
	p := NewPool(0) // Should default to 5 minutes
	defer p.Close()

	if p.config.MaxIdleTime != 5*time.Minute {
		t.Errorf("expected default MaxIdleTime of 5m, got %v", p.config.MaxIdleTime)
	}
}

func TestNewPool_CustomIdleTime(t *testing.T) {
	p := NewPool(10 * time.Minute)
	defer p.Close()

	if p.config.MaxIdleTime != 10*time.Minute {
		t.Errorf("expected MaxIdleTime of 10m, got %v", p.config.MaxIdleTime)
	}
}

func TestPool_InvalidateHost(t *testing.T) {
	p := newTestPool(time.Hour)
	defer p.Close()

	// Add entries for multiple hosts
	p.mu.Lock()
	p.clients["user1@host1:22"] = &pooledClient{client: &Client{}, lastUsed: time.Now()}
	p.clients["user2@host1:22"] = &pooledClient{client: &Client{}, lastUsed: time.Now()}
	p.clients["user1@host2:22"] = &pooledClient{client: &Client{}, lastUsed: time.Now()}
	p.mu.Unlock()

	// Invalidate host1:22
	p.InvalidateHost("host1", 22)

	p.mu.Lock()
	count := len(p.clients)
	_, hasHost1User1 := p.clients["user1@host1:22"]
	_, hasHost1User2 := p.clients["user2@host1:22"]
	_, hasHost2 := p.clients["user1@host2:22"]
	p.mu.Unlock()

	if count != 1 {
		t.Errorf("expected 1 client after invalidate, got %d", count)
	}
	if hasHost1User1 || hasHost1User2 {
		t.Error("expected host1:22 connections to be removed")
	}
	if !hasHost2 {
		t.Error("expected host2:22 connection to remain")
	}
}

func TestPool_MaxSize(t *testing.T) {
	cfg := PoolConfig{
		MaxIdleTime:         time.Hour,
		CleanupInterval:     time.Hour,
		MaxSize:             3, // Small max size for testing
		HealthCheckInterval: time.Hour,
	}
	p := NewPoolWithConfig(context.Background(), cfg)
	defer p.Close()

	// Add 4 entries - should evict the oldest
	p.mu.Lock()
	p.clients["user1@host1:22"] = &pooledClient{client: &Client{}, lastUsed: time.Now().Add(-3 * time.Hour)}
	p.clients["user2@host2:22"] = &pooledClient{client: &Client{}, lastUsed: time.Now().Add(-2 * time.Hour)}
	p.clients["user3@host3:22"] = &pooledClient{client: &Client{}, lastUsed: time.Now().Add(-1 * time.Hour)}
	p.mu.Unlock()

	// This should trigger eviction of the oldest (user1@host1:22)
	newClient := &Client{}
	p.storeIfAbsent("user4@host4:22", newClient)

	p.mu.Lock()
	count := len(p.clients)
	_, hasOldest := p.clients["user1@host1:22"]
	_, hasNewest := p.clients["user4@host4:22"]
	p.mu.Unlock()

	if count != 3 {
		t.Errorf("expected 3 clients after max size enforcement, got %d", count)
	}
	if hasOldest {
		t.Error("expected oldest client to be evicted")
	}
	if !hasNewest {
		t.Error("expected newest client to be stored")
	}
}

// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sshclient

import (
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

func TestPool_CleanupIdleConnections(t *testing.T) {
	// Create pool with very short idle time
	p := &Pool{
		clients: make(map[string]*pooledClient),
		maxIdle: 10 * time.Millisecond,
		done:    make(chan struct{}),
	}
	p.cleanupT = time.NewTicker(time.Hour) // Don't auto-cleanup
	defer p.Close()

	// Add a mock entry directly (simulating a cached connection)
	key := "test@host:22"
	p.clients[key] = &pooledClient{
		client:   nil, // Can't create real client without SSH server
		lastUsed: time.Now().Add(-time.Hour), // Already expired
	}

	if len(p.clients) != 1 {
		t.Fatalf("expected 1 client before cleanup")
	}

	// Run cleanup
	p.cleanup()

	if len(p.clients) != 0 {
		t.Errorf("expected 0 clients after cleanup, got %d", len(p.clients))
	}
}

func TestPool_CleanupKeepsActiveConnections(t *testing.T) {
	p := &Pool{
		clients: make(map[string]*pooledClient),
		maxIdle: time.Hour, // Long idle time
		done:    make(chan struct{}),
	}
	p.cleanupT = time.NewTicker(time.Hour)
	defer p.Close()

	// Add a mock entry that was just used
	key := "test@host:22"
	p.clients[key] = &pooledClient{
		client:   nil,
		lastUsed: time.Now(), // Just used
	}

	// Run cleanup
	p.cleanup()

	if len(p.clients) != 1 {
		t.Errorf("expected 1 client after cleanup (still active), got %d", len(p.clients))
	}
}

func TestPool_Remove(t *testing.T) {
	p := &Pool{
		clients: make(map[string]*pooledClient),
		maxIdle: time.Hour,
		done:    make(chan struct{}),
	}
	p.cleanupT = time.NewTicker(time.Hour)
	defer p.Close()

	cfg := &Config{Username: "test", Host: "host", Port: 22}
	key := p.configKey(cfg)

	// Add a mock entry
	p.clients[key] = &pooledClient{
		client:   &Client{}, // Empty client - Close() will be no-op
		lastUsed: time.Now(),
	}

	if len(p.clients) != 1 {
		t.Fatalf("expected 1 client before remove")
	}

	// Remove it
	err := p.Remove(cfg)
	if err != nil {
		t.Errorf("Remove() error = %v", err)
	}

	if len(p.clients) != 0 {
		t.Errorf("expected 0 clients after remove, got %d", len(p.clients))
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
	p := &Pool{
		clients: make(map[string]*pooledClient),
		maxIdle: time.Hour,
		done:    make(chan struct{}),
	}
	p.cleanupT = time.NewTicker(time.Hour)
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

	if p.maxIdle != 5*time.Minute {
		t.Errorf("expected default maxIdle of 5m, got %v", p.maxIdle)
	}
}

func TestNewPool_CustomIdleTime(t *testing.T) {
	p := NewPool(10 * time.Minute)
	defer p.Close()

	if p.maxIdle != 10*time.Minute {
		t.Errorf("expected maxIdle of 10m, got %v", p.maxIdle)
	}
}

// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package ftpclient

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestConnectionKey(t *testing.T) {
	tests := []struct {
		name     string
		cfg      *Config
		expected string
	}{
		{
			name: "basic config",
			cfg: &Config{
				Username:      "user",
				Port:          21,
				Host:          "ftp.example.com",
				TLSMode:       TLSModeExplicit,
				SkipTLSVerify: false,
			},
			expected: "user:21@ftp.example.com|tls=explicit|skipVerify=false",
		},
		{
			name: "implicit TLS",
			cfg: &Config{
				Username:      "admin",
				Port:          990,
				Host:          "secure.example.com",
				TLSMode:       TLSModeImplicit,
				SkipTLSVerify: true,
			},
			expected: "admin:990@secure.example.com|tls=implicit|skipVerify=true",
		},
		{
			name: "no TLS",
			cfg: &Config{
				Username:      "anon",
				Port:          21,
				Host:          "public.example.com",
				TLSMode:       TLSModeNone,
				SkipTLSVerify: false,
			},
			expected: "anon:21@public.example.com|tls=none|skipVerify=false",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := connectionKey(tt.cfg)
			if got != tt.expected {
				t.Errorf("connectionKey() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestConnectionKey_DifferentConfigs(t *testing.T) {
	cfg1 := &Config{
		Username:      "user1",
		Port:          21,
		Host:          "ftp.example.com",
		TLSMode:       TLSModeExplicit,
		SkipTLSVerify: false,
	}

	cfg2 := &Config{
		Username:      "user2",
		Port:          21,
		Host:          "ftp.example.com",
		TLSMode:       TLSModeExplicit,
		SkipTLSVerify: false,
	}

	cfg3 := &Config{
		Username:      "user1",
		Port:          21,
		Host:          "other.example.com",
		TLSMode:       TLSModeExplicit,
		SkipTLSVerify: false,
	}

	cfg4 := &Config{
		Username:      "user1",
		Port:          21,
		Host:          "ftp.example.com",
		TLSMode:       TLSModeImplicit,
		SkipTLSVerify: false,
	}

	key1 := connectionKey(cfg1)
	key2 := connectionKey(cfg2)
	key3 := connectionKey(cfg3)
	key4 := connectionKey(cfg4)

	// Different username should produce different key
	if key1 == key2 {
		t.Error("Different usernames should produce different keys")
	}

	// Different host should produce different key
	if key1 == key3 {
		t.Error("Different hosts should produce different keys")
	}

	// Different TLS mode should produce different key
	if key1 == key4 {
		t.Error("Different TLS modes should produce different keys")
	}
}

func TestConnectionKey_SameConfig(t *testing.T) {
	cfg1 := &Config{
		Username:      "user",
		Port:          21,
		Host:          "ftp.example.com",
		TLSMode:       TLSModeExplicit,
		SkipTLSVerify: false,
	}

	cfg2 := &Config{
		Username:      "user",
		Port:          21,
		Host:          "ftp.example.com",
		TLSMode:       TLSModeExplicit,
		SkipTLSVerify: false,
		Password:      "different_password", // Password not in key
	}

	key1 := connectionKey(cfg1)
	key2 := connectionKey(cfg2)

	// Same connection params should produce same key (password doesn't affect key)
	if key1 != key2 {
		t.Errorf("Same connection params should produce same key: %q != %q", key1, key2)
	}
}

func TestNewPool(t *testing.T) {
	pool := NewPool(time.Minute)
	if pool == nil {
		t.Fatal("NewPool() returned nil")
	}
	defer pool.Close()

	if pool.idleTimeout != time.Minute {
		t.Errorf("Pool.idleTimeout = %v, want 1m", pool.idleTimeout)
	}

	if pool.connections == nil {
		t.Error("Pool.connections is nil")
	}
}

func TestNewPool_DefaultIdleTimeout(t *testing.T) {
	pool := NewPool(0) // Should default to 5 minutes
	if pool == nil {
		t.Fatal("NewPool(0) returned nil")
	}
	defer pool.Close()

	if pool.idleTimeout != 5*time.Minute {
		t.Errorf("Pool.idleTimeout = %v, want 5m (default)", pool.idleTimeout)
	}
}

func TestPool_Close(t *testing.T) {
	pool := NewPool(time.Minute)

	// Close should not error on empty pool
	err := pool.Close()
	if err != nil {
		t.Errorf("Close() on empty pool error = %v", err)
	}

	// Double close should be safe (idempotent)
	err = pool.Close()
	if err != nil {
		t.Errorf("Double Close() error = %v", err)
	}
}

func TestPool_CloseIdempotent(t *testing.T) {
	pool := NewPool(time.Minute)

	// Close multiple times should be safe
	for i := 0; i < 5; i++ {
		err := pool.Close()
		if err != nil {
			t.Errorf("Close() call %d error = %v", i+1, err)
		}
	}
}

func TestPool_Release(t *testing.T) {
	pool := NewPool(time.Minute)
	defer pool.Close()

	// Release nil client should not panic
	pool.Release(nil)

	// Release unknown client should not panic
	pool.Release(&Client{})
}

func TestPool_CleanupIdle(t *testing.T) {
	// Create pool with very short idle time
	pool := NewPool(10 * time.Millisecond)
	defer pool.Close()

	// Add a mock connection directly
	key := "user:21@host|tls=explicit|skipVerify=false"
	pool.mu.Lock()
	pool.connections[key] = &pooledConnection{
		client:   &Client{conn: nil}, // Nil conn so Close() is safe
		lastUsed: time.Now().Add(-time.Hour), // Already expired
	}
	pool.mu.Unlock()

	pool.mu.Lock()
	countBefore := len(pool.connections)
	pool.mu.Unlock()

	if countBefore != 1 {
		t.Fatalf("Expected 1 connection before cleanup, got %d", countBefore)
	}

	// Run cleanup
	pool.cleanup()

	pool.mu.Lock()
	countAfter := len(pool.connections)
	pool.mu.Unlock()

	if countAfter != 0 {
		t.Errorf("Expected 0 connections after cleanup, got %d", countAfter)
	}
}

func TestPool_CleanupKeepsActive(t *testing.T) {
	pool := NewPool(time.Hour) // Long idle timeout
	defer pool.Close()

	// Add a recently used connection
	key := "user:21@host|tls=explicit|skipVerify=false"
	pool.mu.Lock()
	pool.connections[key] = &pooledConnection{
		client:   &Client{conn: nil},
		lastUsed: time.Now(), // Just used
	}
	pool.mu.Unlock()

	// Run cleanup
	pool.cleanup()

	pool.mu.Lock()
	count := len(pool.connections)
	pool.mu.Unlock()

	if count != 1 {
		t.Errorf("Expected 1 connection after cleanup (still active), got %d", count)
	}
}

func TestPool_ConcurrentRelease(t *testing.T) {
	pool := NewPool(time.Minute)
	defer pool.Close()

	// Add multiple distinct mock connections using different configs
	clients := make([]*Client, 5)
	for i := 0; i < 5; i++ {
		key := connectionKey(&Config{
			Username: fmt.Sprintf("user%d", i), // Different username = different key
			Port:     21,
			Host:     "host",
			TLSMode:  TLSModeExplicit,
		})
		clients[i] = &Client{conn: nil}
		pool.mu.Lock()
		pool.connections[key] = &pooledConnection{
			client:   clients[i],
			lastUsed: time.Now(),
		}
		pool.mu.Unlock()
	}

	// Verify we actually have 5 connections
	pool.mu.Lock()
	initialCount := len(pool.connections)
	pool.mu.Unlock()
	if initialCount != 5 {
		t.Fatalf("Expected 5 connections, got %d", initialCount)
	}

	// Concurrent releases should not race
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			// Release actual clients we created (some may not be found, that's ok)
			if idx < len(clients) {
				pool.Release(clients[idx])
			} else {
				pool.Release(&Client{}) // Unknown client - tests handling of unknown clients
			}
		}(i)
	}
	wg.Wait()
}

func TestPool_ConcurrentClose(t *testing.T) {
	pool := NewPool(time.Minute)

	// Concurrent closes should not panic
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pool.Close()
		}()
	}
	wg.Wait()
}

func TestPooledConnection_LastUsed(t *testing.T) {
	now := time.Now()
	pc := &pooledConnection{
		client:   &Client{},
		lastUsed: now,
	}

	if !pc.lastUsed.Equal(now) {
		t.Errorf("pooledConnection.lastUsed = %v, want %v", pc.lastUsed, now)
	}
}

// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sshclient

import (
	"fmt"
	"sync"
	"time"
)

// Pool manages a pool of SSH connections for reuse.
type Pool struct {
	mu       sync.Mutex
	clients  map[string]*pooledClient
	maxIdle  time.Duration
	cleanupT *time.Ticker
	done     chan struct{}
}

type pooledClient struct {
	client   *Client
	lastUsed time.Time
}

// NewPool creates a new connection pool.
func NewPool(maxIdleTime time.Duration) *Pool {
	if maxIdleTime == 0 {
		maxIdleTime = 5 * time.Minute
	}

	p := &Pool{
		clients: make(map[string]*pooledClient),
		maxIdle: maxIdleTime,
		done:    make(chan struct{}),
	}

	// Start cleanup goroutine
	p.cleanupT = time.NewTicker(time.Minute)
	go p.cleanupLoop()

	return p
}

// Get retrieves or creates an SSH client for the given config.
// If a cached connection exists but is dead, it will be replaced with a new one.
//
// Note: Due to the nature of network connections, a client that passes the
// IsAlive() check may still fail on subsequent operations if the connection
// dies between the check and usage. Callers should handle connection errors
// gracefully and retry if needed.
func (p *Pool) Get(cfg *Config) (*Client, error) {
	key := p.configKey(cfg)

	// First, try to get an existing live connection
	existingClient, deadClient := p.getExisting(key)
	if existingClient != nil {
		return existingClient, nil
	}
	// Close dead client outside the lock
	if deadClient != nil {
		deadClient.Close()
	}

	// Create new client outside the lock to avoid blocking other goroutines
	client, err := New(cfg)
	if err != nil {
		return nil, err
	}

	// Try to store the new client, but another goroutine may have beat us
	if winner := p.storeIfAbsent(key, client); winner != nil {
		// Another goroutine created a client first, use theirs
		client.Close()
		return winner, nil
	}

	return client, nil
}

// getExisting returns an existing live client, or the dead client to close.
// Returns (liveClient, nil) if found alive, (nil, deadClient) if found dead,
// or (nil, nil) if not found.
func (p *Pool) getExisting(key string) (live *Client, dead *Client) {
	p.mu.Lock()
	defer p.mu.Unlock()

	pc, ok := p.clients[key]
	if !ok {
		return nil, nil
	}

	if pc.client.IsAlive() {
		pc.lastUsed = time.Now()
		return pc.client, nil
	}

	// Connection is dead, remove from pool
	delete(p.clients, key)
	return nil, pc.client
}

// storeIfAbsent stores the client if no live client exists for the key.
// Returns nil if stored successfully, or the existing live client if one was found.
func (p *Pool) storeIfAbsent(key string, client *Client) *Client {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Check if another goroutine added a client while we were creating ours
	if pc, ok := p.clients[key]; ok {
		if pc.client.IsAlive() {
			return pc.client
		}
		// The other client is also dead, close it async and use ours
		delete(p.clients, key)
		go pc.client.Close()
	}

	p.clients[key] = &pooledClient{
		client:   client,
		lastUsed: time.Now(),
	}
	return nil
}

// Release marks a client as no longer in use.
// The client remains in the pool for reuse.
func (p *Pool) Release(_ *Client) {
	// Currently a no-op since we keep connections alive
	// Could implement reference counting if needed
}

// Close closes all connections and stops the pool.
func (p *Pool) Close() error {
	close(p.done)
	p.cleanupT.Stop()

	p.mu.Lock()
	defer p.mu.Unlock()

	var lastErr error
	for key, pc := range p.clients {
		if pc.client != nil {
			if err := pc.client.Close(); err != nil {
				lastErr = err
			}
		}
		delete(p.clients, key)
	}
	return lastErr
}

// Remove removes and closes a specific client from the pool.
func (p *Pool) Remove(cfg *Config) error {
	key := p.configKey(cfg)

	p.mu.Lock()
	defer p.mu.Unlock()

	if pc, ok := p.clients[key]; ok {
		delete(p.clients, key)
		if pc.client != nil {
			return pc.client.Close()
		}
	}
	return nil
}

// Stats returns pool statistics.
type PoolStats struct {
	ActiveConnections int
	Hosts             []string
}

// Stats returns current pool statistics.
func (p *Pool) Stats() PoolStats {
	p.mu.Lock()
	defer p.mu.Unlock()

	stats := PoolStats{
		ActiveConnections: len(p.clients),
		Hosts:             make([]string, 0, len(p.clients)),
	}

	for key := range p.clients {
		stats.Hosts = append(stats.Hosts, key)
	}

	return stats
}

func (p *Pool) cleanupLoop() {
	for {
		select {
		case <-p.done:
			return
		case <-p.cleanupT.C:
			p.cleanup()
		}
	}
}

func (p *Pool) cleanup() {
	// Collect stale clients while holding the lock
	var toClose []*Client

	p.mu.Lock()
	now := time.Now()
	for key, pc := range p.clients {
		if now.Sub(pc.lastUsed) > p.maxIdle {
			toClose = append(toClose, pc.client)
			delete(p.clients, key)
		}
	}
	p.mu.Unlock()

	// Close connections outside the lock to avoid blocking
	for _, client := range toClose {
		if client != nil {
			client.Close()
		}
	}
}

func (p *Pool) configKey(cfg *Config) string {
	return fmt.Sprintf("%s@%s:%d", cfg.Username, cfg.Host, cfg.Port)
}

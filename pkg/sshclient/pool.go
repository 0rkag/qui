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
func (p *Pool) Get(cfg *Config) (*Client, error) {
	key := p.configKey(cfg)

	p.mu.Lock()
	if pc, ok := p.clients[key]; ok {
		pc.lastUsed = time.Now()
		p.mu.Unlock()
		return pc.client, nil
	}
	p.mu.Unlock()

	// Create new client outside the lock
	client, err := New(cfg)
	if err != nil {
		return nil, err
	}

	p.mu.Lock()
	// Check again in case another goroutine created it
	if pc, ok := p.clients[key]; ok {
		p.mu.Unlock()
		client.Close() // Close the one we just created
		return pc.client, nil
	}

	p.clients[key] = &pooledClient{
		client:   client,
		lastUsed: time.Now(),
	}
	p.mu.Unlock()

	return client, nil
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
		if err := pc.client.Close(); err != nil {
			lastErr = err
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
		return pc.client.Close()
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
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	for key, pc := range p.clients {
		if now.Sub(pc.lastUsed) > p.maxIdle {
			pc.client.Close()
			delete(p.clients, key)
		}
	}
}

func (p *Pool) configKey(cfg *Config) string {
	return fmt.Sprintf("%s@%s:%d", cfg.Username, cfg.Host, cfg.Port)
}

// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package ftpclient

import (
	"fmt"
	"sync"
	"time"
)

// Pool manages a pool of FTP connections.
type Pool struct {
	mu          sync.Mutex
	connections map[string]*pooledConnection
	idleTimeout time.Duration
	done        chan struct{}
	closed      bool
}

type pooledConnection struct {
	client   *Client
	lastUsed time.Time
}

// NewPool creates a new FTP connection pool.
// idleTimeout specifies how long idle connections are kept (0 = 5 minutes default).
func NewPool(idleTimeout time.Duration) *Pool {
	if idleTimeout == 0 {
		idleTimeout = 5 * time.Minute
	}

	pool := &Pool{
		connections: make(map[string]*pooledConnection),
		idleTimeout: idleTimeout,
		done:        make(chan struct{}),
	}

	// Start cleanup goroutine
	go pool.cleanupLoop()

	return pool
}

// connectionKey generates a unique key for a connection config.
func connectionKey(cfg *Config) string {
	return fmt.Sprintf("%s:%d@%s", cfg.Username, cfg.Port, cfg.Host)
}

// Get retrieves or creates an FTP client for the given config.
func (p *Pool) Get(cfg *Config) (*Client, error) {
	key := connectionKey(cfg)

	p.mu.Lock()
	defer p.mu.Unlock()

	// Check for existing connection
	if pc, ok := p.connections[key]; ok {
		if pc.client.IsConnected() {
			pc.lastUsed = time.Now()
			return pc.client, nil
		}
		// Connection is dead, remove it
		pc.client.Close()
		delete(p.connections, key)
	}

	// Create new connection
	client, err := New(cfg)
	if err != nil {
		return nil, err
	}

	p.connections[key] = &pooledConnection{
		client:   client,
		lastUsed: time.Now(),
	}

	return client, nil
}

// Release marks a connection as available for reuse.
// For now this is a no-op since we keep connections in the pool.
func (p *Pool) Release(client *Client) {
	// Update last used time
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, pc := range p.connections {
		if pc.client == client {
			pc.lastUsed = time.Now()
			return
		}
	}
}

// Close closes all connections in the pool and stops the cleanup goroutine.
func (p *Pool) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return nil
	}
	p.closed = true

	// Signal cleanup goroutine to stop
	close(p.done)

	var lastErr error
	for key, pc := range p.connections {
		if err := pc.client.Close(); err != nil {
			lastErr = err
		}
		delete(p.connections, key)
	}

	return lastErr
}

// cleanupLoop periodically removes idle connections.
func (p *Pool) cleanupLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-p.done:
			return
		case <-ticker.C:
			p.cleanup()
		}
	}
}

// cleanup removes connections that have been idle too long.
func (p *Pool) cleanup() {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	for key, pc := range p.connections {
		if now.Sub(pc.lastUsed) > p.idleTimeout {
			pc.client.Close()
			delete(p.connections, key)
		}
	}
}

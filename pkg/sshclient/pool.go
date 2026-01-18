// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sshclient

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// PoolConfig holds configuration for the connection pool.
type PoolConfig struct {
	MaxIdleTime         time.Duration // Maximum time a connection can be idle (default 5m)
	CleanupInterval     time.Duration // Interval between cleanup runs (default 1m)
	MaxSize             int           // Maximum number of connections in the pool (default 100)
	HealthCheckInterval time.Duration // Interval between health checks (default 2m)
}

// DefaultPoolConfig returns the default pool configuration.
func DefaultPoolConfig() PoolConfig {
	return PoolConfig{
		MaxIdleTime:         5 * time.Minute,
		CleanupInterval:     time.Minute,
		MaxSize:             100,
		HealthCheckInterval: 2 * time.Minute,
	}
}

// Pool manages a pool of SSH connections for reuse.
type Pool struct {
	mu       sync.Mutex
	clients  map[string]*pooledClient
	config   PoolConfig
	cleanupT *time.Ticker
	healthT  *time.Ticker
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

type pooledClient struct {
	client   *Client
	lastUsed time.Time
}

// NewPool creates a new connection pool with the given max idle time.
// Use 0 for default idle time (5 minutes).
func NewPool(maxIdleTime time.Duration) *Pool {
	cfg := DefaultPoolConfig()
	if maxIdleTime > 0 {
		cfg.MaxIdleTime = maxIdleTime
	}
	return NewPoolWithConfig(context.Background(), cfg)
}

// NewPoolWithContext creates a new connection pool with a context for cancellation.
func NewPoolWithContext(ctx context.Context, maxIdleTime time.Duration) *Pool {
	cfg := DefaultPoolConfig()
	if maxIdleTime > 0 {
		cfg.MaxIdleTime = maxIdleTime
	}
	return NewPoolWithConfig(ctx, cfg)
}

// NewPoolWithConfig creates a new connection pool with full configuration.
func NewPoolWithConfig(ctx context.Context, cfg PoolConfig) *Pool {
	// Apply defaults for zero values
	if cfg.MaxIdleTime == 0 {
		cfg.MaxIdleTime = 5 * time.Minute
	}
	if cfg.CleanupInterval == 0 {
		cfg.CleanupInterval = time.Minute
	}
	if cfg.MaxSize == 0 {
		cfg.MaxSize = 100
	}
	if cfg.HealthCheckInterval == 0 {
		cfg.HealthCheckInterval = 2 * time.Minute
	}

	poolCtx, poolCancel := context.WithCancel(ctx)

	p := &Pool{
		clients: make(map[string]*pooledClient),
		config:  cfg,
		ctx:     poolCtx,
		cancel:  poolCancel,
	}

	// Start cleanup goroutine
	p.cleanupT = time.NewTicker(cfg.CleanupInterval)
	p.wg.Add(1)
	go p.cleanupLoop()

	// Start health check goroutine
	p.healthT = time.NewTicker(cfg.HealthCheckInterval)
	p.wg.Add(1)
	go p.healthCheckLoop()

	return p
}

// Get retrieves or creates an SSH client for the given config.
// If a cached connection exists but is dead, it will be replaced with a new one.
//
// Host key verification behavior:
//   - If cfg.ExpectedHostKey is set, the host key will be verified against it
//   - If cfg.SkipHostKeyVerification is true, verification is skipped (insecure)
//   - Otherwise, new keys are accepted (TOFU behavior)
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
	// Uses the config's host key verification settings
	client, _, err := New(cfg)
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

	// Enforce max pool size - evict oldest idle connection if at limit
	if len(p.clients) >= p.config.MaxSize {
		p.evictOldest()
	}

	p.clients[key] = &pooledClient{
		client:   client,
		lastUsed: time.Now(),
	}
	return nil
}

// evictOldest removes the oldest (least recently used) connection from the pool.
// Must be called with mu held.
func (p *Pool) evictOldest() {
	var oldestKey string
	var oldestTime time.Time

	for key, pc := range p.clients {
		if oldestKey == "" || pc.lastUsed.Before(oldestTime) {
			oldestKey = key
			oldestTime = pc.lastUsed
		}
	}

	if oldestKey != "" {
		if pc, ok := p.clients[oldestKey]; ok {
			delete(p.clients, oldestKey)
			// Close asynchronously to avoid blocking
			go pc.client.Close()
		}
	}
}

// Release marks a client as no longer in use.
// The client remains in the pool for reuse.
func (p *Pool) Release(_ *Client) {
	// Currently a no-op since we keep connections alive
	// Could implement reference counting if needed
}

// Close closes all connections and stops the pool.
func (p *Pool) Close() error {
	// Cancel context to signal goroutines to stop
	p.cancel()

	// Stop tickers
	p.cleanupT.Stop()
	p.healthT.Stop()

	// Wait for goroutines to finish
	p.wg.Wait()

	// Now close all connections
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

// InvalidateHost removes all connections to a specific host:port.
// Use this when a host's key has changed and connections need to be re-established.
func (p *Pool) InvalidateHost(host string, port int) {
	suffix := fmt.Sprintf("@%s:%d", host, port)

	p.mu.Lock()
	var toClose []*Client
	for key, pc := range p.clients {
		// Check if the key ends with @host:port
		if len(key) > len(suffix) && key[len(key)-len(suffix):] == suffix {
			toClose = append(toClose, pc.client)
			delete(p.clients, key)
		}
	}
	p.mu.Unlock()

	// Close connections outside the lock
	for _, client := range toClose {
		if client != nil {
			client.Close()
		}
	}
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
	defer p.wg.Done()
	for {
		select {
		case <-p.ctx.Done():
			return
		case <-p.cleanupT.C:
			p.cleanup()
		}
	}
}

func (p *Pool) healthCheckLoop() {
	defer p.wg.Done()
	for {
		select {
		case <-p.ctx.Done():
			return
		case <-p.healthT.C:
			p.healthCheck()
		}
	}
}

func (p *Pool) cleanup() {
	// Collect stale clients while holding the lock
	var toClose []*Client

	p.mu.Lock()
	now := time.Now()
	for key, pc := range p.clients {
		if now.Sub(pc.lastUsed) > p.config.MaxIdleTime {
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

func (p *Pool) healthCheck() {
	// Check health of all connections and remove dead ones
	var toClose []*Client

	p.mu.Lock()
	for key, pc := range p.clients {
		if !pc.client.IsAlive() {
			toClose = append(toClose, pc.client)
			delete(p.clients, key)
		}
	}
	p.mu.Unlock()

	// Close dead connections outside the lock
	for _, client := range toClose {
		if client != nil {
			client.Close()
		}
	}
}

func (p *Pool) configKey(cfg *Config) string {
	// Include a hash of the private key path to ensure different keys for the same host
	// don't share connections (security issue if they did).
	// We use the path since different paths imply different keys.
	keyHash := ""
	if cfg.PrivateKeyPath != "" {
		hash := sha256.Sum256([]byte(cfg.PrivateKeyPath))
		keyHash = hex.EncodeToString(hash[:8]) // First 8 bytes is enough for uniqueness
	}
	return fmt.Sprintf("%s@%s:%d:%s", cfg.Username, cfg.Host, cfg.Port, keyHash)
}

// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	// DefaultLogBufferSize is the default number of log entries to keep in the ring buffer.
	DefaultLogBufferSize = 500
	// DefaultLogSubscriberBuffer is the buffer size for each subscriber's channel.
	DefaultLogSubscriberBuffer = 100
	// DefaultLogPollInterval is the default interval between log polls.
	DefaultLogPollInterval = 2 * time.Second
	// logPollTimeout is the timeout for each log poll request.
	logPollTimeout = 10 * time.Second
)

// InstanceLogHub manages log streaming from a qBittorrent instance.
// It polls the qBittorrent API when there are active subscribers and
// broadcasts new log entries to all subscribers.
type InstanceLogHub struct {
	instanceID int
	host       string
	username   string
	password   string
	basicUser *string
	basicPass *string

	mu            sync.RWMutex
	buffer        []LogEntry
	bufferSize    int
	writePos      int
	count         int
	subscribers   map[*LogSubscriber]struct{}
	lastKnownID   int64
	pollInterval  time.Duration
	httpClient    *http.Client
	authenticated bool

	// Polling control
	pollCtx    context.Context
	pollCancel context.CancelFunc
	polling    bool
}

// LogSubscriber represents a subscriber to the log stream.
type LogSubscriber struct {
	ch     chan LogEntry
	ctx    context.Context
	cancel context.CancelFunc
}

// InstanceLogHubConfig holds configuration for creating an InstanceLogHub.
type InstanceLogHubConfig struct {
	InstanceID    int
	Host          string
	Username      string
	Password      string
	BasicUsername *string
	BasicPassword *string
	TLSSkipVerify bool
	BufferSize    int
	PollInterval  time.Duration
}

// NewInstanceLogHub creates a new InstanceLogHub for streaming logs from a qBittorrent instance.
func NewInstanceLogHub(cfg InstanceLogHubConfig) *InstanceLogHub {
	bufferSize := cfg.BufferSize
	if bufferSize <= 0 {
		bufferSize = DefaultLogBufferSize
	}

	pollInterval := cfg.PollInterval
	if pollInterval <= 0 {
		pollInterval = DefaultLogPollInterval
	}

	jar, _ := cookiejar.New(nil) // nil options never returns an error

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: cfg.TLSSkipVerify, //nolint:gosec // User-configurable TLS skip
		},
	}

	return &InstanceLogHub{
		instanceID:   cfg.InstanceID,
		host:         cfg.Host,
		username:     cfg.Username,
		password:     cfg.Password,
		basicUser:    cfg.BasicUsername,
		basicPass:    cfg.BasicPassword,
		buffer:       make([]LogEntry, bufferSize),
		bufferSize:   bufferSize,
		subscribers:  make(map[*LogSubscriber]struct{}),
		lastKnownID:  -1,
		pollInterval: pollInterval,
		httpClient: &http.Client{
			Jar:       jar,
			Transport: transport,
			Timeout:   30 * time.Second,
		},
	}
}

// Subscribe creates a new subscriber that receives log entries.
func (h *InstanceLogHub) Subscribe(ctx context.Context) *LogSubscriber {
	subCtx, cancel := context.WithCancel(ctx)
	sub := &LogSubscriber{
		ch:     make(chan LogEntry, DefaultLogSubscriberBuffer),
		ctx:    subCtx,
		cancel: cancel,
	}

	h.mu.Lock()
	h.subscribers[sub] = struct{}{}
	shouldStartPolling := len(h.subscribers) == 1 && !h.polling
	h.mu.Unlock()

	// Start polling if this is the first subscriber
	if shouldStartPolling {
		h.startPolling()
	}

	// Auto-unsubscribe when context is done
	go func() {
		<-subCtx.Done()
		h.Unsubscribe(sub)
	}()

	return sub
}

// Unsubscribe removes a subscriber and closes its channel.
func (h *InstanceLogHub) Unsubscribe(sub *LogSubscriber) {
	h.mu.Lock()
	if _, ok := h.subscribers[sub]; ok {
		delete(h.subscribers, sub)
		sub.cancel()
		close(sub.ch)
	}
	shouldStopPolling := len(h.subscribers) == 0 && h.polling
	h.mu.Unlock()

	// Stop polling if no more subscribers
	if shouldStopPolling {
		h.stopPolling()
	}
}

// Channel returns the subscriber's log entry channel.
func (s *LogSubscriber) Channel() <-chan LogEntry {
	return s.ch
}

// History returns the last n log entries from the ring buffer.
func (h *InstanceLogHub) History(n int) []LogEntry {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.count == 0 {
		return nil
	}
	if n <= 0 || n > h.count {
		n = h.count
	}

	result := make([]LogEntry, n)
	start := (h.writePos - n + h.bufferSize) % h.bufferSize
	for i := 0; i < n; i++ {
		result[i] = h.buffer[(start+i)%h.bufferSize]
	}
	return result
}

// SubscriberCount returns the current number of subscribers.
func (h *InstanceLogHub) SubscriberCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subscribers)
}

// startPolling begins the background polling loop.
func (h *InstanceLogHub) startPolling() {
	h.mu.Lock()
	if h.polling {
		h.mu.Unlock()
		return
	}
	h.polling = true
	h.pollCtx, h.pollCancel = context.WithCancel(context.Background())
	ctx := h.pollCtx
	h.mu.Unlock()

	go h.pollLoop(ctx)
}

// stopPolling stops the background polling loop.
func (h *InstanceLogHub) stopPolling() {
	h.mu.Lock()
	if !h.polling {
		h.mu.Unlock()
		return
	}
	h.polling = false
	if h.pollCancel != nil {
		h.pollCancel()
	}
	h.mu.Unlock()
}

// pollLoop continuously polls qBittorrent for new log entries.
func (h *InstanceLogHub) pollLoop(ctx context.Context) {
	ticker := time.NewTicker(h.pollInterval)
	defer ticker.Stop()

	// Initial fetch
	h.fetchAndBroadcast(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.fetchAndBroadcast(ctx)
		}
	}
}

// fetchAndBroadcast fetches new logs and broadcasts them to subscribers.
func (h *InstanceLogHub) fetchAndBroadcast(ctx context.Context) {
	// Check if we need to authenticate (protected read)
	h.mu.RLock()
	needsAuth := !h.authenticated
	lastID := h.lastKnownID
	h.mu.RUnlock()

	if needsAuth {
		if err := h.login(ctx); err != nil {
			log.Warn().Err(err).Int("instanceID", h.instanceID).Msg("Failed to authenticate for log streaming")
			return
		}
	}

	entries, err := h.fetchLogs(ctx, lastID)
	if err != nil {
		log.Warn().Err(err).Int("instanceID", h.instanceID).Msg("Failed to fetch logs")
		// If we get an auth error, reset authenticated state and clear cookies
		if isAuthError(err) {
			h.mu.Lock()
			h.authenticated = false
			// Clear old cookies for clean re-authentication
			jar, _ := cookiejar.New(nil)
			h.httpClient.Jar = jar
			h.mu.Unlock()
		}
		return
	}

	if len(entries) == 0 {
		return
	}

	h.mu.Lock()
	for _, entry := range entries {
		// Update buffer
		h.buffer[h.writePos] = entry
		h.writePos = (h.writePos + 1) % h.bufferSize
		if h.count < h.bufferSize {
			h.count++
		}

		// Update last known ID
		if entry.ID > h.lastKnownID {
			h.lastKnownID = entry.ID
		}

		// Broadcast to subscribers (non-blocking)
		for sub := range h.subscribers {
			select {
			case sub.ch <- entry:
			default:
				// Drop entry for slow consumer
			}
		}
	}
	h.mu.Unlock()
}

// login authenticates with the qBittorrent instance.
func (h *InstanceLogHub) login(ctx context.Context) error {
	loginURL := fmt.Sprintf("%s/api/v2/auth/login", h.host)

	form := url.Values{}
	form.Set("username", h.username)
	form.Set("password", h.password)

	// Use POST body instead of URL query to avoid credential leakage in logs
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, loginURL, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("failed to create login request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.setBasicAuth(req)

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("login request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("login failed with status: %d", resp.StatusCode)
	}

	h.mu.Lock()
	h.authenticated = true
	h.mu.Unlock()

	log.Debug().Int("instanceID", h.instanceID).Msg("Successfully authenticated for log streaming")
	return nil
}

// fetchLogs fetches logs from qBittorrent with the given last_known_id.
func (h *InstanceLogHub) fetchLogs(ctx context.Context, lastKnownID int64) ([]LogEntry, error) {
	ctx, cancel := context.WithTimeout(ctx, logPollTimeout)
	defer cancel()

	logURL := fmt.Sprintf("%s/api/v2/log/main", h.host)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, logURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create log request: %w", err)
	}

	// Add query parameters
	q := req.URL.Query()
	q.Set("normal", "true")
	q.Set("info", "true")
	q.Set("warning", "true")
	q.Set("critical", "true")
	q.Set("last_known_id", strconv.FormatInt(lastKnownID, 10))
	req.URL.RawQuery = q.Encode()
	h.setBasicAuth(req)

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("log request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden {
		return nil, &authError{status: resp.StatusCode}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("log request failed with status: %d", resp.StatusCode)
	}

	var rawEntries []RawLogEntry
	if err := json.NewDecoder(resp.Body).Decode(&rawEntries); err != nil {
		return nil, fmt.Errorf("failed to decode log response: %w", err)
	}

	entries := make([]LogEntry, len(rawEntries))
	for i, raw := range rawEntries {
		entries[i] = raw.ToLogEntry()
	}

	return entries, nil
}

// Close stops polling and releases resources.
func (h *InstanceLogHub) Close() {
	h.mu.Lock()
	// Stop polling
	if h.polling {
		h.polling = false
		if h.pollCancel != nil {
			h.pollCancel()
		}
	}
	// Close all subscribers
	for sub := range h.subscribers {
		sub.cancel()
		close(sub.ch)
		delete(h.subscribers, sub)
	}
	h.mu.Unlock()
}

// authError represents an authentication error.
type authError struct {
	status int
}

func (e *authError) Error() string {
	return fmt.Sprintf("authentication error: status %d", e.status)
}

// isAuthError checks if the error is an authentication error.
func isAuthError(err error) bool {
	var authErr *authError
	return errors.As(err, &authErr)
}

// setBasicAuth adds basic auth to the request if configured.
func (h *InstanceLogHub) setBasicAuth(req *http.Request) {
	if h.basicUser == nil || *h.basicUser == "" {
		return
	}
	pass := ""
	if h.basicPass != nil {
		pass = *h.basicPass
	}
	req.SetBasicAuth(*h.basicUser, pass)
}

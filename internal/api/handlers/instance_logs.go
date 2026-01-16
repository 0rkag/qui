// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/autobrr/qui/internal/qbittorrent"
)

// InstanceLogsHandler handles qBittorrent instance log streaming.
type InstanceLogsHandler struct {
	logStreamManager *qbittorrent.InstanceLogStreamManager
}

// NewInstanceLogsHandler creates a new InstanceLogsHandler.
func NewInstanceLogsHandler(logStreamManager *qbittorrent.InstanceLogStreamManager) *InstanceLogsHandler {
	return &InstanceLogsHandler{
		logStreamManager: logStreamManager,
	}
}

// StreamLogs streams log entries from a qBittorrent instance via SSE.
func (h *InstanceLogsHandler) StreamLogs(w http.ResponseWriter, r *http.Request) {
	instanceID, err := h.parseInstanceID(r)
	if err != nil {
		http.Error(w, "Invalid instance ID", http.StatusBadRequest)
		return
	}

	limit := h.parseLimit(r)

	flusher, hub, err := h.prepareSSE(w, r.Context(), instanceID)
	if err != nil {
		if errors.Is(err, errStreamingNotSupported) {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		} else {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
		return
	}

	sub := hub.Subscribe(r.Context())
	defer hub.Unsubscribe(sub)

	if err := h.sendHistory(w, flusher, hub, limit); err != nil {
		return
	}

	h.streamLoop(w, flusher, r.Context(), sub)
}

func (h *InstanceLogsHandler) parseInstanceID(r *http.Request) (int, error) {
	idStr := chi.URLParam(r, "instanceID")
	return strconv.Atoi(idStr)
}

func (h *InstanceLogsHandler) parseLimit(r *http.Request) int {
	limit := 100 // Default limit for instance logs
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	return limit
}

func (h *InstanceLogsHandler) prepareSSE(w http.ResponseWriter, ctx context.Context, instanceID int) (http.Flusher, *qbittorrent.InstanceLogHub, error) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, nil, errStreamingNotSupported
	}

	hub, err := h.logStreamManager.GetHub(ctx, instanceID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get log hub: %w", err)
	}

	return flusher, hub, nil
}

func (h *InstanceLogsHandler) sendHistory(w http.ResponseWriter, flusher http.Flusher, hub *qbittorrent.InstanceLogHub, limit int) error {
	history := hub.History(limit)
	for _, entry := range history {
		if err := writeInstanceLogSSEData(w, entry); err != nil {
			return err
		}
	}
	flusher.Flush()
	return nil
}

func (h *InstanceLogsHandler) streamLoop(w http.ResponseWriter, flusher http.Flusher, ctx context.Context, sub *qbittorrent.LogSubscriber) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case entry, ok := <-sub.Channel():
			if !ok {
				return
			}
			if err := writeInstanceLogSSEData(w, entry); err != nil {
				return
			}
			flusher.Flush()
		case <-ticker.C:
			if err := writeSSEComment(w, "keepalive"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func writeInstanceLogSSEData(w http.ResponseWriter, entry qbittorrent.LogEntry) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "data: %s\n\n", data)
	return err //nolint:wrapcheck // SSE write errors are terminal; wrapping adds no value
}

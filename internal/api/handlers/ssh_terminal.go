// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/sshclient"
)

// SSHTerminalHandler handles WebSocket-based SSH terminal sessions.
type SSHTerminalHandler struct {
	store *models.InstanceConnectionStore
}

// NewSSHTerminalHandler creates a new SSHTerminalHandler.
func NewSSHTerminalHandler(store *models.InstanceConnectionStore) *SSHTerminalHandler {
	return &SSHTerminalHandler{store: store}
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for easter egg feature
	},
}

// TerminalMessage represents messages sent over the WebSocket.
type TerminalMessage struct {
	Type string `json:"type"` // "input", "resize", "ping"
	Data string `json:"data,omitempty"`
	Cols uint32 `json:"cols,omitempty"`
	Rows uint32 `json:"rows,omitempty"`
}

// HandleTerminal handles GET /api/instances/{instanceID}/connections/{id}/terminal
// Upgrades the connection to WebSocket and establishes an SSH shell session.
func (h *SSHTerminalHandler) HandleTerminal(w http.ResponseWriter, r *http.Request) {
	instanceID, ok := parseIntParam(w, r, "instanceID", "Invalid instance ID")
	if !ok {
		return
	}

	id, ok := parseInt64Param(w, r, "id", "Invalid connection ID")
	if !ok {
		return
	}

	// Get the connection
	conn, err := h.store.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, models.ErrConnectionNotFound) {
			RespondError(w, http.StatusNotFound, "Connection not found")
			return
		}
		log.Error().Err(err).Int64("id", id).Msg("terminal: failed to get connection")
		RespondError(w, http.StatusInternalServerError, "Failed to get connection")
		return
	}

	// Verify the connection belongs to the specified instance
	if conn.InstanceID != instanceID {
		RespondError(w, http.StatusNotFound, "Connection not found")
		return
	}

	// Only SSH connections support terminal
	if conn.Protocol != models.ProtocolSSH && conn.Protocol != models.ProtocolSFTP {
		RespondError(w, http.StatusBadRequest, "Terminal only supported for SSH/SFTP connections")
		return
	}

	// Upgrade to WebSocket
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Error().Err(err).Msg("terminal: websocket upgrade failed")
		return
	}
	defer ws.Close()

	// Create SSH client
	cfg := &sshclient.Config{
		Host:           conn.Host,
		Port:           conn.Port,
		Username:       conn.Username,
		PrivateKeyPath: conn.PrivateKeyPath,
	}

	client, err := sshclient.New(cfg)
	if err != nil {
		h.sendError(ws, "SSH connection failed: "+err.Error())
		return
	}
	defer client.Close()

	// Default terminal size
	cols, rows := uint32(80), uint32(24)

	// Create shell session
	shell, err := client.Shell(cols, rows)
	if err != nil {
		h.sendError(ws, "Failed to start shell: "+err.Error())
		return
	}
	defer shell.Close()

	log.Info().
		Str("host", conn.Host).
		Int("port", conn.Port).
		Str("user", conn.Username).
		Msg("terminal: SSH session started")

	// Use a WaitGroup to track goroutines
	var wg sync.WaitGroup
	done := make(chan struct{})

	// SSH stdout → WebSocket
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 1024)
		for {
			select {
			case <-done:
				return
			default:
				n, err := shell.Stdout.Read(buf)
				if err != nil {
					if err != io.EOF {
						log.Debug().Err(err).Msg("terminal: stdout read error")
					}
					return
				}
				if n > 0 {
					if err := ws.WriteMessage(websocket.TextMessage, buf[:n]); err != nil {
						log.Debug().Err(err).Msg("terminal: websocket write error")
						return
					}
				}
			}
		}
	}()

	// SSH stderr → WebSocket
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 1024)
		for {
			select {
			case <-done:
				return
			default:
				n, err := shell.Stderr.Read(buf)
				if err != nil {
					if err != io.EOF {
						log.Debug().Err(err).Msg("terminal: stderr read error")
					}
					return
				}
				if n > 0 {
					if err := ws.WriteMessage(websocket.TextMessage, buf[:n]); err != nil {
						log.Debug().Err(err).Msg("terminal: websocket write error")
						return
					}
				}
			}
		}
	}()

	// WebSocket → SSH stdin (main loop)
	for {
		messageType, message, err := ws.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Debug().Err(err).Msg("terminal: websocket read error")
			}
			break
		}

		if messageType == websocket.TextMessage {
			var msg TerminalMessage
			if err := json.Unmarshal(message, &msg); err != nil {
				// Treat as raw input if not JSON
				if _, err := shell.Stdin.Write(message); err != nil {
					log.Debug().Err(err).Msg("terminal: stdin write error")
					break
				}
				continue
			}

			switch msg.Type {
			case "input":
				if _, err := shell.Stdin.Write([]byte(msg.Data)); err != nil {
					log.Debug().Err(err).Msg("terminal: stdin write error")
					break
				}
			case "resize":
				if msg.Cols > 0 && msg.Rows > 0 {
					if err := shell.Resize(msg.Cols, msg.Rows); err != nil {
						log.Debug().Err(err).Msg("terminal: resize error")
					}
				}
			case "ping":
				// Keep-alive, no action needed
			}
		}
	}

	// Signal goroutines to stop and wait
	close(done)
	wg.Wait()

	log.Info().
		Str("host", conn.Host).
		Msg("terminal: SSH session closed")
}

// sendError sends an error message over the WebSocket and closes it.
func (h *SSHTerminalHandler) sendError(ws *websocket.Conn, msg string) {
	errMsg := struct {
		Type  string `json:"type"`
		Error string `json:"error"`
	}{
		Type:  "error",
		Error: msg,
	}
	data, _ := json.Marshal(errMsg)
	ws.WriteMessage(websocket.TextMessage, data)
	ws.Close()
}

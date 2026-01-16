// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"time"
)

// LogLevel represents the severity level of a qBittorrent log entry.
type LogLevel string

const (
	LogLevelNormal   LogLevel = "normal"
	LogLevelInfo     LogLevel = "info"
	LogLevelWarning  LogLevel = "warning"
	LogLevelCritical LogLevel = "critical"
)

// LogType represents the numeric type from qBittorrent API.
// These are bitmask values: 1=normal, 2=info, 4=warning, 8=critical
type LogType int

const (
	LogTypeNormal   LogType = 1
	LogTypeInfo     LogType = 2
	LogTypeWarning  LogType = 4
	LogTypeCritical LogType = 8
)

// LogEntry represents a single log entry from qBittorrent.
type LogEntry struct {
	ID        int64     `json:"id"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
	Type      LogType   `json:"type"`
	Level     LogLevel  `json:"level"`
}

// RawLogEntry represents the raw log entry from qBittorrent API.
type RawLogEntry struct {
	ID        int64  `json:"id"`
	Message   string `json:"message"`
	Timestamp int64  `json:"timestamp"` // Unix milliseconds
	Type      int64  `json:"type"`
}

// ToLogEntry converts a raw log entry to a structured LogEntry.
func (r *RawLogEntry) ToLogEntry() LogEntry {
	logType := LogType(r.Type)
	return LogEntry{
		ID:        r.ID,
		Message:   r.Message,
		Timestamp: time.UnixMilli(r.Timestamp),
		Type:      logType,
		Level:     logTypeToLevel(logType),
	}
}

// logTypeToLevel converts a numeric log type to a LogLevel string.
func logTypeToLevel(t LogType) LogLevel {
	switch t {
	case LogTypeNormal:
		return LogLevelNormal
	case LogTypeInfo:
		return LogLevelInfo
	case LogTypeWarning:
		return LogLevelWarning
	case LogTypeCritical:
		return LogLevelCritical
	default:
		return LogLevelNormal
	}
}

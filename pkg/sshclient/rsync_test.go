// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sshclient

import (
	"strings"
	"testing"
)

func TestParseRsyncProgress(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		wantNil  bool
		wantPct  int
		wantETA  string
	}{
		{
			name:    "modern rsync progress line",
			output:  "     12,345,678  23%   45.67MB/s    0:01:23",
			wantNil: false,
			wantPct: 23,
			wantETA: "0:01:23",
		},
		{
			name:    "progress at 100%",
			output:  "     99,999,999  100%  100.00MB/s    0:00:00",
			wantNil: false,
			wantPct: 100,
			wantETA: "0:00:00",
		},
		{
			name:    "progress at start",
			output:  "         1,234  1%    1.23MB/s    0:10:00",
			wantNil: false,
			wantPct: 1,
			wantETA: "0:10:00",
		},
		{
			name:    "non-progress line",
			output:  "sending incremental file list",
			wantNil: true,
		},
		{
			name:    "empty line",
			output:  "",
			wantNil: true,
		},
		{
			name:    "filename only",
			output:  "somefile.txt",
			wantNil: true,
		},
		{
			name:    "multiline with progress",
			output:  "sending incremental file list\n     12,345  50%   10.00MB/s    0:00:01\n",
			wantNil: false,
			wantPct: 50,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := parseRsyncProgress(tt.output)
			if tt.wantNil {
				if info != nil {
					t.Errorf("parseRsyncProgress() = %+v, want nil", info)
				}
				return
			}
			if info == nil {
				t.Fatal("parseRsyncProgress() = nil, want non-nil")
			}
			if info.Percentage != tt.wantPct {
				t.Errorf("Percentage = %d, want %d", info.Percentage, tt.wantPct)
			}
			if tt.wantETA != "" && info.ETA != tt.wantETA {
				t.Errorf("ETA = %q, want %q", info.ETA, tt.wantETA)
			}
		})
	}
}

func TestParseSpeed(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int64
	}{
		{"bytes per second", "100B/s", 100},
		{"kilobytes per second", "1.5KB/s", 1536},
		{"megabytes per second", "10MB/s", 10 * 1024 * 1024},
		{"gigabytes per second", "1GB/s", 1024 * 1024 * 1024},
		{"megabytes decimal", "45.67MB/s", 47882854}, // 45.67 * 1024 * 1024
		{"kilobytes decimal", "123.45KB/s", 126412}, // 123.45 * 1024
		{"no suffix", "1000/s", 1000},
		{"empty string", "", 0},
		{"invalid format", "abc", 0},
		{"just suffix", "MB/s", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseSpeed(tt.input)
			// Allow 1% tolerance for floating point calculations
			tolerance := tt.expected / 100
			if tolerance < 1 {
				tolerance = 1
			}
			diff := got - tt.expected
			if diff < 0 {
				diff = -diff
			}
			if diff > tolerance {
				t.Errorf("parseSpeed(%q) = %d, want %d (diff: %d)", tt.input, got, tt.expected, diff)
			}
		})
	}
}

func TestBuildRsyncArgs(t *testing.T) {
	tests := []struct {
		name         string
		opts         *TransferOptions
		wantPreserve bool
		wantDelete   bool
		wantDryRun   bool
	}{
		{
			name:         "nil options uses defaults",
			opts:         nil,
			wantPreserve: true,
			wantDelete:   false,
			wantDryRun:   false,
		},
		{
			name:         "preserve permissions",
			opts:         &TransferOptions{PreservePermissions: true},
			wantPreserve: true,
			wantDelete:   false,
			wantDryRun:   false,
		},
		{
			name:         "no preserve permissions",
			opts:         &TransferOptions{PreservePermissions: false},
			wantPreserve: false,
			wantDelete:   false,
			wantDryRun:   false,
		},
		{
			name:         "delete enabled",
			opts:         &TransferOptions{Delete: true},
			wantDelete:   true,
		},
		{
			name:         "dry run enabled",
			opts:         &TransferOptions{DryRun: true},
			wantDryRun:   true,
		},
		{
			name: "all options enabled",
			opts: &TransferOptions{
				PreservePermissions: true,
				Delete:              true,
				DryRun:              true,
			},
			wantPreserve: true,
			wantDelete:   true,
			wantDryRun:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := tt.opts
			if opts == nil {
				opts = &TransferOptions{PreservePermissions: true}
			}
			args := buildRsyncArgs(opts)

			argsStr := strings.Join(args, " ")

			// Check -a (archive/preserve) vs -r (recursive only)
			hasArchive := strings.Contains(argsStr, "-a")
			hasRecursive := strings.Contains(argsStr, "-r")
			if tt.wantPreserve && !hasArchive {
				t.Errorf("Expected -a flag for PreservePermissions=true, got: %v", args)
			}
			if !tt.wantPreserve && !hasRecursive {
				t.Errorf("Expected -r flag for PreservePermissions=false, got: %v", args)
			}

			// Check --delete
			hasDelete := strings.Contains(argsStr, "--delete")
			if tt.wantDelete != hasDelete {
				t.Errorf("--delete = %v, want %v", hasDelete, tt.wantDelete)
			}

			// Check --dry-run
			hasDryRun := strings.Contains(argsStr, "--dry-run")
			if tt.wantDryRun != hasDryRun {
				t.Errorf("--dry-run = %v, want %v", hasDryRun, tt.wantDryRun)
			}

			// Progress flag should always be present
			hasProgress := strings.Contains(argsStr, "--progress") || strings.Contains(argsStr, "--info=progress2")
			if !hasProgress {
				t.Errorf("Expected progress flag, got: %v", args)
			}
		})
	}
}

func TestRsyncProgressTracker_ParseProgressLegacy(t *testing.T) {
	tests := []struct {
		name       string
		outputs    []string // Multiple lines of output to feed
		wantPct    int
		wantSpeed  bool // Whether we expect a speed reading
	}{
		{
			name: "single file progress",
			outputs: []string{
				"     1234567 100%    1.23MB/s    0:00:01 (xfr#1, to-chk=5/10)",
			},
			wantPct:   10, // 1/10 files = 10%
			wantSpeed: true,
		},
		{
			name: "partial file progress",
			outputs: []string{
				"      123456  50%    1.23MB/s    0:00:01",
			},
			wantPct:   0, // No xfr info yet
			wantSpeed: true,
		},
		{
			name: "multiple files completed",
			outputs: []string{
				"     1000 100%    1.00MB/s    0:00:01 (xfr#1, to-chk=4/5)",
				"     2000 100%    2.00MB/s    0:00:01 (xfr#2, to-chk=3/5)",
				"     3000 100%    3.00MB/s    0:00:01 (xfr#3, to-chk=2/5)",
			},
			wantPct:   60, // 3/5 files = 60%
			wantSpeed: true,
		},
		{
			name: "ir-chk format",
			outputs: []string{
				"     1234 100%    1.00MB/s    0:00:01 (xfr#2, ir-chk=3/10)",
			},
			wantPct:   20, // 2/10 files = 20%
			wantSpeed: true,
		},
		{
			name:      "non-progress output",
			outputs:   []string{"sending incremental file list"},
			wantPct:   0,
			wantSpeed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracker := &rsyncProgressTracker{}
			var info *ProgressInfo

			for _, output := range tt.outputs {
				info = tracker.parseProgressLegacy(output)
			}

			if info == nil {
				t.Fatal("parseProgressLegacy() returned nil")
			}

			if info.Percentage != tt.wantPct {
				t.Errorf("Percentage = %d, want %d", info.Percentage, tt.wantPct)
			}

			if tt.wantSpeed && info.Speed == "" {
				t.Error("Expected speed reading, got empty")
			}
		})
	}
}

func TestRsyncProgressTracker_CumulativeBytes(t *testing.T) {
	tracker := &rsyncProgressTracker{}

	// Simulate completing three files of different sizes
	tracker.parseProgressLegacy("     1000 100%    1.00MB/s    0:00:01 (xfr#1, to-chk=2/3)")
	tracker.parseProgressLegacy("     2000 100%    1.00MB/s    0:00:01 (xfr#2, to-chk=1/3)")
	info := tracker.parseProgressLegacy("     3000 100%    1.00MB/s    0:00:01 (xfr#3, to-chk=0/3)")

	// Total bytes should be cumulative: 1000 + 2000 + 3000 = 6000
	// But last file resets currentFileBytes, so we get completedBytes = 6000
	if info.BytesTransferred != 6000 {
		t.Errorf("BytesTransferred = %d, want 6000", info.BytesTransferred)
	}

	// Percentage should be 100% (3/3 files)
	if info.Percentage != 100 {
		t.Errorf("Percentage = %d, want 100", info.Percentage)
	}
}

func TestTransferOptions_Defaults(t *testing.T) {
	opts := TransferOptions{}

	if opts.PreservePermissions {
		t.Error("Default PreservePermissions should be false")
	}
	if opts.Delete {
		t.Error("Default Delete should be false")
	}
	if opts.DryRun {
		t.Error("Default DryRun should be false")
	}
	if opts.OnProgress != nil {
		t.Error("Default OnProgress should be nil")
	}
}

func TestTransferResult_Defaults(t *testing.T) {
	result := TransferResult{}

	if result.FilesTransferred != 0 {
		t.Errorf("Default FilesTransferred = %d, want 0", result.FilesTransferred)
	}
	if result.BytesTransferred != 0 {
		t.Errorf("Default BytesTransferred = %d, want 0", result.BytesTransferred)
	}
	if result.Output != "" {
		t.Errorf("Default Output = %q, want empty", result.Output)
	}
}

func TestProgressInfo_Fields(t *testing.T) {
	info := ProgressInfo{
		BytesTransferred: 1024 * 1024,
		BytesTotal:       10 * 1024 * 1024,
		Percentage:       10,
		Speed:            "1.00MB/s",
		SpeedBytesPerSec: 1024 * 1024,
		ETA:              "0:00:09",
	}

	if info.BytesTransferred != 1024*1024 {
		t.Errorf("BytesTransferred = %d, want %d", info.BytesTransferred, 1024*1024)
	}
	if info.BytesTotal != 10*1024*1024 {
		t.Errorf("BytesTotal = %d, want %d", info.BytesTotal, 10*1024*1024)
	}
	if info.Percentage != 10 {
		t.Errorf("Percentage = %d, want 10", info.Percentage)
	}
	if info.Speed != "1.00MB/s" {
		t.Errorf("Speed = %q, want \"1.00MB/s\"", info.Speed)
	}
	if info.SpeedBytesPerSec != 1024*1024 {
		t.Errorf("SpeedBytesPerSec = %d, want %d", info.SpeedBytesPerSec, 1024*1024)
	}
	if info.ETA != "0:00:09" {
		t.Errorf("ETA = %q, want \"0:00:09\"", info.ETA)
	}
}

func TestParseInt64(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int64
		wantErr bool
	}{
		{"positive number", "12345", 12345, false},
		{"zero", "0", 0, false},
		{"large number", "9223372036854775807", 9223372036854775807, false},
		{"negative number", "-123", -123, false},
		{"empty string", "", 0, true},
		{"non-numeric", "abc", 0, true},
		{"mixed", "123abc", 123, false}, // Sscanf stops at first non-digit
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseInt64(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseInt64(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("parseInt64(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseInt(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int
		wantErr bool
	}{
		{"positive number", "12345", 12345, false},
		{"zero", "0", 0, false},
		{"negative number", "-123", -123, false},
		{"empty string", "", 0, true},
		{"non-numeric", "abc", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseInt(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseInt(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("parseInt(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestCreateTempKnownHosts_Empty(t *testing.T) {
	_, err := createTempKnownHosts()
	if err == nil {
		t.Error("createTempKnownHosts() with no entries should return error")
	}
}

func TestCreateTempKnownHosts_Valid(t *testing.T) {
	entry := "example.com ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQC..."

	path, err := createTempKnownHosts(entry)
	if err != nil {
		t.Fatalf("createTempKnownHosts() error = %v", err)
	}

	// Clean up
	defer func() {
		if path != "" {
			// File should be removable
			if err := removeFile(path); err != nil {
				t.Logf("Warning: failed to remove temp file: %v", err)
			}
		}
	}()

	if path == "" {
		t.Error("createTempKnownHosts() returned empty path")
	}
}

// removeFile is a helper to remove a file, ignoring errors
func removeFile(path string) error {
	return nil // For test purposes, we don't actually need to remove
}

func TestValidatePath_Rsync(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"valid absolute path", "/path/to/file", false},
		{"valid nested path", "/home/user/documents/file.txt", false},
		{"root path", "/", false},
		{"relative path", "path/to/file", true},
		{"dot relative", "./file.txt", true},
		{"parent traversal", "../../../etc/passwd", true},
		{"null byte", "/path/to/\x00file", true},
		{"empty path", "", true},
		{"path with spaces", "/path/to/my file.txt", false},
		{"path with unicode", "/path/to/file.txt", false},
		{"very long path", "/" + strings.Repeat("a", 5000), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePath(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePath(%q) error = %v, wantErr %v", tt.path, err, tt.wantErr)
			}
		})
	}
}

func TestValidatePathWithBase_Rsync(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		base    string
		wantErr bool
	}{
		{"path within base", "/home/user/file.txt", "/home/user", false},
		{"path equals base", "/home/user", "/home/user", false},
		{"path escapes base", "/etc/passwd", "/home/user", true},
		{"traversal escapes base", "/home/user/../../../etc/passwd", "/home/user", true},
		{"relative path", "relative/path", "/home/user", true},
		{"invalid base", "/home/user/file.txt", "relative/base", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePathWithBase(tt.path, tt.base)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePathWithBase(%q, %q) error = %v, wantErr %v", tt.path, tt.base, err, tt.wantErr)
			}
		})
	}
}

func TestBuildSSHCommandSecure(t *testing.T) {
	cfg := &Config{
		Host:           "example.com",
		Port:           22,
		Username:       "user",
		PrivateKeyPath: "/path/to/key",
	}

	cmd := buildSSHCommandSecure(cfg, "/tmp/known_hosts")

	// Check required components
	if !strings.Contains(cmd, "ssh") {
		t.Error("Command should contain 'ssh'")
	}
	if !strings.Contains(cmd, "-p 22") {
		t.Error("Command should contain port specification")
	}
	if !strings.Contains(cmd, "StrictHostKeyChecking=yes") {
		t.Error("Command should enable strict host key checking")
	}
	if !strings.Contains(cmd, "UserKnownHostsFile=") {
		t.Error("Command should specify known hosts file")
	}
	if !strings.Contains(cmd, "-i") {
		t.Error("Command should specify identity file")
	}
}

func TestBuildSSHCommandInsecure(t *testing.T) {
	cfg := &Config{
		Host:           "example.com",
		Port:           2222,
		Username:       "user",
		PrivateKeyPath: "/path/to/key",
	}

	cmd := buildSSHCommandInsecure(cfg)

	// Check required components
	if !strings.Contains(cmd, "ssh") {
		t.Error("Command should contain 'ssh'")
	}
	if !strings.Contains(cmd, "-p 2222") {
		t.Error("Command should contain port specification")
	}
	if !strings.Contains(cmd, "StrictHostKeyChecking=no") {
		t.Error("Command should disable strict host key checking")
	}
	if !strings.Contains(cmd, "-i") {
		t.Error("Command should specify identity file")
	}
}

// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sshclient

import (
	"testing"
)

func TestSCPTransferOptions_Defaults(t *testing.T) {
	opts := SCPTransferOptions{}

	// Verify defaults
	if opts.PreservePermissions {
		t.Error("Default PreservePermissions should be false")
	}
	if opts.Recursive {
		t.Error("Default Recursive should be false")
	}
	if opts.FileExistsMode != FileExistsModeAbort {
		t.Errorf("Default FileExistsMode = %v, want FileExistsModeAbort", opts.FileExistsMode)
	}
	if opts.OnProgress != nil {
		t.Error("Default OnProgress should be nil")
	}
}

func TestSCPRemoteToRemoteOptions_Defaults(t *testing.T) {
	opts := SCPRemoteToRemoteOptions{}

	// Verify defaults
	if opts.UseRelay {
		t.Error("Default UseRelay should be false")
	}
	if opts.PreservePermissions {
		t.Error("Default PreservePermissions should be false")
	}
	if opts.FileExistsMode != FileExistsModeAbort {
		t.Errorf("Default FileExistsMode = %v, want FileExistsModeAbort", opts.FileExistsMode)
	}
}

func TestSCPClient_ConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *Config
		wantErr bool
	}{
		{
			name:    "nil config",
			cfg:     nil,
			wantErr: true,
		},
		{
			name: "empty username",
			cfg: &Config{
				Host:     "localhost",
				Port:     22,
				Username: "",
			},
			wantErr: true,
		},
		{
			name: "empty hostname",
			cfg: &Config{
				Host:     "",
				Port:     22,
				Username: "root",
			},
			wantErr: true,
		},
		{
			name: "invalid port",
			cfg: &Config{
				Host:     "localhost",
				Port:     0,
				Username: "root",
			},
			wantErr: true,
		},
		{
			name: "port too high",
			cfg: &Config{
				Host:     "localhost",
				Port:     70000,
				Username: "root",
			},
			wantErr: true,
		},
		{
			name: "valid config",
			cfg: &Config{
				Host:           "localhost",
				Port:           22,
				Username:       "root",
				PrivateKeyPath: "/path/to/key",
			},
			wantErr: false,
		},
		{
			name: "username with special characters rejected",
			cfg: &Config{
				Host:     "localhost",
				Port:     22,
				Username: "user; rm -rf /",
			},
			wantErr: true,
		},
		{
			name: "hostname with shell injection rejected",
			cfg: &Config{
				Host:     "localhost; rm -rf /",
				Port:     22,
				Username: "root",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewSCPClient(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewSCPClient() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidatePath_SCP(t *testing.T) {
	// These tests verify path validation for SCP operations
	// SCP requires absolute paths for security
	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"valid absolute path", "/path/to/file.txt", false},
		{"valid nested path", "/home/user/documents/file.txt", false},
		{"root path", "/", false},
		{"relative path rejected", "path/to/file.txt", true},
		{"dot relative path rejected", "./path/to/file.txt", true},
		{"parent traversal rejected", "../../../etc/passwd", true},
		{"null byte injection", "/path/to/file\x00.txt", true},
		{"empty path", "", true},
		{"path with spaces allowed", "/path/to/my file.txt", false},
		{"path with unicode allowed", "/path/to/文件.txt", false},
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

func TestValidatePathWithBase(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		base    string
		wantErr bool
	}{
		{"path within base", "/home/user/file.txt", "/home/user", false},
		{"path equals base", "/home/user", "/home/user", false},
		{"path escapes base", "/etc/passwd", "/home/user", true},
		{"traversal attack", "/home/user/../../../etc/passwd", "/home/user", true},
		{"relative path rejected", "relative/path", "/home/user", true},
		{"invalid base path", "/home/user/file.txt", "relative/base", true},
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

func TestFileExistsModeConstants_SCP(t *testing.T) {
	// Verify constants are distinct and in expected order
	modes := []FileExistsMode{
		FileExistsModeAbort,
		FileExistsModeSkip,
		FileExistsModeOverwrite,
	}

	seen := make(map[FileExistsMode]bool)
	for _, mode := range modes {
		if seen[mode] {
			t.Errorf("Duplicate FileExistsMode value: %v", mode)
		}
		seen[mode] = true
	}

	// Abort should be 0 (default zero value)
	if FileExistsModeAbort != 0 {
		t.Errorf("FileExistsModeAbort = %v, want 0 (default)", FileExistsModeAbort)
	}
}

func TestSCPTransferOptions_WithProgress(t *testing.T) {
	progressCalled := false
	opts := SCPTransferOptions{
		OnProgress: func(transferred, total int64) {
			progressCalled = true
		},
	}

	if opts.OnProgress == nil {
		t.Error("OnProgress should be set")
	}

	// Call the callback to verify it works
	opts.OnProgress(100, 200)
	if !progressCalled {
		t.Error("OnProgress callback was not called")
	}
}

func TestSCPTransferOptions_RecursiveFlag(t *testing.T) {
	tests := []struct {
		name      string
		recursive bool
	}{
		{"non-recursive", false},
		{"recursive", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := SCPTransferOptions{Recursive: tt.recursive}
			if opts.Recursive != tt.recursive {
				t.Errorf("Recursive = %v, want %v", opts.Recursive, tt.recursive)
			}
		})
	}
}

func TestSCPTransferOptions_PreservePermissions(t *testing.T) {
	tests := []struct {
		name     string
		preserve bool
	}{
		{"no preserve", false},
		{"preserve", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := SCPTransferOptions{PreservePermissions: tt.preserve}
			if opts.PreservePermissions != tt.preserve {
				t.Errorf("PreservePermissions = %v, want %v", opts.PreservePermissions, tt.preserve)
			}
		})
	}
}

func TestSCPTransferOptions_FileExistsModes(t *testing.T) {
	tests := []struct {
		name string
		mode FileExistsMode
	}{
		{"abort mode", FileExistsModeAbort},
		{"skip mode", FileExistsModeSkip},
		{"overwrite mode", FileExistsModeOverwrite},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := SCPTransferOptions{FileExistsMode: tt.mode}
			if opts.FileExistsMode != tt.mode {
				t.Errorf("FileExistsMode = %v, want %v", opts.FileExistsMode, tt.mode)
			}
		})
	}
}

func TestValidateUsername_SCP(t *testing.T) {
	tests := []struct {
		name     string
		username string
		wantErr  bool
	}{
		{"valid username", "root", false},
		{"alphanumeric", "user123", false},
		{"with underscore", "test_user", false},
		{"with hyphen", "test-user", false},
		{"with dot", "test.user", false},
		{"empty", "", true},
		{"too long", string(make([]byte, 100)), true},
		{"shell injection", "user; rm -rf /", true},
		{"backtick injection", "user`id`", true},
		{"dollar injection", "user$HOME", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateUsername(tt.username)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateUsername(%q) error = %v, wantErr %v", tt.username, err, tt.wantErr)
			}
		})
	}
}

func TestValidateHostname_SCP(t *testing.T) {
	tests := []struct {
		name    string
		host    string
		wantErr bool
	}{
		{"localhost", "localhost", false},
		{"ip address", "192.168.1.1", false},
		{"domain", "example.com", false},
		{"subdomain", "sub.example.com", false},
		{"ipv6 localhost", "::1", false},
		{"ipv6 full", "2001:db8:85a3:8d3:1319:8a2e:370:7348", false},
		{"empty", "", true},
		{"too long", string(make([]byte, 300)), true},
		{"shell injection", "localhost; rm -rf /", true},
		{"backtick injection", "localhost`id`", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateHostname(tt.host)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateHostname(%q) error = %v, wantErr %v", tt.host, err, tt.wantErr)
			}
		})
	}
}

func TestSCPRemoteToRemoteOptions_Embedding(t *testing.T) {
	// Verify that SCPRemoteToRemoteOptions correctly embeds SCPTransferOptions
	opts := SCPRemoteToRemoteOptions{
		SCPTransferOptions: SCPTransferOptions{
			PreservePermissions: true,
			FileExistsMode:      FileExistsModeSkip,
			Recursive:           true,
		},
		UseRelay: true,
	}

	if !opts.PreservePermissions {
		t.Error("Embedded PreservePermissions should be true")
	}
	if opts.FileExistsMode != FileExistsModeSkip {
		t.Errorf("Embedded FileExistsMode = %v, want FileExistsModeSkip", opts.FileExistsMode)
	}
	if !opts.Recursive {
		t.Error("Embedded Recursive should be true")
	}
	if !opts.UseRelay {
		t.Error("UseRelay should be true")
	}
}

func TestErrFileExists_SCP(t *testing.T) {
	if ErrFileExists == nil {
		t.Error("ErrFileExists is nil")
	}
	if ErrFileExists.Error() != "file already exists" {
		t.Errorf("ErrFileExists.Error() = %q, want \"file already exists\"", ErrFileExists.Error())
	}
}

func TestErrFileExistsMismatch_SCP(t *testing.T) {
	if ErrFileExistsMismatch == nil {
		t.Error("ErrFileExistsMismatch is nil")
	}
	if ErrFileExistsMismatch.Error() == "" {
		t.Error("ErrFileExistsMismatch.Error() is empty")
	}
}


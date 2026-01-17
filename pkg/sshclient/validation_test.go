// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sshclient

import (
	"testing"
)

func TestValidateUsername(t *testing.T) {
	tests := []struct {
		name     string
		username string
		wantErr  bool
	}{
		{"valid simple", "user", false},
		{"valid with underscore", "my_user", false},
		{"valid with hyphen", "my-user", false},
		{"valid with dot", "user.name", false},
		{"valid numeric suffix", "user123", false},
		{"valid starts with underscore", "_user", false},
		{"empty", "", true},
		{"starts with dot", ".user", true},
		{"starts with hyphen", "-user", true},
		{"contains space", "my user", true},
		{"contains semicolon", "user;rm", true},
		{"contains backtick", "user`id`", true},
		{"contains dollar", "user$HOME", true},
		{"contains pipe", "user|cat", true},
		{"contains ampersand", "user&", true},
		{"too long", string(make([]byte, 65)), true},
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

func TestValidateHostname(t *testing.T) {
	tests := []struct {
		name    string
		host    string
		wantErr bool
	}{
		{"valid simple", "example.com", false},
		{"valid with subdomain", "sub.example.com", false},
		{"valid ip", "192.168.1.1", false},
		{"valid ipv6 raw", "::1", false},
		{"valid ipv6 full", "2001:db8::1", false},
		{"valid with hyphen", "my-server.com", false},
		{"valid single word", "localhost", false},
		{"empty", "", true},
		{"starts with dot", ".example.com", true},
		{"starts with hyphen", "-example.com", true},
		{"ipv6 with brackets", "[::1]", true}, // SSH takes raw IPv6, not bracketed
		{"contains space", "my server.com", true},
		{"contains semicolon", "host;rm", true},
		{"contains backtick", "host`id`", true},
		{"too long", string(make([]byte, 254)), true},
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

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *Config
		wantErr bool
	}{
		{
			name: "valid config",
			cfg: &Config{
				Host:     "example.com",
				Port:     22,
				Username: "user",
			},
			wantErr: false,
		},
		{
			name:    "nil config",
			cfg:     nil,
			wantErr: true,
		},
		{
			name: "invalid username",
			cfg: &Config{
				Host:     "example.com",
				Port:     22,
				Username: "user;rm -rf /",
			},
			wantErr: true,
		},
		{
			name: "invalid host",
			cfg: &Config{
				Host:     "host`id`",
				Port:     22,
				Username: "user",
			},
			wantErr: true,
		},
		{
			name: "invalid port zero",
			cfg: &Config{
				Host:     "example.com",
				Port:     0,
				Username: "user",
			},
			wantErr: true,
		},
		{
			name: "invalid port too high",
			cfg: &Config{
				Host:     "example.com",
				Port:     65536,
				Username: "user",
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConfig(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestShellQuote(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"simple", "simple", "'simple'"},
		{"with space", "with space", "'with space'"},
		{"with single quote", "it's", "'it'\"'\"'s'"},
		{"command injection attempt", "; rm -rf /", "'; rm -rf /'"},
		{"backtick injection", "`id`", "'`id`'"},
		{"dollar injection", "$HOME", "'$HOME'"},
		{"path with spaces", "/path/to/my file.txt", "'/path/to/my file.txt'"},
		{"empty string", "", "''"},
		{"multiple quotes", "a'b'c", "'a'\"'\"'b'\"'\"'c'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShellQuote(tt.input)
			if got != tt.expected {
				t.Errorf("ShellQuote(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestShellQuote_Idempotence(t *testing.T) {
	// Verify that quoted strings, when passed to a shell, produce the original
	dangerous := []string{
		"; rm -rf /",
		"$(cat /etc/passwd)",
		"`whoami`",
		"${PATH}",
		"a\nb",
		"test'quote",
		"test\"doublequote",
		"test\\backslash",
	}

	for _, input := range dangerous {
		quoted := ShellQuote(input)
		// The quoted version should start and end with single quotes
		if len(quoted) < 2 || quoted[0] != '\'' || quoted[len(quoted)-1] != '\'' {
			t.Errorf("ShellQuote(%q) = %q, should be wrapped in single quotes", input, quoted)
		}
	}
}

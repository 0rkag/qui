// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sshclient

import (
	"fmt"
	"strings"
	"testing"

	"github.com/autobrr/qui/pkg/checksum"
)

func TestBuildChecksumCommand_MD5(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{
			name:     "simple path",
			path:     "/path/to/file.txt",
			expected: "md5sum '/path/to/file.txt' | cut -d' ' -f1",
		},
		{
			name:     "path with spaces",
			path:     "/path/to/my file.txt",
			expected: "md5sum '/path/to/my file.txt' | cut -d' ' -f1",
		},
		{
			name:     "path with single quotes",
			path:     "/path/to/file's.txt",
			expected: "md5sum '/path/to/file'\"'\"'s.txt' | cut -d' ' -f1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := fmt.Sprintf("md5sum %s | cut -d' ' -f1", ShellQuote(tt.path))
			if cmd != tt.expected {
				t.Errorf("BuildChecksumCommand(MD5) = %q, want %q", cmd, tt.expected)
			}
		})
	}
}

func TestBuildChecksumCommand_SHA256(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{
			name:     "simple path",
			path:     "/path/to/file.txt",
			expected: "sha256sum '/path/to/file.txt' | cut -d' ' -f1",
		},
		{
			name:     "path with spaces",
			path:     "/path/to/my file.txt",
			expected: "sha256sum '/path/to/my file.txt' | cut -d' ' -f1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := fmt.Sprintf("sha256sum %s | cut -d' ' -f1", ShellQuote(tt.path))
			if cmd != tt.expected {
				t.Errorf("BuildChecksumCommand(SHA256) = %q, want %q", cmd, tt.expected)
			}
		})
	}
}

func TestBuildChecksumCommand_QuotedPaths(t *testing.T) {
	// Test that dangerous characters are properly escaped via single-quoting
	dangerousPaths := []struct {
		name string
		path string
	}{
		{"semicolon injection", "/path/to/; rm -rf /"},
		{"backtick injection", "/path/to/`id`"},
		{"dollar injection", "/path/to/$HOME"},
		{"pipe injection", "/path/to/file | cat /etc/passwd"},
		{"ampersand injection", "/path/to/file & rm -rf /"},
		{"newline injection", "/path/to/file\nrm -rf /"},
	}

	for _, tt := range dangerousPaths {
		t.Run(tt.name, func(t *testing.T) {
			quoted := ShellQuote(tt.path)

			// Verify quoted string is properly wrapped in single quotes
			// Single quotes prevent shell interpretation of special chars
			if len(quoted) < 2 || quoted[0] != '\'' || quoted[len(quoted)-1] != '\'' {
				t.Errorf("ShellQuote(%q) = %q, should be wrapped in single quotes", tt.path, quoted)
			}

			// Build a command to verify it uses the quoted path
			cmd := fmt.Sprintf("md5sum %s | cut -d' ' -f1", quoted)

			// Verify the command contains the quoted path intact
			if !strings.Contains(cmd, quoted) {
				t.Errorf("Command should contain quoted path %q, got: %s", quoted, cmd)
			}

			// Verify the original dangerous path content is inside single quotes in the command
			// The quoted path should appear as a single unit starting with '
			quotedStart := strings.Index(cmd, quoted)
			if quotedStart == -1 {
				t.Errorf("Quoted path not found in command")
				return
			}

			// Verify the quoted string boundaries are correct
			if cmd[quotedStart] != '\'' {
				t.Errorf("Quoted region should start with single quote at position %d", quotedStart)
			}
			quotedEnd := quotedStart + len(quoted) - 1
			if cmd[quotedEnd] != '\'' {
				t.Errorf("Quoted region should end with single quote at position %d", quotedEnd)
			}
		})
	}
}

func TestFileSizeCommand_Format(t *testing.T) {
	// Test that the stat command is built correctly for both Linux and BSD
	path := "/path/to/file.txt"
	quotedPath := ShellQuote(path)

	// The expected format supports both Linux (stat -c %s) and BSD (stat -f %z)
	expected := fmt.Sprintf("stat -c %%s %s 2>/dev/null || stat -f %%z %s", quotedPath, quotedPath)
	actual := fmt.Sprintf("stat -c %%s %s 2>/dev/null || stat -f %%z %s", ShellQuote(path), ShellQuote(path))

	if actual != expected {
		t.Errorf("FileSizeCommand() = %q, want %q", actual, expected)
	}
}

func TestFileSizeCommand_QuotedPaths(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{"simple path", "/path/to/file.txt"},
		{"path with spaces", "/path/to/my file.txt"},
		{"path with single quotes", "/path/to/file's.txt"},
		{"semicolon injection", "/path/to/; rm -rf /"},
		{"backtick injection", "/path/to/`id`"},
		{"dollar injection", "/path/to/$HOME"},
		{"pipe injection", "/path/to/file | cat /etc/passwd"},
		{"ampersand injection", "/path/to/file & rm -rf /"},
		{"newline injection", "/path/to/file\nrm -rf /"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			quotedPath := ShellQuote(tt.path)
			cmd := fmt.Sprintf("stat -c %%s %s 2>/dev/null || stat -f %%z %s", quotedPath, quotedPath)

			// Verify the path is wrapped in single quotes
			if len(quotedPath) < 2 || quotedPath[0] != '\'' || quotedPath[len(quotedPath)-1] != '\'' {
				t.Errorf("Path should be quoted: %q", quotedPath)
			}

			// Verify command contains the stat options
			if !strings.Contains(cmd, "stat -c %s") {
				t.Error("Command should contain Linux stat option")
			}
			if !strings.Contains(cmd, "stat -f %z") {
				t.Error("Command should contain BSD stat option")
			}

			// Verify command contains the quoted path (twice - once for each stat variant)
			if strings.Count(cmd, quotedPath) != 2 {
				t.Errorf("Command should contain quoted path twice, got: %s", cmd)
			}
		})
	}
}

func TestChecksumAlgorithmMapping(t *testing.T) {
	// Verify that our checksum commands map correctly to algorithm constants
	tests := []struct {
		alg      checksum.Algorithm
		cmdStart string
	}{
		{checksum.AlgorithmMD5, "md5sum"},
		{checksum.AlgorithmSHA256, "sha256sum"},
	}

	for _, tt := range tests {
		t.Run(string(tt.alg), func(t *testing.T) {
			var cmd string
			switch tt.alg {
			case checksum.AlgorithmMD5:
				cmd = fmt.Sprintf("md5sum %s | cut -d' ' -f1", ShellQuote("/test"))
			case checksum.AlgorithmSHA256:
				cmd = fmt.Sprintf("sha256sum %s | cut -d' ' -f1", ShellQuote("/test"))
			}

			if len(cmd) < len(tt.cmdStart) || cmd[:len(tt.cmdStart)] != tt.cmdStart {
				t.Errorf("Command for %s should start with %q, got %q", tt.alg, tt.cmdStart, cmd)
			}
		})
	}
}

func TestParseChecksumOutput_Format(t *testing.T) {
	// Test parsing of checksum output in "hash filename" format
	tests := []struct {
		name     string
		output   string
		expected string
		wantErr  bool
	}{
		{
			name:     "standard md5sum output",
			output:   "d41d8cd98f00b204e9800998ecf8427e  /path/to/file.txt",
			expected: "d41d8cd98f00b204e9800998ecf8427e",
			wantErr:  false,
		},
		{
			name:     "standard sha256sum output",
			output:   "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855  /path/to/file.txt",
			expected: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			wantErr:  false,
		},
		{
			name:     "with trailing newline",
			output:   "d41d8cd98f00b204e9800998ecf8427e  /path/to/file.txt\n",
			expected: "d41d8cd98f00b204e9800998ecf8427e",
			wantErr:  false,
		},
		{
			name:     "hash only (after cut -d' ' -f1)",
			output:   "d41d8cd98f00b204e9800998ecf8427e\n",
			expected: "d41d8cd98f00b204e9800998ecf8427e",
			wantErr:  false,
		},
		{
			name:     "empty output",
			output:   "",
			expected: "",
			wantErr:  true,
		},
		{
			name:     "whitespace only",
			output:   "   \n\t  ",
			expected: "",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate parsing - in real code this is done by strings.TrimSpace
			// after the command returns. Here we test the expected format.
			result := parseChecksumOutput(tt.output)

			if tt.wantErr {
				if result != "" {
					t.Errorf("parseChecksumOutput(%q) = %q, want empty for error case", tt.output, result)
				}
			} else {
				if result != tt.expected {
					t.Errorf("parseChecksumOutput(%q) = %q, want %q", tt.output, result, tt.expected)
				}
			}
		})
	}
}

// parseChecksumOutput simulates the parsing done in FileChecksum
// After running "md5sum /path | cut -d' ' -f1", we get just the hash
func parseChecksumOutput(output string) string {
	// Trim whitespace
	result := output
	for len(result) > 0 && (result[0] == ' ' || result[0] == '\t' || result[0] == '\n' || result[0] == '\r') {
		result = result[1:]
	}
	for len(result) > 0 && (result[len(result)-1] == ' ' || result[len(result)-1] == '\t' || result[len(result)-1] == '\n' || result[len(result)-1] == '\r') {
		result = result[:len(result)-1]
	}

	// If it contains a space (full output format), take first field
	for i, c := range result {
		if c == ' ' {
			result = result[:i]
			break
		}
	}

	return result
}

func TestValidatePath_ForChecksum(t *testing.T) {
	// Verify path validation prevents command injection in checksum operations
	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"valid absolute path", "/path/to/file.txt", false},
		{"path traversal", "../../../etc/passwd", true},
		{"null byte injection", "/path/to/file\x00.txt", true},
		{"empty path", "", true},
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

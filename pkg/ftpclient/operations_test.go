// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package ftpclient

import (
	"testing"
)

func TestValidatePath_Valid(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{"absolute path", "/path/to/file"},
		{"relative path", "path/to/file"},
		{"single file", "file.txt"},
		{"root path", "/"},
		{"current dir", "."},
		{"nested path", "/a/b/c/d/e/f/g"},
		{"with dots in name", "/path/to/file.name.ext"},
		{"with numbers", "/path/123/file456"},
		{"with dashes", "/path-name/file-name"},
		{"with underscores", "/path_name/file_name"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePath(tt.path)
			if err != nil {
				t.Errorf("validatePath(%q) error = %v, want nil", tt.path, err)
			}
		})
	}
}

func TestValidatePath_Traversal(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{"parent traversal", "../etc/passwd"},
		{"double parent", "../../etc/passwd"},
		{"triple parent", "../../../etc/passwd"},
		{"mid-path traversal", "/path/../../../etc/passwd"},
		{"encoded traversal", "..%2F..%2Fetc/passwd"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePath(tt.path)
			if err == nil {
				t.Errorf("validatePath(%q) expected error, got nil", tt.path)
			}
		})
	}
}

func TestValidatePath_NullByte(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{"null at start", "\x00/path/to/file"},
		{"null at end", "/path/to/file\x00"},
		{"null in middle", "/path/\x00/file"},
		{"null in filename", "/path/file\x00.txt"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePath(tt.path)
			if err == nil {
				t.Errorf("validatePath(%q) expected error for null byte, got nil", tt.path)
			}
		})
	}
}

func TestValidatePath_Empty(t *testing.T) {
	err := validatePath("")
	if err == nil {
		t.Error("validatePath(\"\") expected error for empty path, got nil")
	}
}

func TestValidatePath_SpecialPaths(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"current dir", ".", false},
		{"parent dir only", "..", true},
		{"double parent", "../..", true},
		{"hidden file", ".hidden", false},
		{"hidden dir", ".hidden/file", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePath(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("validatePath(%q) error = %v, wantErr = %v", tt.path, err, tt.wantErr)
			}
		})
	}
}

// Note: Valid path operations require an actual FTP connection and are covered
// by integration tests. These unit tests verify path validation rejects invalid
// paths before any network operation is attempted.

func TestOperations_PathValidation(t *testing.T) {
	client := &Client{conn: nil}

	// Invalid paths that should be rejected by all operations
	invalidPaths := []struct {
		name string
		path string
	}{
		{"traversal path", "../../../etc"},
		{"null byte path", "/path/\x00/dir"},
		{"empty path", ""},
	}

	// Operations that take a single path and return error
	singlePathOps := []struct {
		name string
		fn   func(string) error
	}{
		{"MkdirAll", client.MkdirAll},
		{"Remove", client.Remove},
		{"RemoveDir", client.RemoveDir},
		{"RemoveAll", client.RemoveAll},
	}

	// Operations that take a single path and return (T, error)
	singlePathQueryOps := []struct {
		name string
		fn   func(string) error
	}{
		{"Stat", func(p string) error { _, err := client.Stat(p); return err }},
		{"Exists", func(p string) error { _, err := client.Exists(p); return err }},
		{"IsDir", func(p string) error { _, err := client.IsDir(p); return err }},
	}

	// Test all operations with all invalid paths
	for _, op := range singlePathOps {
		for _, tt := range invalidPaths {
			t.Run(op.name+"/"+tt.name, func(t *testing.T) {
				err := op.fn(tt.path)
				if err == nil {
					t.Errorf("%s(%q) expected validation error, got nil", op.name, tt.path)
				}
			})
		}
	}

	for _, op := range singlePathQueryOps {
		for _, tt := range invalidPaths {
			t.Run(op.name+"/"+tt.name, func(t *testing.T) {
				err := op.fn(tt.path)
				if err == nil {
					t.Errorf("%s(%q) expected validation error, got nil", op.name, tt.path)
				}
			})
		}
	}
}

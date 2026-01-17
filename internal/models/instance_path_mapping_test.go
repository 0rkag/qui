// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"testing"
)

func TestNormalizePath(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"simple path", "/data/downloads", "/data/downloads"},
		{"trailing slash", "/data/downloads/", "/data/downloads"},
		{"double slashes", "/data//downloads", "/data/downloads"},
		{"relative dots", "/data/./downloads", "/data/downloads"},
		{"whitespace", "  /data/downloads  ", "/data/downloads"},
		{"root path", "/", "/"},
		{"windows path", "C:\\data\\downloads", "C:\\data\\downloads"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizePath(tt.input)
			if result != tt.expected {
				t.Errorf("normalizePath(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestMatchesPrefix(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		prefix   string
		expected bool
	}{
		{"exact match", "/data/downloads", "/data/downloads", true},
		{"prefix match", "/data/downloads/movies", "/data/downloads", true},
		{"no match", "/data/uploads", "/data/downloads", false},
		{"partial name should not match", "/data/downloads2", "/data/downloads", false},
		{"prefix longer than path", "/data", "/data/downloads", false},
		{"similar prefix", "/mnt/storage", "/mnt/stor", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchesPrefix(tt.path, tt.prefix)
			if result != tt.expected {
				t.Errorf("matchesPrefix(%q, %q) = %v, want %v", tt.path, tt.prefix, result, tt.expected)
			}
		})
	}
}

func TestToCanonicalPath(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		mappings  []*InstancePathMapping
		expected  string
		expectErr bool
	}{
		{
			name:     "no mappings - returns same path",
			path:     "/downloads/movies",
			mappings: nil,
			expected: "/downloads/movies",
		},
		{
			name: "exact match",
			path: "/downloads",
			mappings: []*InstancePathMapping{
				{InstancePath: "/downloads", CanonicalPath: "/data/media"},
			},
			expected: "/data/media",
		},
		{
			name: "prefix match with subpath",
			path: "/downloads/movies/action",
			mappings: []*InstancePathMapping{
				{InstancePath: "/downloads", CanonicalPath: "/data/media"},
			},
			expected: "/data/media/movies/action",
		},
		{
			name: "longest prefix wins",
			path: "/downloads/movies/action",
			mappings: []*InstancePathMapping{
				{InstancePath: "/downloads", CanonicalPath: "/data/media"},
				{InstancePath: "/downloads/movies", CanonicalPath: "/data/films"},
			},
			expected: "/data/films/action",
		},
		{
			name: "no matching prefix",
			path: "/uploads/photos",
			mappings: []*InstancePathMapping{
				{InstancePath: "/downloads", CanonicalPath: "/data/media"},
			},
			expected: "/uploads/photos",
		},
		{
			name: "partial name should not match",
			path: "/downloads2/movies",
			mappings: []*InstancePathMapping{
				{InstancePath: "/downloads", CanonicalPath: "/data/media"},
			},
			expected: "/downloads2/movies",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := toCanonicalPath(tt.path, tt.mappings)
			if tt.expectErr {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if result != tt.expected {
				t.Errorf("toCanonicalPath(%q) = %q, want %q", tt.path, result, tt.expected)
			}
		})
	}
}

func TestFromCanonicalPath(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		mappings  []*InstancePathMapping
		expected  string
		expectErr bool
	}{
		{
			name:     "no mappings - returns same path",
			path:     "/data/media/movies",
			mappings: nil,
			expected: "/data/media/movies",
		},
		{
			name: "exact match",
			path: "/data/media",
			mappings: []*InstancePathMapping{
				{InstancePath: "/downloads", CanonicalPath: "/data/media"},
			},
			expected: "/downloads",
		},
		{
			name: "prefix match with subpath",
			path: "/data/media/movies/action",
			mappings: []*InstancePathMapping{
				{InstancePath: "/downloads", CanonicalPath: "/data/media"},
			},
			expected: "/downloads/movies/action",
		},
		{
			name: "longest prefix wins",
			path: "/data/media/movies/action",
			mappings: []*InstancePathMapping{
				{InstancePath: "/downloads", CanonicalPath: "/data/media"},
				{InstancePath: "/movies", CanonicalPath: "/data/media/movies"},
			},
			expected: "/movies/action",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := fromCanonicalPath(tt.path, tt.mappings)
			if tt.expectErr {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if result != tt.expected {
				t.Errorf("fromCanonicalPath(%q) = %q, want %q", tt.path, result, tt.expected)
			}
		})
	}
}

func TestApplyDirectMappings(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		mappings map[string]string
		expected string
	}{
		{
			name:     "no mappings",
			path:     "/downloads/movies",
			mappings: nil,
			expected: "/downloads/movies",
		},
		{
			name:     "empty mappings",
			path:     "/downloads/movies",
			mappings: map[string]string{},
			expected: "/downloads/movies",
		},
		{
			name: "exact match",
			path: "/downloads",
			mappings: map[string]string{
				"/downloads": "/data/media",
			},
			expected: "/data/media",
		},
		{
			name: "prefix match",
			path: "/downloads/movies/action",
			mappings: map[string]string{
				"/downloads": "/data/media",
			},
			expected: "/data/media/movies/action",
		},
		{
			name: "longest prefix wins",
			path: "/downloads/movies/action",
			mappings: map[string]string{
				"/downloads":        "/data/media",
				"/downloads/movies": "/data/films",
			},
			expected: "/data/films/action",
		},
		{
			name: "no match",
			path: "/uploads/photos",
			mappings: map[string]string{
				"/downloads": "/data/media",
			},
			expected: "/uploads/photos",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ApplyDirectMappings(tt.path, tt.mappings)
			if result != tt.expected {
				t.Errorf("ApplyDirectMappings(%q) = %q, want %q", tt.path, result, tt.expected)
			}
		})
	}
}

func TestInstancePathMappingValidate(t *testing.T) {
	tests := []struct {
		name      string
		mapping   *InstancePathMapping
		expectErr bool
		errMsg    string
	}{
		{
			name: "valid mapping",
			mapping: &InstancePathMapping{
				InstanceID:    1,
				InstancePath:  "/downloads",
				CanonicalPath: "/data/media",
			},
			expectErr: false,
		},
		{
			name: "missing instance ID",
			mapping: &InstancePathMapping{
				InstancePath:  "/downloads",
				CanonicalPath: "/data/media",
			},
			expectErr: true,
			errMsg:    "instance ID is required",
		},
		{
			name: "missing instance path",
			mapping: &InstancePathMapping{
				InstanceID:    1,
				CanonicalPath: "/data/media",
			},
			expectErr: true,
			errMsg:    "instance path is required",
		},
		{
			name: "missing canonical path",
			mapping: &InstancePathMapping{
				InstanceID:   1,
				InstancePath: "/downloads",
			},
			expectErr: true,
			errMsg:    "canonical path is required",
		},
		{
			name: "whitespace-only instance path",
			mapping: &InstancePathMapping{
				InstanceID:    1,
				InstancePath:  "   ",
				CanonicalPath: "/data/media",
			},
			expectErr: true,
			errMsg:    "instance path is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.mapping.Validate()
			if tt.expectErr {
				if err == nil {
					t.Errorf("expected error but got none")
					return
				}
				if err.Error() != tt.errMsg {
					t.Errorf("error = %q, want %q", err.Error(), tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

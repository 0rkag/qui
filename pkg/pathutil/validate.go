// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package pathutil provides path validation and manipulation utilities
// for secure file operations across different transfer protocols.
package pathutil

import (
	"errors"
	"path"
	"path/filepath"
	"strings"
)

// Validation errors returned by path validation functions.
var (
	// ErrPathTraversal is returned when a path contains traversal attempts like "..".
	ErrPathTraversal = errors.New("path contains traversal attempt")

	// ErrPathNotAbsolute is returned when an absolute path is required but not provided.
	ErrPathNotAbsolute = errors.New("path must be absolute")

	// ErrPathNullByte is returned when a path contains null bytes.
	ErrPathNullByte = errors.New("path contains null byte")

	// ErrPathEmpty is returned when a path is empty.
	ErrPathEmpty = errors.New("path cannot be empty")
)

// ValidateOptions configures path validation behavior.
type ValidateOptions struct {
	// RequireAbsolute requires the path to be absolute (start with /).
	RequireAbsolute bool

	// AllowTrailingSlash allows paths to end with a slash.
	AllowTrailingSlash bool
}

// DefaultValidateOptions returns the default validation options.
// By default, requires absolute paths and disallows trailing slashes.
func DefaultValidateOptions() ValidateOptions {
	return ValidateOptions{
		RequireAbsolute:    true,
		AllowTrailingSlash: false,
	}
}

// Validate checks a path for security issues like traversal attempts and null bytes.
// Use opts to configure validation behavior. If opts is nil, uses default options.
func Validate(p string, opts *ValidateOptions) error {
	if opts == nil {
		defaultOpts := DefaultValidateOptions()
		opts = &defaultOpts
	}

	if p == "" {
		return ErrPathEmpty
	}

	// Check for null bytes (security issue - can truncate paths)
	if strings.ContainsRune(p, 0) {
		return ErrPathNullByte
	}

	// Check for path traversal BEFORE cleaning
	// This catches explicit ".." sequences in the input
	if strings.Contains(p, "..") {
		return ErrPathTraversal
	}

	// Clean the path to normalize it
	cleaned := path.Clean(p)

	// After cleaning, check again for traversal
	// Clean might normalize "./foo/../bar" to "bar" but won't escape
	// However, paths like "../foo" stay as "../foo" after Clean
	if strings.HasPrefix(cleaned, "..") || strings.Contains(cleaned, "/../") {
		return ErrPathTraversal
	}

	// Check for absolute path if required
	if opts.RequireAbsolute && !strings.HasPrefix(cleaned, "/") {
		return ErrPathNotAbsolute
	}

	return nil
}

// ValidateAbsolute validates that a path is absolute and contains no traversal attempts.
// This is a convenience function equivalent to Validate with RequireAbsolute=true.
func ValidateAbsolute(p string) error {
	opts := ValidateOptions{RequireAbsolute: true}
	return Validate(p, &opts)
}

// ValidateRelative validates that a path contains no traversal attempts.
// Unlike ValidateAbsolute, this allows relative paths.
// This is a convenience function equivalent to Validate with RequireAbsolute=false.
func ValidateRelative(p string) error {
	opts := ValidateOptions{RequireAbsolute: false}
	return Validate(p, &opts)
}

// Clean returns a cleaned version of the path after validation.
// Returns an error if the path fails validation.
func Clean(p string, opts *ValidateOptions) (string, error) {
	if err := Validate(p, opts); err != nil {
		return "", err
	}
	return path.Clean(p), nil
}

// ValidateLocalPath validates a local filesystem path (uses filepath package).
// This handles platform-specific path separators.
func ValidateLocalPath(p string, requireAbsolute bool) error {
	if p == "" {
		return ErrPathEmpty
	}

	// Check for null bytes
	if strings.ContainsRune(p, 0) {
		return ErrPathNullByte
	}

	// Check for path traversal BEFORE cleaning
	if strings.Contains(p, "..") {
		return ErrPathTraversal
	}

	// Clean using filepath for local paths
	cleaned := filepath.Clean(p)

	// After cleaning, check again
	if strings.HasPrefix(cleaned, "..") || strings.Contains(cleaned, string(filepath.Separator)+".."+string(filepath.Separator)) {
		return ErrPathTraversal
	}

	// Check for absolute path if required
	if requireAbsolute && !filepath.IsAbs(cleaned) {
		return ErrPathNotAbsolute
	}

	return nil
}

---
sidebar_position: 4
title: Path Utilities
description: Shared path validation and manipulation utilities.
---

# Path Utilities

The `pkg/pathutil` package provides shared path validation and manipulation utilities used across qui for secure file operations.

## Overview

Path validation is critical for security, preventing:
- **Path traversal attacks** (`../../../etc/passwd`)
- **Null byte injection** (truncating paths)
- **Invalid path formats** (relative paths where absolute required)

## Functions

### Validate

Configurable path validation:

```go
import "github.com/autobrr/qui/pkg/pathutil"

opts := &pathutil.ValidateOptions{
    RequireAbsolute:    true,  // Path must start with /
    AllowTrailingSlash: false, // Disallow trailing /
}

err := pathutil.Validate("/data/torrents/file.mkv", opts)
// Returns nil (valid)

err := pathutil.Validate("../etc/passwd", opts)
// Returns ErrPathTraversal

err := pathutil.Validate("relative/path", opts)
// Returns ErrPathNotAbsolute
```

### ValidateAbsolute

Convenience function requiring absolute paths:

```go
err := pathutil.ValidateAbsolute("/data/file.txt")  // OK
err := pathutil.ValidateAbsolute("relative/path")   // Error
```

### ValidateRelative

Convenience function allowing relative paths (used by FTP):

```go
err := pathutil.ValidateRelative("data/file.txt")   // OK
err := pathutil.ValidateRelative("../escape")       // Error
```

### ValidateLocalPath

Platform-aware validation for local filesystem paths:

```go
// Uses filepath package for OS-specific separators
err := pathutil.ValidateLocalPath("/home/user/file", true)  // Unix
err := pathutil.ValidateLocalPath("C:\\Users\\file", true)  // Windows
```

## Errors

| Error | Description |
|-------|-------------|
| `ErrPathTraversal` | Path contains `..` traversal attempt |
| `ErrPathNotAbsolute` | Path is relative when absolute required |
| `ErrPathNullByte` | Path contains null byte |
| `ErrPathEmpty` | Path is empty string |

## Usage in qui

### SSH Client

The SSH client requires absolute paths:

```go
// pkg/sshclient/transfer.go
func ValidatePath(p string) error {
    if len(p) > 4096 {
        return fmt.Errorf("path too long")
    }
    return pathutil.ValidateAbsolute(p)
}
```

### FTP Client

The FTP client allows relative paths:

```go
// pkg/ftpclient/operations.go
func validatePath(p string) error {
    return pathutil.ValidateRelative(p)
}
```

## Security Checks

The validation performs these checks in order:

1. **Empty check** - Path cannot be empty
2. **Null byte check** - No `\x00` characters (can truncate paths in C libraries)
3. **Traversal check (pre-clean)** - Detect explicit `..` in input
4. **Path cleaning** - Normalize using `path.Clean()`
5. **Traversal check (post-clean)** - Detect `..` after normalization
6. **Absolute check** - Verify path starts with `/` (if required)

## Additional Utilities

The package also includes utilities for torrent file handling:

### SanitizePathSegment

Sanitizes a single path segment (filename or directory name):

```go
safe := pathutil.SanitizePathSegment("CON.txt")  // "_CON.txt" (Windows reserved)
safe := pathutil.SanitizePathSegment("file:name") // "file_name" (illegal char)
```

### TorrentKey

Generates a unique key for a torrent:

```go
key := pathutil.TorrentKey("Ubuntu.22.04.iso", "abc123def456...")
// Returns deterministic hash for caching/deduplication
```

### IsolationFolderName

Creates a safe folder name for torrent isolation:

```go
folder := pathutil.IsolationFolderName("My Torrent [2024]", "abc123")
// Returns sanitized name with hash suffix
```

## Key Files

| File | Purpose |
|------|---------|
| `validate.go` | Path validation functions |
| `sanitize.go` | Path segment sanitization |
| `torrent.go` | Torrent-specific utilities |

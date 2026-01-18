---
sidebar_position: 3
title: Path Resolver
description: Path mapping system with canonical path translation.
---

# Path Resolver

The path resolver (`internal/models/instance_path_mapping.go`) translates paths between instances using a **canonical path model**. This enables transfers between instances that see the same storage at different mount points.

## The Problem

Different deployments mount storage differently:

```
Instance A (Docker):  /downloads/movies/film.mkv
Instance B (native):  /mnt/storage/downloads/movies/film.mkv
qui server:           /data/media/downloads/movies/film.mkv
```

Direct instance-to-instance mapping requires N×N configurations. With 5 instances, that's 20 mapping sets.

## The Solution

Use a **two-step translation** via the qui server's view (canonical path):

```
Instance A path  →  Canonical path  →  Instance B path
/downloads/      →  /data/media/    →  /mnt/storage/
```

Each instance only defines its own mappings. Adding a new instance doesn't require updating existing ones.

## Data Model

```go
type InstancePathMapping struct {
    ID            int64
    InstanceID    int
    InstancePath  string  // Path as seen by qBittorrent
    CanonicalPath string  // Path as seen by qui server
    Enabled       bool
    Description   string
    SortOrder     int
}
```

Stored in `instance_path_mappings` table with unique constraint on `(instance_id, instance_path)`.

## PathResolver

```go
type PathResolver struct {
    store *InstancePathMappingStore
}

// Convert instance path → canonical path
func (r *PathResolver) ToCanonicalPath(ctx, instanceID, path) (string, error)

// Convert canonical path → instance path
func (r *PathResolver) FromCanonicalPath(ctx, instanceID, path) (string, error)

// Full resolution for transfers
func (r *PathResolver) ResolveTargetPath(
    ctx,
    sourcePath string,
    sourceInstanceID int,
    targetInstanceID int,
    transferMappings map[string]string,  // Optional per-transfer override
) (string, error)
```

## Matching Algorithm

Uses **longest prefix matching**:

```go
func toCanonicalPath(path string, mappings []*InstancePathMapping) (string, error) {
    var bestMatch *InstancePathMapping
    var bestLen int

    for _, m := range mappings {
        if matchesPrefix(path, m.InstancePath) {
            if len(m.InstancePath) > bestLen {
                bestMatch = m
                bestLen = len(m.InstancePath)
            }
        }
    }

    if bestMatch == nil {
        return path, nil  // No mapping, return as-is
    }

    return bestMatch.CanonicalPath + path[len(bestMatch.InstancePath):], nil
}
```

### Prefix Matching

Matches on path boundaries to avoid partial directory matches:

```go
func matchesPrefix(path, prefix string) bool {
    if path == prefix {
        return true
    }
    if !strings.HasPrefix(path, prefix) {
        return false
    }
    // Ensure boundary match
    nextChar := path[len(prefix)]
    return nextChar == '/' || nextChar == '\\'
}
```

Example:
- `/downloads` matches `/downloads/movies/film.mkv`
- `/downloads` does NOT match `/downloads-old/file.mkv`

## Translation Flow

```
Source Instance A                    Target Instance B
/downloads/movies/film.mkv
        │
        ▼ ToCanonicalPath
/data/media/downloads/movies/film.mkv
        │
        ▼ FromCanonicalPath
                                     /mnt/storage/downloads/movies/film.mkv
```

## Priority

1. **Per-transfer mappings** - If provided in API request, used directly
2. **Instance mappings** - Default two-step translation
3. **HardlinkBaseDir** - Fallback if no mapping matches
4. **Same path** - Last resort, assumes shared storage

```go
func (r *PathResolver) ResolveTargetPath(...) (string, error) {
    // Per-transfer override
    if len(transferMappings) > 0 {
        return ApplyDirectMappings(sourcePath, transferMappings), nil
    }

    // Two-step via canonical
    canonical, err := r.ToCanonicalPath(ctx, sourceInstanceID, sourcePath)
    if err != nil {
        return "", err
    }

    return r.FromCanonicalPath(ctx, targetInstanceID, canonical)
}
```

## Path Normalization

Paths are normalized before matching:

```go
func normalizePath(p string) string {
    p = strings.TrimSpace(p)
    p = filepath.Clean(p)
    // Remove trailing slash (unless root)
    if len(p) > 1 && strings.HasSuffix(p, "/") {
        p = p[:len(p)-1]
    }
    return p
}
```

## Store Operations

```go
store := models.NewInstancePathMappingStore(db)

// CRUD
mapping, err := store.Create(ctx, &InstancePathMapping{...})
mapping, err := store.Get(ctx, id)
err := store.Update(ctx, mapping)
err := store.Delete(ctx, id)

// List
mappings, err := store.ListByInstance(ctx, instanceID)
mappings, err := store.ListEnabledByInstance(ctx, instanceID)

// Bulk
err := store.DeleteByInstance(ctx, instanceID)
err := store.UpdateSortOrder(ctx, map[int64]int{1: 0, 2: 1})
```

## Integration

Used by both LocalExecutor and SSHExecutor:

```go
// In executor
if e.pathResolver != nil {
    targetPath, err := e.pathResolver.ResolveTargetPath(
        ctx,
        sourcePath,
        t.SourceInstanceID,
        t.TargetInstanceID,
        t.PathMappings,
    )
}
```

## Key Files

| File | Purpose |
|------|---------|
| `internal/models/instance_path_mapping.go` | Model, store, PathResolver |
| `internal/database/migrations/053_*.sql` | Schema |
| `internal/api/handlers/instance_path_mappings.go` | API handlers |

---
sidebar_position: 1
title: Transfer Service
description: Architecture of the torrent transfer service and executor pattern.
---

# Transfer Service

The transfer service (`internal/services/transfer/`) handles moving torrents between qBittorrent instances. It uses an **executor pattern** to support different deployment scenarios.

## Overview

```
┌──────────────────────────────────────────────────────────────┐
│                     Transfer Service                          │
│                                                               │
│  ┌─────────────┐    ┌─────────────┐    ┌─────────────┐       │
│  │   Queue     │ →  │   Worker    │ →  │  Executor   │       │
│  │  (chan)     │    │  (goroutine)│    │  (interface)│       │
│  └─────────────┘    └─────────────┘    └──────┬──────┘       │
│                                               │               │
│                           ┌───────────────────┼───────────────┐
│                           │                   │               │
│                           ▼                   ▼               │
│                    ┌─────────────┐     ┌─────────────┐       │
│                    │   Local     │     │    SSH      │       │
│                    │  Executor   │     │  Executor   │       │
│                    └─────────────┘     └─────────────┘       │
└──────────────────────────────────────────────────────────────┘
```

## State Machine

Transfers progress through a state machine:

```
Pending → Preparing → Linking → Adding → [Deleting] → Completed
                ↓         ↓        ↓          ↓
              Failed   Failed   Failed     Failed
```

| State | Description |
|-------|-------------|
| `pending` | Queued, waiting for worker |
| `preparing` | Gathering torrent metadata, computing paths |
| `linking` | Creating hardlinks/reflinks or transferring files |
| `adding` | Adding torrent to target instance |
| `deleting` | Removing from source (if enabled) |
| `completed` | Transfer finished successfully |
| `failed` | Error occurred, see error field |
| `cancelled` | User cancelled the transfer |

## Executor Pattern

The `TransferExecutor` interface abstracts file operations:

```go
type TransferExecutor interface {
    Prepare(ctx, transfer) (*PrepareResult, error)
    CreateLinks(ctx, transfer, prepResult) (int, error)
    AddTorrent(ctx, transfer, prepResult) error
    DeleteSource(ctx, transfer) error
    Rollback(ctx, transfer, prepResult) error
    CanHandle(source, target *Instance) bool
}
```

### LocalExecutor

Used when qui has direct filesystem access to both instances.

**Selection criteria:** Both instances have `HasLocalFilesystemAccess = true`

**Link modes:**
- `hardlink` - Same filesystem, instant, no extra space
- `reflink` - CoW filesystem (BTRFS, XFS), instant
- `copy` - Fallback, copies files
- `direct` - Shared storage, no file ops needed

### SSHExecutor

Used when file operations must be performed remotely.

**Selection criteria:** At least one instance lacks local filesystem access

**Transfer modes:**
- Push (local → remote via rsync)
- Pull (remote → local via rsync)
- Remote-to-remote (rsync executed on source)
- Remote linking (hardlink/reflink via SSH commands)

## PrepareResult

Data gathered during preparation, passed through all phases:

```go
type PrepareResult struct {
    TorrentName    string
    SourceSavePath string
    TargetSavePath string
    LinkMode       string           // hardlink, reflink, copy, transfer, direct
    Files          []TorrentFile
    TorrentData    []byte           // Exported .torrent content
    Category       string
    Tags           []string
    SourceInstance *models.Instance
    TargetInstance *models.Instance
}
```

Caching this data avoids redundant API calls in later phases.

## Worker Pool

The service runs a configurable number of worker goroutines (default: 2):

```go
func (s *Service) worker(id int) {
    for {
        select {
        case <-s.workerCtx.Done():
            return
        case transferID := <-s.queue:
            s.processTransfer(transferID)
        }
    }
}
```

Transfers are queued by ID. Workers fetch the full transfer from the database to ensure consistency after restarts.

## Recovery

On startup, the service recovers interrupted transfers:

| Interrupted State | Recovery Action |
|-------------------|-----------------|
| `preparing` | Reset to `pending`, re-queue |
| `linking` | Rollback files, mark `failed` |
| `adding` | Check if torrent exists on target, continue or rollback |
| `deleting` | Re-queue to complete deletion |

## Path Resolution

The transfer service integrates with the [PathResolver](/docs/development/modules/path-resolver) for path translation:

1. Get source path from torrent properties
2. Translate via PathResolver (if mappings configured)
3. Fall back to `HardlinkBaseDir` or same path

Per-transfer path mappings override instance-level mappings.

## Key Files

| File | Purpose |
|------|---------|
| `service.go` | Service struct, lifecycle, worker management |
| `executor.go` | Interface definition, PrepareResult |
| `local.go` | LocalExecutor implementation |
| `ssh.go` | SSHExecutor implementation |
| `path.go` | Path utilities (legacy) |

## Usage

```go
// Create service
svc := transfer.New(store, instanceStore, syncManager)

// Wire up executors
svc.RegisterExecutor(transfer.NewLocalExecutor(...))
svc.RegisterExecutor(transfer.NewSSHExecutor(...))

// Start workers
svc.Start(ctx)
defer svc.Stop()

// Queue a transfer
transfer, err := svc.QueueTransfer(ctx, &transfer.TransferRequest{
    SourceInstanceID: 1,
    TargetInstanceID: 2,
    TorrentHash:      "abc123...",
    DeleteFromSource: true,
})
```

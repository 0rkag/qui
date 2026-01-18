---
sidebar_position: 9
title: Torrent Transfers
description: Move torrents between qBittorrent instances with hardlinks, reflinks, or file transfers.
---

# Torrent Transfers

:::warning[Work in Progress]
This feature is under active development. The UI and behavior may change. Use with caution in production environments.
:::

Move torrents between qBittorrent instances without re-downloading. qui creates hardlinks or reflinks when possible, making transfers instant and space-efficient. For remote instances, files are transferred via rsync.

## How It All Fits Together

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           Transfer System Overview                           │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │                          qui server                                  │   │
│   │                                                                      │   │
│   │   ┌─────────────┐     ┌─────────────┐     ┌─────────────┐          │   │
│   │   │  Transfer   │     │    Path     │     │    SSH      │          │   │
│   │   │   Service   │────▶│   Resolver  │────▶│    Pool     │          │   │
│   │   └─────────────┘     └─────────────┘     └──────┬──────┘          │   │
│   │          │                   │                   │                  │   │
│   │          │            Maps paths between        Manages             │   │
│   │          │            instance views            connections         │   │
│   └──────────┼───────────────────────────────────────┼──────────────────┘   │
│              │                                       │                      │
│              │                                       │                      │
│   ┌──────────▼──────────┐                 ┌─────────▼─────────┐            │
│   │   LOCAL INSTANCE    │                 │  REMOTE INSTANCE  │            │
│   │   (same machine)    │                 │  (via SSH)        │            │
│   │                     │                 │                   │            │
│   │  /downloads/        │    ═══════════  │  /data/torrents/  │            │
│   │  └─ movies/         │    Hardlinks    │  └─ movies/       │            │
│   │     └─ film.mkv     │    or Rsync     │     └─ film.mkv   │            │
│   │                     │    ═══════════  │                   │            │
│   │  ┌─────────────┐    │                 │  ┌─────────────┐  │            │
│   │  │ qBittorrent │    │                 │  │ qBittorrent │  │            │
│   │  └─────────────┘    │                 │  └─────────────┘  │            │
│   └─────────────────────┘                 └───────────────────┘            │
│                                                                              │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │                        Shared Storage                                │   │
│   │   qui sees:     /srv/media/downloads/movies/film.mkv                │   │
│   │   Instance A:   /downloads/movies/film.mkv        ← Path Mapping    │   │
│   │   Instance B:   /data/torrents/movies/film.mkv    ← Path Mapping    │   │
│   └─────────────────────────────────────────────────────────────────────┘   │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

**Key components:**
- **Transfer Service** orchestrates the move, selecting the best method (hardlink, reflink, copy, rsync)
- **[Path Resolver](/docs/features/path-mappings)** translates paths between different views of the same storage
- **[SSH Pool](/docs/advanced/remote-instances)** maintains connections for remote file operations

## Quick Start

1. Select one or more torrents in the torrent list
2. Right-click to open the context menu
3. Click **Move to Instance**
4. Select the target instance
5. Click **Move**

qui queues the transfer and processes it in the background. You can monitor progress in the transfer list.

## Setup Checklist

Use this checklist to configure transfers between instances. Not all steps are required—check the "When needed" column.

| Step | Where | When Needed |
|------|-------|-------------|
| ☐ Enable **Local Filesystem Access** | Instance → Preferences | qui runs on the same machine as qBittorrent |
| ☐ Configure **Path Mappings** | Instance → Preferences → Path Mappings | Instances see storage at different paths |
| ☐ Configure **SSH Connection** | Instance → Preferences → Connection | qBittorrent is on a remote machine |
| ☐ Enable **Use Hardlinks** | Instance → Preferences | Same filesystem, want instant transfers |
| ☐ Enable **Use Reflinks** | Instance → Preferences | BTRFS/XFS/APFS filesystem |
| ☐ Install **rsync** | Both machines | Transferring between different machines |

**Common scenarios:**

| Scenario | Required Setup |
|----------|---------------|
| Two local instances, same filesystem | Local Filesystem Access ✓, Path Mappings (if paths differ), Hardlinks ✓ |
| Two local instances, different filesystems | Local Filesystem Access ✓, Path Mappings ✓ |
| Local → Remote seedbox | SSH on remote ✓, Path Mappings ✓, rsync ✓ |
| Two Docker containers | Path Mappings ✓ (map container paths to host paths) |

## Transfer Modes

qui automatically selects the best transfer mode based on your instance configuration:

| Mode | When Used | Speed | Disk Usage |
|------|-----------|-------|------------|
| **Hardlink** | Same filesystem, hardlinks enabled | Instant | No extra space |
| **Reflink** | CoW filesystem (BTRFS, XFS), reflinks enabled | Instant | No extra space initially |
| **Copy** | Same machine, no link support | Slow | Doubles disk usage |
| **Transfer** | Different machines (via SSH) | Network speed | Doubles disk usage |
| **Direct** | Shared storage, no file ops needed | Instant | No extra space |

### Hardlinks (Recommended)

Hardlinks create additional directory entries pointing to the same file data. The file only uses disk space once, even though it appears in multiple locations.

**Requirements:**
- Source and target paths on the same filesystem
- Instance has **Use Hardlinks** enabled in preferences

### Reflinks

Reflinks are copy-on-write links supported by BTRFS, XFS (with reflink support), and APFS. Initially they share disk blocks, but diverge if either copy is modified.

**Requirements:**
- CoW-capable filesystem
- Instance has **Use Reflinks** enabled in preferences

### File Transfer (SSH)

When instances are on different machines, qui transfers files via rsync over SSH. This requires [SSH connection configuration](/docs/advanced/remote-instances).

## How Transfers Work

```
┌─────────────────────────────────────────────────────────────────┐
│                        Transfer Flow                             │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  1. SELECT TORRENTS          2. CHOOSE TARGET                   │
│     ┌─────────┐                 ┌─────────┐                     │
│     │ Torrent │ ──────────────▶ │ Target  │                     │
│     │  List   │   Right-click   │Instance │                     │
│     └─────────┘   "Move to..."  └─────────┘                     │
│                                      │                          │
│                                      ▼                          │
│  3. QUEUE TRANSFER           4. BACKGROUND PROCESSING           │
│     ┌─────────┐                 ┌─────────┐                     │
│     │ Transfer│ ──────────────▶ │ Worker  │                     │
│     │  Queue  │                 │  Pool   │                     │
│     └─────────┘                 └────┬────┘                     │
│                                      │                          │
│         ┌────────────────────────────┼────────────────────┐     │
│         ▼                            ▼                    ▼     │
│    ┌─────────┐                 ┌──────────┐         ┌─────────┐ │
│    │ Prepare │ ──────────────▶ │  Create  │ ──────▶ │   Add   │ │
│    │Metadata │                 │  Links   │         │ Torrent │ │
│    └─────────┘                 └──────────┘         └─────────┘ │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

## Transfer States

```
    ┌─────────┐
    │ Pending │ ─────────────────────────────────────┐
    └────┬────┘                                      │
         │                                           │
         ▼                                           ▼
    ┌───────────┐     ┌─────────┐     ┌─────────┐   ┌──────────┐
    │ Preparing │ ──▶ │ Linking │ ──▶ │ Adding  │ ──▶│Completed │
    └─────┬─────┘     └────┬────┘     └────┬────┘   └──────────┘
          │                │               │
          │                │               │         ┌──────────┐
          └────────────────┴───────────────┴────────▶│  Failed  │
                                                     └──────────┘
```

| State | Description |
|-------|-------------|
| Pending | Queued, waiting to be processed |
| Preparing | Gathering torrent metadata and computing paths |
| Linking | Creating hardlinks, reflinks, or transferring files |
| Adding | Adding torrent to target instance |
| Completed | Transfer finished successfully |
| Failed | Transfer encountered an error |

## Monitoring Transfers

View active and completed transfers:

1. Go to **Transfers** in the main navigation
2. Filter by instance or state as needed
3. Click a transfer to see details

Failed transfers show error messages to help diagnose issues.

## Path Mappings

When instances see storage at different mount points, configure [path mappings](/docs/features/path-mappings) so qui can translate paths correctly.

**Example:**
- Instance A (Docker): `/downloads/movies/`
- Instance B (native): `/mnt/storage/downloads/movies/`

With path mappings configured, qui automatically translates paths during transfers.

## Automation Integration

Transfers can be triggered automatically via [automations](/docs/features/automations). Add a **Move to Instance** action to your automation rules:

1. Go to **Services** and select your instance
2. Open the **Automations** section
3. Create or edit a rule
4. Add the **Move to Instance** action
5. Select the target instance
6. Configure options (delete from source, preserve category/tags)

**Example use case:** Move completed torrents from a racing instance to a long-term seeding instance after 24 hours.

## Options

| Option | Description | Default |
|--------|-------------|---------|
| Delete from source | Remove torrent from source after transfer | Yes |
| Preserve category | Keep the same category on target | Yes |
| Preserve tags | Keep the same tags on target | Yes |

## REST API

Manage transfers programmatically:

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/transfers` | List transfers |
| `GET` | `/api/transfers/{id}` | Get transfer details |
| `POST` | `/api/transfers` | Queue a new transfer |
| `DELETE` | `/api/transfers/{id}` | Cancel a pending transfer |
| `POST` | `/api/instances/{id}/torrents/{hash}/move` | Move a single torrent |

**Example: Queue a transfer**

```http
POST /api/transfers
Content-Type: application/json

{
  "sourceInstanceId": 1,
  "targetInstanceId": 2,
  "torrentHash": "abc123...",
  "deleteFromSource": true,
  "preserveCategory": true,
  "preserveTags": true
}
```

## Troubleshooting

### Transfer fails with "path not found"

The target path doesn't exist or isn't accessible. Check:
- Path mappings are configured correctly
- Target instance has filesystem access
- Directories exist and have correct permissions

### Transfer fails with "cross-device link"

Hardlinks require source and target on the same filesystem. Either:
- Configure path mappings to use a common mount point
- Enable reflinks or fallback to copy mode
- Use SSH transfer for different machines

### Transfer is stuck in "Preparing"

qui can't gather torrent metadata. Check:
- Source instance is online and accessible
- Torrent exists and is fully downloaded
- qBittorrent API is responding

### Files transferred but torrent won't start

The target instance may not recognize the files. Verify:
- Path mappings translate correctly to the target's view
- Files are in the expected location
- Run a recheck on the torrent in qBittorrent

## See Also

- **[Path Mappings](/docs/features/path-mappings)** — Configure how paths translate between instances
- **[Remote Instances](/docs/advanced/remote-instances)** — Set up SSH for remote qBittorrent instances
- **[Automations](/docs/features/automations#move-to-instance)** — Trigger transfers automatically with rules

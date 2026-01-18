---
sidebar_position: 10
title: Path Mappings
description: Configure how paths translate between instances and the qui server.
---

# Instance Path Mappings

:::warning[Work in Progress]
This feature is under active development. The UI and behavior may change.
:::

When qBittorrent instances and the qui server see storage at different mount points, path mappings tell qui how to translate paths. This is essential for [transfers](/docs/features/transfers) between instances with different filesystem views.

:::tip[Related Documentation]
- **[Torrent Transfers](/docs/features/transfers)** — Move torrents between instances using path mappings
- **[Remote Instances](/docs/advanced/remote-instances)** — Configure SSH for remote qBittorrent instances
:::

## The Problem

Different deployments mount the same storage differently:

| Component | Path |
|-----------|------|
| Instance A (Docker) | `/downloads/movies/` |
| Instance B (native) | `/mnt/storage/downloads/movies/` |
| qui server | `/data/media/downloads/movies/` |

Without path mappings, qui can't translate `/downloads/movies/` on Instance A to `/mnt/storage/downloads/movies/` on Instance B.

## The Solution: Canonical Paths

Instead of mapping every instance to every other instance (N×N mappings), qui uses a **canonical path** model. Each instance defines how its paths relate to the qui server's view:

```
Instance A path  →  Canonical (qui) path  →  Instance B path
/downloads/      →  /data/media/downloads/ →  /mnt/storage/downloads/
```

**Benefits:**
- Configure once per instance, works with all other instances
- Adding a new instance doesn't require updating existing mappings
- Simpler mental model: "how does this instance see storage vs. qui?"

## Quick Start

1. Go to **Instances** and click the gear icon on an instance
2. Open the **Path Mappings** tab
3. Click **Add Mapping**
4. Enter the instance path and the corresponding qui server path
5. Click **Save**

Repeat for each instance that has different mount points than the qui server.

## Configuration

### Path Mapping Fields

| Field | Description |
|-------|-------------|
| **Instance Path** | Path as seen by qBittorrent (e.g., `/downloads/`) |
| **Canonical Path** | Same location as seen by qui server (e.g., `/data/media/downloads/`) |
| **Enabled** | Toggle to temporarily disable without deleting |
| **Description** | Optional note for your reference |

### Mapping Rules

- Paths should be absolute (start with `/` on Linux/macOS)
- Trailing slashes are normalized automatically
- The **longest matching prefix** wins when multiple mappings could apply
- If no mapping matches, the path is used as-is

## Examples

### Docker Instance + Native qui

qBittorrent runs in Docker with volume mounts. qui runs natively on the host.

| Instance Path | Canonical Path | Notes |
|---------------|----------------|-------|
| `/downloads` | `/home/user/downloads` | Docker volume mount |
| `/data/tv` | `/mnt/media/tv` | Another volume |

### Seedbox + Local NAS

qBittorrent on a remote seedbox, files synced to local NAS where qui runs.

| Instance Path | Canonical Path | Notes |
|---------------|----------------|-------|
| `/home/user/torrents` | `/mnt/nas/seedbox-sync` | rclone/syncthing destination |

### Multiple Docker Instances

Two qBittorrent containers with different internal paths.

**Instance A:**
| Instance Path | Canonical Path |
|---------------|----------------|
| `/downloads` | `/srv/torrents/downloads` |

**Instance B:**
| Instance Path | Canonical Path |
|---------------|----------------|
| `/data/torrents` | `/srv/torrents/downloads` |

Both map to the same canonical path because they share storage on the host.

## How Translation Works

```
┌─────────────────────────────────────────────────────────────────────┐
│                     Path Translation Flow                            │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│   INSTANCE A                 qui server              INSTANCE B      │
│   (Docker)                   (Canonical)             (Native)        │
│                                                                      │
│  /downloads/                /srv/torrents/         /data/torrents/   │
│       │                          │                       ▲           │
│       │    ┌─────────────────────┴───────────────────────┘           │
│       │    │                                                         │
│       ▼    ▼                                                         │
│  ┌─────────────┐         ┌─────────────┐         ┌─────────────┐    │
│  │  Instance   │         │  Canonical  │         │  Instance   │    │
│  │    Path     │ ──────▶ │    Path     │ ──────▶ │    Path     │    │
│  │  Mapping    │  Step 1 │   (qui's    │  Step 2 │   Mapping   │    │
│  │             │         │    view)    │         │             │    │
│  └─────────────┘         └─────────────┘         └─────────────┘    │
│                                                                      │
│  /downloads/movies/film.mkv                                          │
│       │                                                              │
│       └──────▶ /srv/torrents/movies/film.mkv                        │
│                      │                                               │
│                      └──────▶ /data/torrents/movies/film.mkv        │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

When transferring from Instance A to Instance B:

1. **Source translation**: Instance A path → Canonical path
   - `/downloads/movies/film.mkv` → `/srv/torrents/movies/film.mkv`

2. **Target translation**: Canonical path → Instance B path
   - `/srv/torrents/movies/film.mkv` → `/data/torrents/movies/film.mkv`

qui uses longest-prefix matching, so more specific mappings take precedence:

| Instance Path | Canonical Path |
|---------------|----------------|
| `/downloads` | `/data/general` |
| `/downloads/movies` | `/data/media/movies` |

A file at `/downloads/movies/film.mkv` matches the second mapping (longer prefix) and becomes `/data/media/movies/film.mkv`.

## Per-Transfer Overrides

For one-off transfers with special requirements, you can specify path mappings directly in the transfer request. These override instance-level mappings entirely.

This is useful for:
- Testing new mappings before saving them
- Edge cases that don't fit the normal pattern
- Migrations with temporary paths

## Relationship with External Programs

[External programs](/docs/features/external-programs) have their own path mapping system. By default, instance path mappings are used. You can override this per-program if needed.

## REST API

Manage path mappings programmatically:

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/instances/{id}/path-mappings` | List mappings for an instance |
| `POST` | `/api/instances/{id}/path-mappings` | Create a mapping |
| `PUT` | `/api/instances/{id}/path-mappings/{mappingId}` | Update a mapping |
| `DELETE` | `/api/instances/{id}/path-mappings/{mappingId}` | Delete a mapping |

**Example: Create a mapping**

```http
POST /api/instances/1/path-mappings
Content-Type: application/json

{
  "instancePath": "/downloads",
  "canonicalPath": "/data/media/downloads",
  "enabled": true,
  "description": "Main downloads directory"
}
```

## Troubleshooting

### Transfers fail with wrong target path

Check that:
- Both source and target instances have path mappings configured
- The canonical path is the same storage location, just as qui sees it
- Mappings are enabled (not disabled)

### Path appears unchanged after mapping

The mapping prefix might not match. Verify:
- No typos in the instance path
- Path separators match (`/` vs `\`)
- The full prefix matches (e.g., `/downloads` won't match `/download`)

### Overlapping mappings cause confusion

Use the most specific mappings possible. If you have:
- `/data` → `/mnt/data`
- `/data/movies` → `/mnt/media/movies`

A file at `/data/movies/film.mkv` will correctly use the second mapping.

### Windows paths

Use backslashes as seen by qBittorrent:
- Instance path: `D:\Downloads`
- Canonical path: `/mnt/windows-share/Downloads`

qui handles the path separator conversion during translation.

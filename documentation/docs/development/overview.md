---
sidebar_position: 1
title: Architecture Overview
description: Technical overview of qui's architecture, tech stack, and project structure.
---

# Architecture Overview

qui is a web interface for managing multiple qBittorrent instances. This document provides a technical overview for developers who want to understand or contribute to the codebase.

## Architecture Diagram

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                                 qui server                                   │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  ┌──────────────┐     ┌──────────────┐     ┌──────────────┐                 │
│  │   Frontend   │     │   REST API   │     │   SSE/WS     │                 │
│  │   (React)    │◀───▶│   Handlers   │◀───▶│   Events     │                 │
│  └──────────────┘     └──────┬───────┘     └──────────────┘                 │
│                              │                                               │
│         ┌────────────────────┼────────────────────┐                         │
│         ▼                    ▼                    ▼                         │
│  ┌──────────────┐     ┌──────────────┐     ┌──────────────┐                 │
│  │    Sync      │     │   Transfer   │     │    Other     │                 │
│  │   Manager    │     │   Service    │     │   Services   │                 │
│  │              │     │              │     │              │                 │
│  │ • Poll qBit  │     │ • Executors  │     │ • Automations│                 │
│  │ • Cache data │     │ • Workers    │     │ • CrossSeed  │                 │
│  │ • Broadcast  │     │ • Recovery   │     │ • Reannounce │                 │
│  └──────┬───────┘     └──────┬───────┘     └──────────────┘                 │
│         │                    │                                               │
│         │             ┌──────┴───────┐                                      │
│         │             │   Executor   │                                      │
│         │             │   Registry   │                                      │
│         │             └──────┬───────┘                                      │
│         │         ┌──────────┴──────────┐                                   │
│         │         ▼                     ▼                                   │
│         │  ┌──────────────┐     ┌──────────────┐                            │
│         │  │    Local     │     │     SSH      │                            │
│         │  │   Executor   │     │   Executor   │                            │
│         │  │              │     │              │                            │
│         │  │ • Hardlinks  │     │ • SSH Pool   │                            │
│         │  │ • Reflinks   │     │ • Rsync      │                            │
│         │  │ • Copy       │     │ • Remote ops │                            │
│         │  └──────────────┘     └──────┬───────┘                            │
│         │                              │                                     │
│         ▼                              ▼                                     │
│  ┌──────────────┐              ┌──────────────┐                             │
│  │   Models &   │              │  SSH Client  │                             │
│  │    Stores    │              │    (pkg)     │                             │
│  └──────┬───────┘              └──────────────┘                             │
│         │                                                                    │
│         ▼                                                                    │
│  ┌──────────────┐                                                           │
│  │   SQLite     │                                                           │
│  │   Database   │                                                           │
│  └──────────────┘                                                           │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
         │                              │
         ▼                              ▼
┌──────────────────┐          ┌──────────────────┐
│  qBittorrent A   │          │  qBittorrent B   │
│  (local/remote)  │          │  (local/remote)  │
└──────────────────┘          └──────────────────┘
```

## Tech Stack

| Layer | Technology |
|-------|------------|
| Backend | Go 1.21+ |
| Frontend | React 18, TypeScript, Vite |
| Database | SQLite (embedded) |
| API | REST with Chi router |
| Build | Single binary with embedded frontend |

## Project Structure

```
qui/
├── cmd/qui/              # Application entry point
│   └── main.go           # CLI, config loading, service wiring
├── internal/             # Private application code
│   ├── api/              # HTTP handlers and routes
│   ├── auth/             # Authentication (local, OIDC)
│   ├── backups/          # Backup and restore functionality
│   ├── config/           # Configuration management
│   ├── database/         # SQLite migrations and connection
│   ├── domain/           # Core domain types
│   ├── models/           # Data models and stores
│   ├── proxy/            # qBittorrent reverse proxy
│   ├── qbittorrent/      # qBittorrent sync manager
│   └── services/         # Background services
│       ├── automations/  # Rule-based automation engine
│       ├── crossseed/    # Cross-seeding service
│       ├── orphanscan/   # Orphan file detection
│       ├── reannounce/   # Tracker reannounce service
│       └── transfer/     # Torrent transfer between instances
├── pkg/                  # Reusable packages
│   ├── sshclient/        # SSH client with connection pooling
│   ├── hardlinktree/     # Hardlink tree creation
│   ├── reflinktree/      # Reflink (CoW) tree creation
│   └── fsutil/           # Filesystem utilities
├── web/                  # Frontend React application
│   └── src/
└── documentation/        # Docusaurus documentation site
```

## Core Abstractions

### Instance

An **Instance** represents a qBittorrent server connection. Each instance has:
- Connection details (URL, credentials)
- Sync settings (poll interval, filters)
- Feature flags (hardlinks, reflinks, local filesystem access)
- Optional SSH connection for remote file operations

### Sync Manager

The **Sync Manager** (`internal/qbittorrent/`) maintains real-time state for all instances:
- Polls qBittorrent APIs at configurable intervals
- Caches torrent data, categories, and tags
- Provides methods for torrent operations (add, delete, pause, etc.)
- Broadcasts updates via Server-Sent Events (SSE)

### Services

**Services** are background workers that perform automated tasks:

| Service | Purpose |
|---------|---------|
| `automations` | Evaluates rules and executes actions on matching torrents |
| `crossseed` | Finds and adds matching torrents across trackers |
| `orphanscan` | Detects files not associated with any torrent |
| `reannounce` | Fixes stalled torrents by reannouncing to trackers |
| `transfer` | Moves torrents between instances with hardlinks/rsync |

### Models & Stores

**Models** (`internal/models/`) define data structures and database access:
- Each model has a corresponding `Store` for CRUD operations
- Stores use the `dbinterface.Querier` interface for testability
- Migrations in `internal/database/migrations/`

## Request Flow

```
HTTP Request
     │
     ▼
┌─────────────────┐
│   Chi Router    │  ← Route matching, middleware
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│    Handlers     │  ← Request validation, response formatting
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│   Sync Manager  │  ← qBittorrent operations
│   or Services   │  ← Background task scheduling
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│     Stores      │  ← Database persistence
└─────────────────┘
```

## Frontend Architecture

The React frontend (`web/src/`) uses:
- **Vite** for fast development and bundling
- **TanStack Query** for server state management
- **React Router** for navigation
- **Tailwind CSS** for styling
- **shadcn/ui** components

The frontend is embedded in the Go binary at build time using `embed.FS`.

## Building

```bash
# Development (with hot reload)
cd web && pnpm dev      # Frontend on :5173
go run ./cmd/qui        # Backend on :7476

# Production build
cd web && pnpm build    # Build frontend
go build ./cmd/qui      # Embed and compile
```

## Configuration

qui uses a layered configuration approach:
1. **Defaults** - Sensible defaults for all settings
2. **Config file** - `config.toml` in data directory
3. **Environment variables** - Override any setting
4. **CLI flags** - Override for current run

See [Configuration](/docs/configuration/environment) for details.

## Database

qui uses SQLite with the following characteristics:
- Single file database in data directory
- Automatic migrations on startup
- WAL mode for concurrent reads
- Foreign keys enabled

Migrations are embedded in the binary and run sequentially on startup.

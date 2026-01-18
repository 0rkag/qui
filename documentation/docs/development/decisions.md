---
sidebar_position: 4
title: Technical Decisions
description: Key architectural decisions and their rationale.
---

# Technical Decisions

This document explains key architectural decisions made in qui's development.

## Canonical Path Model

**Decision:** Use two-step path translation via a canonical (qui server) path instead of direct instance-to-instance mappings.

**Context:** When moving torrents between instances, paths need translation because different deployments mount storage at different paths.

**Alternatives considered:**
1. **Direct N×N mappings** - Each instance maps to every other instance
2. **Canonical path model** - Each instance maps to qui's view

**Rationale:**
- N×N requires `n(n-1)` mapping configurations for n instances
- Canonical requires only `n` configurations
- Adding a new instance with canonical model requires only configuring that instance
- Canonical path matches qui's filesystem view, enabling future path validation

**Trade-offs:**
- Users must understand the canonical path concept
- Requires qui to have a consistent view of storage (usually true)

---

## Executor Pattern for Transfers

**Decision:** Abstract file operations behind a `TransferExecutor` interface with pluggable implementations.

**Context:** Transfers need to work in different deployment scenarios:
- Local filesystem access (same machine)
- SSH access (remote machines)
- Future: Agent-based access (complex networks)

**Alternatives considered:**
1. **Single implementation with conditionals** - Check deployment type inline
2. **Strategy pattern** - Select strategy at runtime
3. **Executor pattern** - Interface with `CanHandle()` for selection

**Rationale:**
- Clean separation of concerns
- Each executor handles its own complexity
- Easy to add new deployment types
- `CanHandle()` enables automatic selection based on instance config
- Testable in isolation

**Trade-offs:**
- More initial abstraction
- Shared logic requires careful extraction

---

## SSH Connection Pooling

**Decision:** Maintain a pool of SSH connections with automatic cleanup rather than creating connections per-operation.

**Context:** SSH connection establishment is expensive (handshake, key exchange). Transfers may involve many file operations.

**Alternatives considered:**
1. **Connection per operation** - Simple but slow
2. **Single persistent connection** - Fast but no concurrency
3. **Connection pool** - Balance of speed and resource use

**Rationale:**
- Reusing connections dramatically improves performance
- Pool handles dead connection detection automatically
- Configurable idle timeout prevents resource leaks
- Supports concurrent operations from multiple workers

**Implementation:**
- Connections keyed by `user@host:port`
- Background goroutine cleans up idle connections every 60s
- Default idle timeout: 5 minutes
- `IsAlive()` check before returning pooled connection

**Trade-offs:**
- More complex than simple per-operation connections
- Must handle connection state carefully

---

## SQLite for Storage

**Decision:** Use embedded SQLite instead of an external database.

**Context:** qui is distributed as a single binary. Users expect simple deployment.

**Alternatives considered:**
1. **PostgreSQL/MySQL** - Powerful but requires separate deployment
2. **Embedded SQLite** - Zero configuration, single file
3. **BoltDB/BadgerDB** - Key-value, less flexible queries

**Rationale:**
- Single binary deployment is a core value
- SQLite handles qui's workload well
- WAL mode provides good concurrent read performance
- Familiar SQL for queries
- Easy backup (copy the file)

**Configuration:**
- WAL mode enabled for concurrent reads
- Foreign keys enforced
- Busy timeout for write contention

**Trade-offs:**
- Write concurrency limited (single writer)
- Not suitable for distributed deployments
- Large databases may need occasional VACUUM

---

## Embedded Frontend

**Decision:** Embed the React frontend in the Go binary using `embed.FS`.

**Context:** qui serves both API and frontend from the same process.

**Alternatives considered:**
1. **Separate frontend deployment** - More flexible but complex
2. **Server-side rendering** - Better SEO but complex for SPA
3. **Embedded static files** - Single binary, simple deployment

**Rationale:**
- Matches single-binary philosophy
- No CORS configuration needed
- Version consistency (frontend matches backend)
- Simplified deployment and updates

**Implementation:**
```go
//go:embed all:dist
var frontendFiles embed.FS
```

**Trade-offs:**
- Larger binary size (~5MB for frontend)
- Requires rebuild for frontend changes
- Development requires running both servers

---

## State Machine for Transfers

**Decision:** Model transfer progress as an explicit state machine with database persistence.

**Context:** Transfers are long-running operations that can be interrupted.

**Alternatives considered:**
1. **Fire-and-forget** - Simple but no recovery
2. **In-memory state** - Fast but lost on restart
3. **Persisted state machine** - Recoverable, auditable

**Rationale:**
- Transfers can take minutes (large files, slow networks)
- Server restarts shouldn't lose progress
- Each state has clear recovery semantics
- State provides user visibility into progress

**States:**
```
pending → preparing → linking → adding → deleting → completed
                                                  ↓
                                               failed
```

**Trade-offs:**
- More database operations
- State machine logic adds complexity
- Must handle all state transitions carefully

---

## Longest Prefix Matching for Paths

**Decision:** Use longest prefix matching when multiple path mappings could apply.

**Context:** Users may have overlapping path mappings for specificity.

**Example:**
```
/downloads        → /data/general
/downloads/movies → /data/media/movies
```

A file at `/downloads/movies/film.mkv` could match either.

**Alternatives considered:**
1. **First match wins** - Order-dependent, confusing
2. **Reject overlaps** - Restrictive, limits flexibility
3. **Longest prefix wins** - Predictable, intuitive

**Rationale:**
- Most specific mapping should apply
- Matches how routing works in web frameworks
- Users can have general fallback with specific overrides
- Predictable behavior

**Trade-offs:**
- Slightly more complex matching logic
- Users must understand precedence

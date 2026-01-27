---
sidebar_position: 1
title: Architecture
description: E2E test architecture and design decisions.
---

# E2E Test Architecture

This document describes the architecture and design decisions for qui's end-to-end test suite.

## Overview

The e2e tests validate qui's functionality against real qBittorrent instances running in Docker containers. Tests use testcontainers-go for container lifecycle management.

## Design Principles

1. **Real Dependencies**: Tests run against actual qBittorrent instances, not mocks
2. **Isolation**: Each test gets its own container set, preventing state bleed
3. **Parallelization**: Tests use `t.Parallel()` where safe for faster execution
4. **Build Tags**: Slow/resource-intensive tests are opt-in via build tags
5. **Cleanup**: All resources are cleaned up via `t.Cleanup()` handlers

## Component Architecture

```
+---------------------------------------------------------+
|                      Test Files                          |
|  (instance_test.go, torrent_test.go, automation_test.go) |
+----------------------------+----------------------------+
                             |
                             v
+---------------------------------------------------------+
|                    internal/client                        |
|  +-------------+  +-----------------------------------+  |
|  |   api.go    |  |            types.go               |  |
|  |  - HTTP     |  |  - InstanceConfig                 |  |
|  |  - Methods  |  |  - Torrent, TorrentFile           |  |
|  |  - Auth     |  |  - Automation, AutomationPayload  |  |
|  +-------------+  +-----------------------------------+  |
+----------------------------+----------------------------+
                             |
                             v
+---------------------------------------------------------+
|                internal/containers                        |
|  +----------------------------------------------------+  |
|  |                  manager.go                         |  |
|  |  - TestEnv (orchestrates setup)                    |  |
|  |  - QBitInstance (container wrapper)                |  |
|  |  - qui server process management                   |  |
|  |  - Container config (ports, volumes, tmpfs)        |  |
|  +----------------------------------------------------+  |
+----------------------------+----------------------------+
                             |
                             v
+---------------------------------------------------------+
|                  testcontainers-go                        |
|              Docker container management                  |
+---------------------------------------------------------+
```

## Test Categories

### Basic Tests (No Build Tag)

Run with: `make test-e2e`

| Test File | Coverage |
|-----------|----------|
| `instance_test.go` | Instance lifecycle, connection, credentials |
| `torrent_test.go` | Add/remove, pause/resume, magnet handling |
| `torrent_file_ops_test.go` | File operations, rename, export |
| `torrent_tracker_ops_test.go` | Tracker operations |
| `category_tag_test.go` | Category/tag CRUD, assignment |
| `automation_test.go` | Automation CRUD, validation |
| `automation_exec_test.go` | Automation execution and preview |
| `multi_instance_test.go` | Cross-instance operations |
| `golden_test.go` | API response structure validation |
| `health_test.go` | Health endpoint checks |
| `auth_test.go` | Authentication flow |
| `preferences_test.go` | Preference management |
| `dashboard_settings_test.go` | Dashboard settings |
| `tracker_customizations_test.go` | Tracker customizations |
| `client_api_keys_test.go` | API key management |
| `log_exclusions_test.go` | Log exclusion rules |

### Download Tests (Build Tag: `download`)

Run with: `make test-e2e-download`

Tests actual torrent downloads:
- Progress tracking
- Download speed observation
- State transitions (downloading -> completed)
- File verification post-download
- Magnet link metadata resolution

### Scale Tests (Build Tag: `scale`)

Run with: `make test-e2e-scale`

Verifies qui handles multiple instances:
- Concurrent container startup
- Cross-instance queries
- Data isolation between instances
- Concurrent operations at scale

## Container Configuration

Each qBittorrent container is configured with:

- **tmpfs mount** for `/downloads` — avoids permission issues when writing to bind-mounted volumes
- **Exposed ports** for WebUI access — random ports in the 10000-60000 range with retry on conflict
- **Auto-generated admin password** — extracted from container logs during setup

## Client API Design

The test client (`internal/client/`) provides:

1. **Basic CRUD methods**: Direct API calls that fail tests on error
2. **Wait methods**: Polling with conditions for async operations
3. **Error expectation methods**: For testing validation/error cases

Example:
```go
// Blocks until condition is met or timeout
torrent := c.WaitForCondition(t, instanceID, hash, 5*time.Minute, func(tor client.Torrent) bool {
    return tor.Progress >= 1.0
})
```

## Golden File Testing

`golden_test.go` validates API response structure against known-good files in `golden/`.

Update golden files when API changes are intentional:
```bash
cd e2e && go test -v -count=1 -run TestGolden ./tests/... -update-golden
```

## Directory Structure

```
e2e/
├── internal/
│   ├── assert/        # Custom assertion helpers
│   │   ├── eventually.go  # Eventually/Never polling assertions
│   │   └── golden/        # Golden file testing utilities
│   │       └── golden.go
│   ├── client/        # qui API client for tests
│   │   ├── client.go  # Base HTTP client, auth
│   │   ├── types.go   # Request/response types
│   │   ├── torrents.go # Torrent operations
│   │   └── ...        # One file per API domain
│   └── containers/    # Docker container management
│       └── manager.go # testcontainers-go setup
├── golden/            # Golden files for API response testing
├── testdata/          # Test fixtures (.torrent files)
└── tests/             # Test files
    ├── main_test.go       # TestMain: warmup + coverage merge
    └── helpers_test.go    # Shared helpers (testMagnet, waitForInstance, etc.)
```

## Design Decisions

### Why real containers instead of mocks?

qui's core value is managing real qBittorrent instances. Mocking the qBittorrent API would test our mock, not our integration. Real containers catch issues like: authentication edge cases, API version incompatibilities, and sync timing bugs that mocks would hide.

### Why per-test isolation instead of shared containers?

Each test gets its own container set. This is more expensive but eliminates test interdependencies — tests can run in any order, in parallel, without worrying about state left behind by another test. A shared container approach would be faster but fragile: one test creating a category could break another test's assertions.

### Why the warmup phase?

Without warmup, parallel tests would all try to build the qui Docker image simultaneously, causing build races and timeouts. The warmup builds the image once and pulls the qBittorrent image before any test runs, then all tests reuse the cached image.

### Why `time.Sleep` for sync waits?

qui polls qBittorrent periodically. When a test creates a category in qBittorrent, qui won't see it until the next poll cycle. Sleep is the simplest way to wait for this. Polling-based waits (`WaitForCondition`) are used where they make sense (download progress), but for simple sync operations a short sleep is clearer and sufficient.

### Why build tags instead of separate test binaries?

Build tags (`download`, `scale`) let developers run only what's relevant. A contributor fixing category logic doesn't need to download torrents or spin up 10 instances. Tags keep the default `make test-e2e` fast and cheap while the full suite remains available.

### Why coverage via Docker instead of standard `go test -cover`?

Standard Go coverage instruments the test binary. Here, the system under test is qui running as a server inside Docker — the test binary is just an HTTP client. To measure which qui code paths the e2e tests exercise, we build a coverage-instrumented binary (`qui-cover`), run it inside the container, then extract the raw coverage data via `docker cp` after shutdown.

### Why testcontainers-go?

It provides programmatic Docker control with Go-native APIs — container lifecycle, health checks, log watching, and network management. The alternative (docker-compose + shell scripts) would work but loses type safety, makes assertions harder, and fragments the test logic across languages.

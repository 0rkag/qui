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
┌─────────────────────────────────────────────────────────────┐
│                        Test Files                           │
│  (instance_test.go, torrent_test.go, automation_test.go)   │
└─────────────────────────┬───────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────────┐
│                    internal/client                          │
│  ┌─────────────┐  ┌─────────────────────────────────────┐  │
│  │   api.go    │  │              types.go               │  │
│  │  - HTTP     │  │  - InstanceConfig                   │  │
│  │  - Methods  │  │  - Torrent, TorrentFile             │  │
│  │  - Auth     │  │  - Automation, AutomationPayload    │  │
│  └─────────────┘  └─────────────────────────────────────┘  │
└─────────────────────────┬───────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────────┐
│                  internal/containers                        │
│  ┌──────────────────────────────────────────────────────┐  │
│  │                    manager.go                         │  │
│  │  - TestEnv (orchestrates setup)                      │  │
│  │  - QBitInstance (container wrapper)                  │  │
│  │  - qui server process management                     │  │
│  │  - Container config (ports, volumes, tmpfs)          │  │
│  └──────────────────────────────────────────────────────┘  │
└─────────────────────────┬───────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────────┐
│                   testcontainers-go                         │
│              Docker container management                    │
└─────────────────────────────────────────────────────────────┘
```

## Test Categories

### Basic Tests (No Build Tag)

Run with: `make test-e2e`

| Test File | Coverage |
|-----------|----------|
| `instance_test.go` | Instance lifecycle, connection, credentials |
| `torrent_test.go` | Add/remove, pause/resume, magnet handling |
| `category_tag_test.go` | Category/tag CRUD, assignment |
| `automation_test.go` | Automation CRUD, validation |
| `multi_instance_test.go` | Cross-instance operations |
| `golden_test.go` | API response structure validation |

### Download Tests (Build Tag: `download`)

Run with: `make test-e2e-download`

Tests actual torrent downloads:
- Progress tracking
- Download speed observation
- State transitions (downloading → completed)
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

```go
HostConfigModifier: func(hc *container.HostConfig) {
    hc.PortBindings = nat.PortMap{...}
    // tmpfs mount prevents permission denied errors
    hc.Tmpfs = map[string]string{
        "/downloads": "rw,size=1g",
    }
}
```

**Why tmpfs?** The qBittorrent container's download directory may have permission issues when writing to bind-mounted volumes. Using tmpfs avoids this while providing sufficient space for test downloads.

## Client API Design

The test client (`internal/client/api.go`) provides:

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
go test -v -run TestGolden -update ./tests/...
```

## Future Enhancements

Potential areas for expansion:

- [ ] Cross-seed detection tests
- [ ] Backup/restore tests
- [ ] Reverse proxy tests
- [ ] WebSocket real-time update tests
- [ ] Authentication/authorization tests
- [ ] Rate limiting tests

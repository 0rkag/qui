# End-to-End Tests

This directory contains end-to-end tests for qui that run against real qBittorrent instances using Docker containers via testcontainers-go.

## Prerequisites

- Go 1.21+
- Docker running locally
- Network access to pull Docker images

## Running Tests

### Basic Tests (No Build Tags)

```bash
# Run all basic e2e tests
make test-e2e

# Run specific test groups
make test-e2e-instances    # Instance management tests
make test-e2e-torrents     # Torrent operations tests
```

### Download Tests

Download tests actually download torrents and verify progress, completion, and file state. They require the `download` build tag and take longer to run.

```bash
make test-e2e-download

# Or directly:
cd e2e && go test -v -count=1 -tags=download ./tests/...
```

### Scale Tests

Scale tests verify qui can handle many qBittorrent instances concurrently. They require the `scale` build tag.

```bash
make test-e2e-scale

# With custom instance count (default: 10):
QUI_E2E_SCALE_INSTANCES=50 make test-e2e-scale
```

### Full Test Suite

Run all tests including download and scale tests:

```bash
make test-e2e-full

# Or directly:
cd e2e && go test -v -count=1 -tags=download,scale ./tests/...
```

### All Tests (Unit + E2E)

```bash
make test-all
```

## Test Organization

### Build Tags

Tests are organized using Go build tags to allow selective execution:

| Tag | Tests | Description |
|-----|-------|-------------|
| (none) | Basic tests | Instance, torrent, category, tag, golden file tests |
| `download` | Download tests | Actually download torrents, verify progress/completion |
| `scale` | Scale tests | Test with many instances (default 10, configurable) |

### Test Files

| File | Description |
|------|-------------|
| `instance_test.go` | Instance CRUD, connection lifecycle |
| `torrent_test.go` | Torrent add/remove, pause/resume, magnet links |
| `torrent_download_test.go` | Download verification (requires `download` tag) |
| `category_tag_test.go` | Category and tag management |
| `automation_test.go` | Automation CRUD, validation |
| `multi_instance_test.go` | Cross-instance operations |
| `scale_test.go` | Scale testing (requires `scale` tag) |
| `golden_test.go` | API response golden file tests |
| `main_test.go` | Shared test setup |

## Configuration

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `QUI_E2E_QBIT_IMAGE` | `ghcr.io/hotio/qbittorrent:release-5.0.2` | qBittorrent Docker image |
| `QUI_E2E_SCALE_INSTANCES` | `10` | Number of instances for scale tests |

### Testing Different qBittorrent Versions

```bash
# Test against qBittorrent 4.6.7
QUI_E2E_QBIT_IMAGE=ghcr.io/hotio/qbittorrent:release-4.6.7 make test-e2e

# Test against qBittorrent 5.1.4
QUI_E2E_QBIT_IMAGE=ghcr.io/hotio/qbittorrent:release-5.1.4 make test-e2e
```

## Test Data

Test data files are located in `testdata/`:

```
testdata/
└── torrents/          # .torrent files for download tests
    ├── wired-cd.torrent
    ├── sintel.torrent
    ├── big-buck-bunny.torrent
    └── README.md      # Attribution info
```

These are public domain/Creative Commons torrents from WebTorrent for testing purposes.

## Architecture

```
e2e/
├── internal/
│   ├── client/        # qui API client for tests
│   │   ├── api.go     # API methods
│   │   └── types.go   # Request/response types
│   └── containers/    # Docker container management
│       └── manager.go # testcontainers-go setup
├── golden/            # Golden files for API response testing
├── testdata/          # Test fixtures
└── tests/             # Test files
```

### Container Management

Tests use testcontainers-go to:
1. Start qBittorrent containers with proper configuration
2. Start a qui instance connected to those containers
3. Provide clients for API interaction
4. Clean up containers after tests

Containers are configured with:
- tmpfs mount for `/downloads` (avoids permission issues)
- Exposed ports for WebUI access
- Auto-generated admin password

## Cleanup

If tests fail and leave containers running:

```bash
make test-e2e-clean
```

This removes all testcontainers and prunes Docker networks.

## Writing New Tests

1. Add tests to existing `*_test.go` files or create new ones
2. Use build tags if tests are slow or resource-intensive:
   ```go
   //go:build download

   package tests
   ```
3. Use the provided helpers:
   - `containers.Setup()` - Start containers
   - `env.Client()` - Get API client
   - `waitForInstance()` - Wait for instance to connect
4. Always cleanup in tests using `t.Cleanup()`
5. Use golden files for complex API response assertions

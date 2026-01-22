---
sidebar_position: 1
title: Testing
description: Running tests for qui development.
---

# Testing

qui uses a combination of unit tests and end-to-end tests to ensure reliability.

## Unit Tests

Run unit tests with race detection:

```bash
make test
```

## End-to-End Tests

E2E tests run against real qBittorrent instances using Docker containers via testcontainers-go.

### Prerequisites

- Go 1.21+
- Docker running locally

### Running E2E Tests

```bash
# Basic e2e tests
make test-e2e

# Specific test groups
make test-e2e-instances    # Instance management
make test-e2e-torrents     # Torrent operations

# Download tests (actually downloads torrents - slower)
make test-e2e-download

# Scale tests (tests with many instances)
make test-e2e-scale

# Full suite (all tests including download and scale)
make test-e2e-full

# All tests (unit + full e2e)
make test-all
```

### Testing Different qBittorrent Versions

```bash
# Test against qBittorrent 4.6.7
QUI_E2E_QBIT_IMAGE=ghcr.io/hotio/qbittorrent:release-4.6.7 make test-e2e

# Test against qBittorrent 5.1.4
QUI_E2E_QBIT_IMAGE=ghcr.io/hotio/qbittorrent:release-5.1.4 make test-e2e
```

### Cleanup

If tests fail and leave containers running:

```bash
make test-e2e-clean
```

### Full Documentation

For complete e2e test documentation including architecture details, see:

- **[e2e/README.md](https://github.com/autobrr/qui/blob/develop/e2e/README.md)** - Test organization, configuration, and usage
- **[e2e/plans/ARCHITECTURE.md](https://github.com/autobrr/qui/blob/develop/e2e/plans/ARCHITECTURE.md)** - Technical architecture and design decisions

## OpenAPI Validation

Validate the OpenAPI specification:

```bash
make test-openapi
```

## Linting

```bash
# Lint changed files only (fast)
make lint

# Lint entire codebase
make lint-full

# Auto-fix where possible
make lint-fix
```

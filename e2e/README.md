# End-to-End Tests

E2E tests validate qui against real qBittorrent instances running in Docker containers via testcontainers-go.

## Quick Start

```bash
# Prerequisites: Go 1.25+, Docker running

# Run core tests (~5 min)
make test-e2e

# Run full suite including downloads and scale tests (~10 min)
make test-e2e-full

# Clean up containers after a failed run
make test-e2e-clean
```

First run builds a Docker image from source and pulls qBittorrent — if it times out:

```bash
QUI_E2E_WARMUP_TIMEOUT=10 make test-e2e
```

## Documentation

For full documentation see the [Development > Testing](../documentation/docs/development/testing.md) section:

- **[Testing Guide](../documentation/docs/development/testing.md)** — When to run tests, make targets, env vars, troubleshooting, coverage
- **[E2E Architecture](../documentation/docs/development/e2e/architecture.md)** — Component architecture and design decisions
- **[Writing E2E Tests](../documentation/docs/development/e2e/writing-tests.md)** — Annotated example and contributor checklist

## Directory Structure

```
e2e/
├── internal/
│   ├── assert/        # Custom assertion helpers
│   ├── client/        # qui API client for tests
│   └── containers/    # Docker container management
├── golden/            # Golden files for API response testing
├── testdata/          # Test fixtures (.torrent files)
└── tests/             # Test files
```

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

E2E tests run against real qBittorrent instances using Docker containers via testcontainers-go. They verify qui's behavior end-to-end but are resource-intensive (~5 min warm, ~10 min cold start), so running them selectively matters.

### When to Run E2E Tests

E2E tests are a developer responsibility, not a CI gate. Run them before opening PRs with backend changes.

**Run e2e tests when you change:**
- Backend behavior — API endpoints, business logic, data flow
- Container/instance management — how qui connects to or manages qBittorrent
- Authentication or authorization logic
- Cross-instance operations or aggregation

**Skip e2e tests when you change:**
- Frontend only (`web/`) — unless it reveals an API contract change
- Documentation, CI config, linting rules
- Build/release tooling (`distrib/`, `.goreleaser.yml`)

**Which tier to run depends on what you touched:**
- Most backend changes: `make test-e2e` (basic suite, ~5 min)
- Download path or torrent transfer logic: `make test-e2e-download` — these tests download real torrent data, which adds significant time and bandwidth depending on your connection
- Multi-instance scaling or concurrency: `make test-e2e-scale`
- Before opening a PR with significant backend changes: `make test-e2e-full`

When in doubt, `make test-e2e` is the safe default — it covers the core API surface without the overhead of download or scale tests.

### Prerequisites

- **Go 1.25+**
- **Docker** — any Docker-compatible runtime (Docker Desktop, OrbStack, Colima, Podman with Docker socket)
- **~4 GB free RAM** — each test spins up 1-3 qBittorrent containers + a qui container. Scale tests use more.
- **Network access** — first run pulls Docker images (~1 GB total for base images + qBittorrent)

### First Run

The first run builds a Docker image from source (frontend + Go binary) and pulls the qBittorrent image. This is significantly slower than subsequent runs due to Docker layer caching.

If the warmup times out on first run, increase the timeout:

```bash
QUI_E2E_WARMUP_TIMEOUT=10 make test-e2e
```

### Make Targets

| Target | What it runs | Build tags | Notes |
|--------|-------------|------------|-------|
| `make test-e2e` | Core API tests | none | Safe default for most backend changes |
| `make test-e2e-download` | Torrent download tests | `download` | Downloads real data — slow, bandwidth-heavy |
| `make test-e2e-scale` | Multi-instance scale tests | `scale` | Spins up many containers, resource-heavy |
| `make test-e2e-full` | All of the above | `download,scale` | Full validation before PRs |
| `make test-e2e-coverage` | Core tests + coverage | none | Outputs `e2e/coverage.out` |
| `make test-e2e-coverage-full` | Full suite + coverage | `download,scale` | Complete coverage report |
| `make test-e2e-clean` | Remove test containers | — | Use after failed runs or to free resources |

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `QUI_E2E_QBIT_IMAGE` | `linuxserver/qbittorrent:5.1.4` | qBittorrent Docker image. Shorthand versions supported: `4.6.7`, `5.0.2`, `5.1.4`, `5.1.4-libtorrentv1` |
| `QUI_E2E_SCALE_INSTANCES` | `10` | Number of qBittorrent instances for scale tests. Higher values need more RAM and ports. |
| `QUI_E2E_COVERAGE` | unset | Set to `1` to enable coverage collection. Builds a coverage-instrumented binary and collects data via `docker cp` after shutdown. |
| `QUI_E2E_WARMUP_TIMEOUT` | `5` | Warmup timeout in minutes. Increase for first runs where Docker images must be pulled and built from scratch. |

### Testing Different qBittorrent Versions

```bash
# Using short names
QUI_E2E_QBIT_IMAGE=4.6.7 make test-e2e
QUI_E2E_QBIT_IMAGE=5.0.2 make test-e2e
QUI_E2E_QBIT_IMAGE=5.1.4-libtorrentv1 make test-e2e

# Using full image names
QUI_E2E_QBIT_IMAGE=linuxserver/qbittorrent:4.6.7 make test-e2e
```

### Analyzing Test Results

Tests run with `-v` (verbose), so every test and subtest prints its result:

```
--- PASS: TestCategories (24.43s)
    --- PASS: TestCategories/create_and_list_categories (1.11s)
    --- PASS: TestCategories/edit_category (2.10s)
```

**What to look for in failures:**

```
--- FAIL: TestGoldenResponses/categories/list (0.05s)
    golden_test.go:48: golden mismatch (-want +got):
        ... diff output ...
```

- The file and line number (`golden_test.go:48`) point directly to the failing assertion
- Diff output uses `-want +got` format — minus lines are expected, plus lines are actual
- Container logs appear inline when tests emit `t.Log()` output

**Parallel test output is interleaved.** Run a single test in isolation:

```bash
cd e2e && go test -v -count=1 -run TestCategories ./tests/...
```

### Coverage Reports

Coverage is collected by running a coverage-instrumented qui binary inside Docker. After all tests complete, raw coverage data is merged into a standard Go coverage file.

**Generating coverage:**

```bash
make test-e2e-coverage          # core tests only
make test-e2e-coverage-full     # full suite
```

Both produce `e2e/coverage.out` and print a summary line:

```
total:    (statements)    16.9%
Coverage report: /path/to/e2e/coverage.out
```

**Viewing detailed coverage:**

```bash
go tool cover -html=e2e/coverage.out
```

Opens a browser with per-file, line-by-line highlighting — green (covered), red (uncovered), grey (not instrumented).

**Viewing per-function coverage:**

```bash
go tool cover -func=e2e/coverage.out
```

Prints a table of every function and its coverage percentage — useful for spotting untested functions without opening a browser.

**What the number means:**
This measures which qui backend code paths the e2e tests exercise. It does *not* include unit test coverage — those are separate (`make test`). A low overall percentage is normal since e2e tests focus on critical paths, not exhaustive branch coverage.

### Troubleshooting

**Warmup times out on first run**

The Docker image build (frontend + Go binary) can exceed the default 5-minute timeout when Docker has no layer cache. Increase it:

```bash
QUI_E2E_WARMUP_TIMEOUT=10 make test-e2e
```

**"Address already in use" errors**

Tests allocate random ports in the 10000-60000 range. The framework retries automatically (up to 3 attempts), but if ports are exhausted or stuck, clean up and retry:

```bash
make test-e2e-clean
```

**Leftover containers after a failed run**

Crashed or interrupted tests may leave orphaned containers. Clean them up with:

```bash
make test-e2e-clean
```

**Docker not running or not accessible**

The framework uses testcontainers-go which respects standard Docker environment variables:

| Variable | Description |
|----------|-------------|
| `DOCKER_HOST` | Docker daemon socket or remote host (e.g., `unix:///var/run/docker.sock`, `tcp://localhost:2375`) |
| `DOCKER_TLS_VERIFY` | Enable TLS verification for remote Docker hosts |
| `DOCKER_CERT_PATH` | Path to TLS certificates for remote Docker |
| `TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE` | Override the socket path used inside containers (for Ryuk) |

If unset, testcontainers defaults to `/var/run/docker.sock`. Users of Colima, Podman, or remote Docker hosts may need to set `DOCKER_HOST` accordingly.

**Golden test failures after API changes**

If you changed API response shapes, golden files need updating:

```bash
cd e2e && go test -v -count=1 -run TestGolden ./tests/... -update-golden
```

**Tests are slow or containers keep restarting**

Check Docker resource allocation. The test suite runs multiple containers in parallel — insufficient RAM causes container OOM kills and restarts.

### Further Reading

- **[E2E Architecture](./e2e/architecture.md)** — Component architecture and design decisions
- **[Writing E2E Tests](./e2e/writing-tests.md)** — Annotated example and contributor checklist

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

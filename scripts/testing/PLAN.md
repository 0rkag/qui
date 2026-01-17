# Transfer Testing Plan

## Current State

### What We Have

1. **Unit Tests** (`internal/services/transfer/*_test.go`)
   - Mock-based tests for LocalExecutor
   - Tests for worker, requests, recovery
   - Good coverage of business logic

2. **Integration Tests** (`internal/services/transfer/integration_test.go`)
   - Tagged with `//go:build integration`
   - Tests local filesystem operations (hardlinks, rollback)
   - Uses mocks for qBittorrent API
   - Run with: `go test -tags=integration ./internal/services/transfer/...`

3. **E2E Bash Script** (`scripts/testing/run-test.sh`)
   - Spins up real qBittorrent containers
   - Tests actual SSH/rsync transfers
   - Manual execution only

### What's Missing

#### 1. SSH Executor Tests
The `SSHExecutor` in `ssh.go` has **no tests**. It handles:
- Remote-to-remote rsync transfers
- Path mapping resolution via SSH
- SSH connection management

#### 2. Hardlink Transfer Tests
Current bash script uses separate volumes per container, so all transfers use rsync.
Need to test hardlink mode (same filesystem).

#### 3. Test Scenarios Not Covered
| Scenario | Current Coverage |
|----------|-----------------|
| Basic rsync transfer | ✅ bash script |
| Hardlink transfer (same FS) | ❌ |
| Transfer with deleteFromSource | ❌ |
| Transfer with category/tag preservation | ❌ |
| Transfer cancellation | ❌ |
| Concurrent transfers | ❌ |
| Partial download transfer | ❌ |
| Transfer failure recovery | Unit tests only |
| Path mapping edge cases | ❌ |

#### 4. Verification
- No file integrity checks (checksums)
- No verification that torrent is properly seeding on target

---

## Integration Plan

### Option A: Enhanced Bash Script (Quick Win)

Make `run-test.sh` CI-friendly and add more scenarios.

**Pros:**
- Already works
- Easy to debug
- Tests real system behavior

**Cons:**
- Slow (container startup, torrent download)
- Requires Docker
- Not integrated with `go test`

**Implementation:**
```makefile
# Add to Makefile
test-e2e:
    @echo "Running E2E transfer tests..."
    cd scripts/testing && ./run-test.sh

test-e2e-clean:
    cd scripts/testing && ./run-test.sh cleanup
```

### Option B: Go Integration Tests with testcontainers-go

Create Go-based integration tests that programmatically manage Docker containers.

**Pros:**
- Integrated with `go test`
- Better assertions and error reporting
- Can run subset of tests

**Cons:**
- More complex setup
- Need to rewrite container management in Go

**Implementation:**
```go
// internal/services/transfer/e2e_test.go
//go:build e2e

func TestE2E_TransferViaSSH(t *testing.T) {
    // Use testcontainers-go to start qBittorrent
    ctx := context.Background()
    qbit1 := startQBitContainer(t, ctx, 8081)
    qbit2 := startQBitContainer(t, ctx, 8082)
    defer qbit1.Terminate(ctx)
    defer qbit2.Terminate(ctx)

    // Setup SSH, add torrent, trigger transfer...
}
```

### Option C: Hybrid Approach (Recommended)

1. **Keep bash script** for full E2E validation
2. **Add SSH executor unit tests** with mocked SSH client
3. **Add integration tests** for path mapping and filesystem operations
4. **CI runs bash script** as a separate job

---

## Recommended Implementation

### Phase 1: Unit Tests for SSH Executor

Add `ssh_test.go` with mocked SSH operations:

```go
// internal/services/transfer/ssh_test.go
func TestSSHExecutor_Prepare(t *testing.T) {
    // Test path mapping resolution
    // Test torrent metadata fetching
}

func TestSSHExecutor_CreateLinks_Rsync(t *testing.T) {
    // Mock SSH client
    // Verify rsync command construction
}

func TestSSHExecutor_DetermineLinkMode(t *testing.T) {
    // Test same filesystem detection via SSH
}
```

### Phase 2: Enhance Bash Script

```bash
# Add to run-test.sh

# Test hardlinks (add shared volume)
test_hardlink_transfer() {
    # Configure qbit1 and qbit2 with same volume
    # Transfer should use hardlinks
}

# Test delete from source
test_delete_source() {
    # Transfer with deleteFromSource=true
    # Verify torrent removed from source
}

# Test cancellation
test_cancel_transfer() {
    # Start large transfer
    # Cancel mid-way
    # Verify rollback
}
```

### Phase 3: CI Integration

```yaml
# .github/workflows/test.yml
jobs:
  unit-tests:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - run: go test ./...

  integration-tests:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - run: go test -tags=integration ./...

  e2e-tests:
    runs-on: ubuntu-latest
    needs: [unit-tests, integration-tests]
    steps:
      - uses: actions/checkout@v4
      - run: cd scripts/testing && ./run-test.sh
```

### Phase 4: Makefile Targets

```makefile
# Testing targets
test:
    go test ./...

test-integration:
    go test -tags=integration ./...

test-e2e:
    cd scripts/testing && ./run-test.sh

test-e2e-clean:
    cd scripts/testing && ./run-test.sh cleanup

test-all: test test-integration test-e2e
```

---

## Immediate TODOs

1. [x] Add `test-e2e` target to Makefile
2. [x] Create `ssh_test.go` with mocked SSH client tests
3. [x] Add hardlink test scenario to bash script (shared volume)
4. [x] Add test for `deleteFromSource` option
5. [x] Add basic CI workflow for E2E tests
6. [x] Document test requirements (see below)

---

## Test Requirements

### Unit Tests (`go test ./...`)
- Go 1.23+
- No external dependencies

### Integration Tests (`go test -tags=integration ./internal/services/transfer/...`)
- Go 1.23+
- Filesystem access for link mode tests

### E2E Tests (`./scripts/testing/run-test.sh`)
- Docker with compose support
- Go 1.23+ (for building qui)
- jq (for JSON parsing)
- ssh-keygen (for generating test SSH keys)
- curl (for API calls)

**Usage:**
```bash
# Run tests (auto-cleanup after completion)
./run-test.sh

# Run tests and keep environment for debugging
./run-test.sh --keep

# Cleanup only
./run-test.sh cleanup
```

---

## Future Improvements

- [ ] Use testcontainers-go for better Go integration
- [ ] Add performance benchmarks for large transfers
- [ ] Add stress tests for concurrent transfers
- [ ] Add chaos testing (kill containers mid-transfer)

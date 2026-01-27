---
sidebar_position: 2
title: Writing Tests
description: How to write and extend e2e tests.
---

# Writing E2E Tests

This guide walks through the anatomy of an e2e test using an annotated example, followed by a checklist for common contribution scenarios.

## Annotated Example

Based on `TestCategories` in `category_tag_test.go` — this is the pattern most new tests will follow.

### Test Setup

```go
package tests // All e2e tests live in this package

import (
    "context"
    "testing"
    "time"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"

    "github.com/autobrr/qui/e2e/internal/client"     // HTTP client for qui API
    "github.com/autobrr/qui/e2e/internal/containers"  // Docker container orchestration
)

func TestCategories(t *testing.T) {
    // Skip in short mode (go test -short) so unit test runs aren't slowed.
    if testing.Short() {
        t.Skip("skipping e2e test")
    }

    // IMPORTANT: Always call t.Parallel() unless your test modifies
    // global state (e.g. auth tests that change passwords).
    t.Parallel()

    // Every test gets its own isolated container environment:
    // - a fresh Docker network
    // - N qBittorrent containers (here: 1)
    // - a fresh qui container
    ctx := context.Background()
    env := containers.Setup(ctx, t, containers.DefaultConfig(), 1)
    t.Cleanup(func() { env.Teardown(ctx) })

    // env.Client() returns an HTTP client pointed at this test's qui instance.
    // env.Instances[] holds qBittorrent connection details (URL, password).
    c := env.Client()
    qbit := env.Instances[0]

    // Register the qBittorrent instance with qui — most tests need this.
    instanceID := c.CreateInstance(t, client.InstanceConfig{
        Name:     "category-test",
        Host:     qbit.URL,
        Username: "admin",
        Password: qbit.Password,
    })
    t.Cleanup(func() { c.DeleteInstance(t, instanceID) })

    // Wait until qui has connected to and synced with qBittorrent.
    waitForInstance(t, c, instanceID)
```

### Subtests

```go
    // Subtests run sequentially within this test function.
    // Each subtest should clean up after itself so later subtests
    // start from a known state.

    t.Run("create and list categories", func(t *testing.T) {
        // Client methods accept testing.T and fail the test on error —
        // no need for manual error checking.
        c.CreateCategory(t, instanceID, "movies", "/downloads/movies")

        // Always clean up resources you create. t.Cleanup runs even
        // if the test fails, preventing state leaks between subtests.
        t.Cleanup(func() { c.DeleteCategory(t, instanceID, "movies") })

        // qui syncs with qBittorrent asynchronously. Sleep gives the
        // sync loop time to pick up changes. This is expected — we're
        // testing through two systems (qui -> qBittorrent).
        time.Sleep(time.Second)

        categories := c.GetCategories(t, instanceID)

        // Use require when a failure means subsequent lines would panic
        // (e.g. nil map access). Use assert for additional checks that
        // can fail independently.
        require.Contains(t, categories, "movies")
        assert.Equal(t, "/downloads/movies", categories["movies"].SavePath)
    })

    t.Run("set category on torrent via magnet", func(t *testing.T) {
        c.CreateCategory(t, instanceID, "tv-shows", "/downloads/tv")
        t.Cleanup(func() { c.DeleteCategory(t, instanceID, "tv-shows") })

        // Adding a torrent via magnet link — a common setup step.
        // testMagnet is a constant defined in helpers_test.go.
        hash := c.AddTorrentFromMagnet(t, instanceID, testMagnet, client.AddTorrentOptions{
            Paused: true, // Add paused to avoid downloading data
        })
        t.Cleanup(func() { c.DeleteTorrent(t, instanceID, hash, true) })

        time.Sleep(time.Second)

        // Mutate state through qui's API, then verify via a read.
        c.SetCategory(t, instanceID, []string{hash}, "tv-shows")

        // Longer sleep — qui must sync the category change back from
        // qBittorrent, which happens on the next poll cycle.
        time.Sleep(2 * time.Second)

        torrent := c.GetTorrent(t, instanceID, hash)
        assert.Equal(t, "tv-shows", torrent.Category)
    })

    t.Run("set category on torrent from file", func(t *testing.T) {
        c.CreateCategory(t, instanceID, "docs", "/downloads/docs")
        t.Cleanup(func() { c.DeleteCategory(t, instanceID, "docs") })

        // Adding a torrent from a .torrent file instead of a magnet link.
        // Use testdataPath() to resolve files from e2e/testdata/.
        // Available test torrents: wired-cd.torrent, sintel.torrent,
        // big-buck-bunny.torrent (see e2e/testdata/torrents/).
        torrentFile := testdataPath("torrents/sintel.torrent")
        hash := c.AddTorrentFromFile(t, instanceID, torrentFile, client.AddTorrentOptions{
            Paused: true, // Always pause unless testing downloads
        })
        t.Cleanup(func() { c.DeleteTorrent(t, instanceID, hash, true) })

        time.Sleep(time.Second)

        c.SetCategory(t, instanceID, []string{hash}, "docs")

        time.Sleep(2 * time.Second)

        torrent := c.GetTorrent(t, instanceID, hash)
        assert.Equal(t, "docs", torrent.Category)
    })
}
```

### Key Patterns Summary

| Pattern | When to use |
|---------|------------|
| `testing.Short()` skip | Always — lets `go test -short` skip e2e |
| `t.Parallel()` | Always, unless test modifies global state (auth, preferences) |
| `containers.Setup(ctx, t, cfg, N)` | Every test — N is the number of qBittorrent instances |
| `t.Cleanup()` | Every resource you create (containers, instances, torrents, categories) |
| `time.Sleep(time.Second)` | After mutations — qui syncs with qBittorrent asynchronously |
| `time.Sleep(2 * time.Second)` | After mutations that go through qBittorrent and back to qui |
| `require.X` | When failure would cause a panic on subsequent lines |
| `assert.X` | For checks that can fail independently |
| `testMagnet` | Quick torrent setup via magnet link |
| `testdataPath("torrents/X")` | Torrent setup from `.torrent` file |

## Contributor Checklist

### I added a new API endpoint

- [ ] Add client method(s) in `e2e/internal/client/` — accept `testing.T`, call `t.Fatal` on errors, return typed responses
- [ ] Add test cases in an existing test file or create a new `*_test.go` file
- [ ] If the endpoint returns a new response shape, consider adding a golden file in `e2e/golden/`

### I changed an API response shape

- [ ] Update types in `e2e/internal/client/types.go`
- [ ] Update golden files: `cd e2e && go test -v -count=1 -run TestGolden ./tests/... -update-golden`
- [ ] Review existing tests that assert on the changed fields

### I changed backend business logic

- [ ] Run `make test-e2e` to check existing tests still pass
- [ ] Add or update test cases covering the changed behavior
- [ ] If the change affects downloads or transfers: run `make test-e2e-download`
- [ ] If the change affects multi-instance behavior: run `make test-e2e-scale`

### I added a new qBittorrent integration

- [ ] Check if `containers.DefaultConfig()` needs changes (e.g. new env vars, mounts)
- [ ] Add client methods for the new qBittorrent-facing behavior
- [ ] Add tests with appropriate sync sleeps (qui polls qBittorrent asynchronously)

### New test file conventions

- [ ] Package: `tests`
- [ ] Add `testing.Short()` skip guard
- [ ] Call `t.Parallel()` unless the test modifies global state (auth, preferences)
- [ ] Use `t.Cleanup()` for all created resources
- [ ] Use `require` for checks that would cause panics on failure, `assert` for everything else
- [ ] If the test needs special resources (bandwidth, many instances), gate it behind a build tag (`download`, `scale`)

---
sidebar_position: 2
title: SSH Client
description: SSH client package with connection pooling and file operations.
---

# SSH Client

The SSH client package (`pkg/sshclient/`) provides SSH connectivity with connection pooling, command execution, and file operations. It's used by the [SSHExecutor](/docs/development/modules/transfer-service#sshexecutor) for remote file operations.

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                          Pool                                │
│  ┌────────────────────────────────────────────────────────┐ │
│  │  map[configKey]*pooledClient                           │ │
│  │                                                         │ │
│  │  "user@host:22" → Client (live, lastUsed: 10s ago)     │ │
│  │  "admin@nas:22" → Client (idle, lastUsed: 3m ago)      │ │
│  └────────────────────────────────────────────────────────┘ │
│                              │                               │
│                              ▼                               │
│        Background cleanup goroutine (every 60s)             │
│        Removes connections idle > 5 minutes                 │
└─────────────────────────────────────────────────────────────┘
```

## Components

### Config

```go
type Config struct {
    Host                    string        // SSH server hostname
    Port                    int           // SSH port (default: 22)
    Username                string        // SSH username
    PrivateKeyPath          string        // Path to private key file
    Password                string        // Password (alternative to key)
    Timeout                 time.Duration // Connection timeout
    ExpectedHostKey         *HostKeyInfo  // Expected host key for verification
    SkipHostKeyVerification bool          // Skip verification (insecure)
}

type HostKeyInfo struct {
    Fingerprint string // SHA256 fingerprint (e.g., "SHA256:...")
    Algorithm   string // Key algorithm (e.g., "ssh-ed25519")
}
```

### Client

The `Client` wraps an SSH connection with high-level operations:

```go
// Command execution
Exec(ctx, cmd string) (*ExecResult, error)
ExecSimple(ctx, cmd string) (string, error)  // Fails on non-zero exit

// File operations
MkdirAll(ctx, path string) error
Exists(ctx, path string) (bool, error)
IsDir(ctx, path string) (bool, error)
Hardlink(ctx, src, dst string) error
Reflink(ctx, src, dst string) error
Copy(ctx, src, dst string) error
Remove(ctx, path string) error

// Health check
IsAlive() bool
```

### Pool

Connection pooling for efficiency:

```go
pool := sshclient.NewPool(5 * time.Minute)  // Idle timeout

// Get or create connection
client, err := pool.Get(cfg)

// Connection is returned to pool automatically
// Don't call client.Close() - pool manages lifecycle

pool.Close()  // Shutdown all connections
```

## Command Execution

Commands run with context support and graceful cancellation:

```go
result, err := client.Exec(ctx, "ls -la /data")
if err != nil {
    return err
}

fmt.Println(result.Stdout)
fmt.Println(result.Stderr)
fmt.Println(result.ExitCode)
```

On context cancellation:
1. Sends SIGKILL to remote process
2. Closes the SSH session
3. Returns context error

## File Transfers

The package supports rsync and SCP transfers:

```go
// Push local to remote
sshclient.RsyncPush(ctx, cfg, "/local/path/", "/remote/path/", opts)

// Pull remote to local
sshclient.RsyncPull(ctx, cfg, "/remote/path/", "/local/path/", opts)

// Remote to remote (executed on source)
sshclient.RsyncRemoteToRemote(ctx, srcCfg, dstCfg, srcPath, dstPath, opts)
```

Transfer options:
```go
type TransferOptions struct {
    PreservePermissions bool  // rsync -p
    Delete              bool  // rsync --delete
    DryRun              bool  // rsync -n
}
```

## Security

### Host Key Verification (TOFU)

The SSH client implements Trust-On-First-Use (TOFU) host key verification:

```go
// First connection: captures and stores host key fingerprint
client, hostKeyInfo, err := sshclient.New(cfg)
// hostKeyInfo contains the fingerprint to store in database

// Subsequent connections: verify against stored fingerprint
cfg.ExpectedHostKey = &sshclient.HostKeyInfo{
    Fingerprint: storedFingerprint,  // e.g., "SHA256:..."
    Algorithm:   storedAlgorithm,    // e.g., "ssh-ed25519"
}
client, _, err := sshclient.New(cfg)
// Returns HostKeyError if fingerprint doesn't match
```

**Host key verification for external commands (rsync/scp):**

When using rsync or scp, the client:
1. Captures the host key via SSH connection
2. Creates a temporary `known_hosts` file with the verified key
3. Runs rsync/scp with `-o StrictHostKeyChecking=yes -o UserKnownHostsFile=<temp>`
4. Cleans up the temporary file after command completes

This ensures MITM protection even for external process invocations.

### Path Validation

All path operations validate inputs using the shared `pathutil` package:

```go
import "github.com/autobrr/qui/pkg/pathutil"

// SSH requires absolute paths
if err := pathutil.ValidateAbsolute(path); err != nil {
    return err
}

// Checks for:
// - Empty paths
// - Null bytes (security issue)
// - Path traversal attempts (..)
// - Non-absolute paths (when required)
```

### Shell Quoting

User-provided values are escaped to prevent injection:

```go
func ShellQuote(s string) string {
    return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

// Usage
cmd := fmt.Sprintf("ln %s %s", ShellQuote(src), ShellQuote(dst))
```

### Config Validation

SSH configs are validated before use:
- Username must be alphanumeric (with `-`, `_`, `.`)
- Hostname must be valid
- Port must be in range 1-65535

## Integration

The SSH client is used by SSHExecutor in the transfer service:

```go
type SSHExecutor struct {
    sshPool *sshclient.Pool
    // ...
}

func (e *SSHExecutor) CreateLinks(ctx, t, prep) (int, error) {
    client, err := e.sshPool.Get(e.buildConfig(prep.TargetInstance))
    if err != nil {
        return 0, err
    }

    for _, file := range prep.Files {
        err := client.Hardlink(ctx, file.SourcePath, file.TargetPath)
        if err != nil {
            return linked, err
        }
        linked++
    }
    return linked, nil
}
```

## Key Files

| File | Purpose |
|------|---------|
| `client.go` | Client struct, command execution, file ops, host key verification |
| `pool.go` | Connection pooling with cleanup and host key support |
| `transfer.go` | Rsync and SCP operations with secure host key handling |
| `sftp.go` | SFTP client for file transfers |
| `sftp_transfer.go` | SFTP upload/download with progress tracking |

### Related Packages

| Package | Purpose |
|---------|---------|
| `pkg/pathutil` | Shared path validation (traversal, absolute path checks) |

## Testing

```bash
# Unit tests
go test ./pkg/sshclient/...

# Integration tests (requires SSH server)
SSH_TEST_HOST=localhost \
SSH_TEST_USER=testuser \
SSH_TEST_KEY=~/.ssh/test_key \
go test -tags=integration ./pkg/sshclient/...
```

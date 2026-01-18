---
sidebar_position: 3
title: Remote Instances
description: Connect to qBittorrent instances on remote machines via SSH.
---

# Remote Instances (SSH)

:::warning[Work in Progress]
This feature is under active development. The UI and behavior may change.
:::

When qBittorrent runs on a different machine than qui, you can configure SSH connections to enable file operations like [transfers](/docs/features/transfers). qui connects via SSH to create hardlinks, reflinks, or transfer files using rsync.

:::tip[Related Documentation]
- **[Torrent Transfers](/docs/features/transfers)** — Move torrents between instances (includes setup checklist)
- **[Path Mappings](/docs/features/path-mappings)** — Translate paths between instance views
:::

## When You Need SSH

Configure SSH when:
- qBittorrent is on a remote server (seedbox, VPS, NAS)
- qui runs on a different machine than qBittorrent
- You want to transfer torrents between instances on different machines

You **don't** need SSH when:
- qui and qBittorrent are on the same machine
- qui can directly access the qBittorrent filesystem (e.g., shared Docker volumes)

## Prerequisites

### On the Remote Machine

1. **SSH server running** (OpenSSH or compatible)
2. **rsync installed** for file transfers
3. **User account** with access to torrent files

### On the qui Machine

1. **SSH private key** (passwordless authentication)
2. **rsync installed** for file transfers
3. Network access to the remote machine

### Generate SSH Keys (if needed)

```bash
# On the qui machine
ssh-keygen -t ed25519 -f ~/.ssh/qui_key -N ""

# Copy public key to remote machine
ssh-copy-id -i ~/.ssh/qui_key.pub user@remote-host
```

## Quick Start

1. Go to **Instances** and click the gear icon on an instance
2. Open the **Connection** tab
3. Toggle **Enable SSH Connection**
4. Fill in the connection details
5. Click **Test Connection** to verify (qui will attempt to connect and run a simple command)
6. Click **Save**

:::tip
Always test the connection before saving. qui validates that it can establish an SSH session and execute commands on the remote host.
:::

## Configuration

### SSH Connection Settings

| Setting | Description | Example |
|---------|-------------|---------|
| **Host** | Hostname or IP address | `seedbox.example.com` or `192.168.1.100` |
| **Port** | SSH port | `22` (default) |
| **Username** | SSH login user | `media` |
| **Private Key Path** | Path to SSH private key on qui machine | `/home/qui/.ssh/id_ed25519` |

### Instance Settings

| Setting | Description |
|---------|-------------|
| **Has Local Filesystem Access** | Disable this for remote instances |

When disabled, qui knows to use SSH for file operations instead of direct filesystem access.

## How It Works

### File Operations via SSH

qui executes commands over SSH:
- `mkdir -p` to create directories
- `ln` for hardlinks (same machine)
- `cp --reflink=auto` for reflinks
- `cp` for copies

### File Transfers via rsync

When transferring between machines, qui uses rsync:

| Scenario | Method |
|----------|--------|
| Local qui → Remote instance | `rsync` push to remote |
| Remote instance → Local qui | `rsync` pull from remote |
| Remote → Remote (different hosts) | `rsync` executed on source, pushing to destination |

rsync preserves permissions and handles large files efficiently with delta transfers.

## Connection Pooling

qui maintains a pool of SSH connections to avoid reconnecting for every operation:
- Connections are reused for 5 minutes of inactivity
- Dead connections are automatically replaced
- Multiple operations can share connections

## Security Considerations

### Key-Based Authentication Only

qui uses SSH keys exclusively. Password authentication is not supported. This is more secure and enables unattended operation.

### Limit User Permissions

Create a dedicated user for qui with minimal permissions:

```bash
# On remote machine
sudo useradd -m -s /bin/bash qui-transfer
sudo usermod -aG media qui-transfer  # Add to group with file access
```

### Restrict Key Usage (Optional)

Limit what the SSH key can do in `~/.ssh/authorized_keys`:

```
command="/usr/bin/rrsync -ro /path/to/torrents",no-agent-forwarding,no-port-forwarding,no-pty,no-X11-forwarding ssh-ed25519 AAAA... qui-key
```

This restricts the key to rsync operations in a specific directory.

## Path Mappings for Remote Instances

Remote instances typically need [path mappings](/docs/features/path-mappings) since the remote filesystem layout differs from qui's local view.

**Example:**

| Instance Path (remote) | Canonical Path (qui's view) |
|------------------------|----------------------------|
| `/home/user/downloads` | `/mnt/seedbox/downloads` |

If you mount the remote filesystem locally (SSHFS, NFS, SMB), the canonical path should reflect where it's mounted on the qui machine.

## Troubleshooting

### Connection refused

- Verify SSH server is running: `systemctl status sshd`
- Check firewall allows port 22 (or custom port)
- Verify host and port in qui settings

### Permission denied (publickey)

- Key path in qui settings is correct and readable
- Public key is in remote `~/.ssh/authorized_keys`
- Key permissions: `chmod 600 ~/.ssh/qui_key`
- Test manually: `ssh -i /path/to/key user@host`

### Host key verification failed

First connection to a host requires accepting its key. Either:
- SSH to the host manually first to accept the key
- Add the host key to `~/.ssh/known_hosts`

### rsync: command not found

Install rsync on both machines:

```bash
# Debian/Ubuntu
sudo apt install rsync

# RHEL/CentOS/Fedora
sudo dnf install rsync

# macOS
brew install rsync
```

### Transfers are slow

- Check network bandwidth between machines
- Verify rsync is using delta transfers (not full copies)
- Consider enabling compression for slow links (future feature)

### Permission denied on files

The SSH user needs read access to source files and write access to target directories:

```bash
# Add user to media group
sudo usermod -aG media qui-transfer

# Ensure group has access
chmod -R g+rX /path/to/torrents
```

### "No executor available" error

This means neither local nor SSH executor can handle the transfer. Check:
- At least one instance has SSH configured, OR
- Both instances have local filesystem access enabled
- SSH connection settings are valid

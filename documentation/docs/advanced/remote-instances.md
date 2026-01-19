---
sidebar_position: 3
title: Remote Instances
description: Connect to qBittorrent instances on remote machines via SSH or FTP.
---

# Remote Instances

:::warning[Work in Progress]
This feature is under active development. The UI and behavior may change.
:::

When qBittorrent runs on a different machine than qui, you can configure SSH or FTP connections to enable file operations like [transfers](/docs/features/transfers). qui connects via SSH to create hardlinks, reflinks, or transfer files using rsync. Alternatively, FTP can be used for simpler file transfer scenarios.

## Connection Types

| Type | Best For | Features |
|------|----------|----------|
| **SSH** | Full functionality | Hardlinks, reflinks, rsync, terminal access |
| **FTP/FTPS** | Simple file transfers | Basic upload/download, TLS encryption |

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

### Host Key Verification (TOFU)

qui implements Trust-On-First-Use (TOFU) host key verification to protect against man-in-the-middle attacks:

1. **First connection**: When you test an SSH connection for the first time, qui captures the server's host key fingerprint
2. **Storage**: The fingerprint is stored in the database along with the connection settings
3. **Subsequent connections**: Every connection verifies the server's key matches the stored fingerprint
4. **Key changes**: If the server's key changes (e.g., after reinstallation), qui will reject the connection and alert you

This applies to all SSH operations including:
- Direct SSH connections
- rsync file transfers
- SCP file copies
- SFTP operations

:::tip
If a server's host key legitimately changes, you can accept the new key through the connection settings UI.
:::

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

This can occur in two scenarios:

**First connection:**
- Use the **Test Connection** button in the UI to capture and store the host key
- The fingerprint will be displayed for verification before accepting

**Host key mismatch (key changed):**
- The server's key has changed from what qui has stored
- This could indicate a legitimate server change (reinstall, new hardware) or a security issue
- Verify the change is expected before accepting the new key
- Use the **Accept New Key** option in the connection settings if the change is legitimate

:::warning
Never accept a new host key without verification. An unexpected key change could indicate a man-in-the-middle attack.
:::

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

---

## FTP Connections

For scenarios where SSH is not available, qui supports FTP and FTPS connections for file transfers.

### FTP Connection Types

| Type | Description | Default Port |
|------|-------------|--------------|
| `ftp_explicit` | FTP with explicit TLS (STARTTLS) | 21 |
| `ftp_implicit` | FTP with implicit TLS | 990 |
| `ftp_plain` | Plain FTP (not recommended) | 21 |

:::warning
Plain FTP transmits credentials and data unencrypted. Only use for trusted local networks.
:::

### FTP Configuration

| Setting | Description | Example |
|---------|-------------|---------|
| **Host** | FTP server hostname or IP | `ftp.example.com` |
| **Port** | FTP port | `21` or `990` |
| **Username** | FTP login user | `media` |
| **Password** | FTP password | Stored securely |
| **TLS Skip Verify** | Skip TLS certificate verification | For self-signed certs |

### FTP Quick Start

1. Go to **Instances** and click the gear icon on an instance
2. Open the **Connection** tab
3. Select connection type: `FTP (Explicit TLS)`, `FTP (Implicit TLS)`, or `FTP (Plain)`
4. Fill in host, port, username, and password
5. Click **Test Connection** to verify
6. Click **Save**

### FTP Limitations

Compared to SSH, FTP connections have limitations:

| Feature | SSH | FTP |
|---------|-----|-----|
| Hardlinks | Yes | No |
| Reflinks | Yes | No |
| rsync delta transfers | Yes | No |
| Terminal access | Yes | No |
| Relay transfers (remote-to-remote) | Yes | Yes |

FTP connections are best for simple scenarios where you only need to upload/download files and don't require advanced linking capabilities.

### FTP Troubleshooting

#### Connection refused

- Verify FTP server is running
- Check firewall allows the FTP port
- For explicit TLS, ensure STARTTLS is supported

#### TLS handshake failed

- Enable **TLS Skip Verify** for self-signed certificates
- Verify the FTP server's TLS configuration
- Try explicit vs implicit TLS mode

#### Passive mode issues

- Ensure passive port range is open on firewall
- Some NAT configurations may require passive mode adjustments on the server

---
sidebar_position: 1
title: Metrics
---

# Prometheus Metrics

Prometheus metrics can be enabled to monitor your qBittorrent instances. When enabled, metrics are served on a **separate port** (default: 9074) with **no authentication required** for easier monitoring setup.

## Enable Metrics

Metrics are **disabled by default**. Enable them via configuration file or environment variable:

### Config File (`config.toml`)

```toml
metricsEnabled = true
metricsHost = "127.0.0.1"  # Bind to localhost only (recommended for security)
metricsPort = 9074         # Standard Prometheus port range
# metricsBasicAuthUsers = "user:$2y$10$bcrypt_hash_here"  # Optional: basic auth
```

### Environment Variables

```bash
QUI__METRICS_ENABLED=true
QUI__METRICS_HOST=0.0.0.0    # Optional: bind to all interfaces if needed
QUI__METRICS_PORT=9074       # Optional: custom port
QUI__METRICS_BASIC_AUTH_USERS="user:$2y$10$hash"  # Optional: basic auth
```

## Available Metrics

### Torrent Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `qbittorrent_torrents_downloading` | Gauge | `instance_id`, `instance_name` | Number of downloading torrents |
| `qbittorrent_torrents_seeding` | Gauge | `instance_id`, `instance_name` | Number of seeding torrents |
| `qbittorrent_torrents_paused` | Gauge | `instance_id`, `instance_name` | Number of paused torrents |
| `qbittorrent_torrents_error` | Gauge | `instance_id`, `instance_name` | Number of torrents in error state |
| `qbittorrent_torrents_checking` | Gauge | `instance_id`, `instance_name` | Number of torrents being checked |

### Transfer Statistics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `qbittorrent_session_download_bytes` | Counter | `instance_id`, `instance_name` | Total downloaded data this session (bytes) |
| `qbittorrent_session_upload_bytes` | Counter | `instance_id`, `instance_name` | Total uploaded data this session (bytes) |
| `qbittorrent_alltime_download_bytes` | Counter | `instance_id`, `instance_name` | Total all-time downloaded data (bytes) |
| `qbittorrent_alltime_upload_bytes` | Counter | `instance_id`, `instance_name` | Total all-time uploaded data (bytes) |

### Instance Status

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `qbittorrent_instance_connection_status` | Gauge | `instance_id`, `instance_name` | Connection status (1=connected, 0=disconnected) |
| `qbittorrent_scrape_errors_total` | Counter | `instance_id`, `instance_name`, `type` | Total scrape errors by type |

### Internal Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `qui_db_wedged_transaction_total` | Counter | Wedged transaction detections (indicates a bug) |

### Standard Go Metrics

qui also exposes standard Go runtime metrics via the `go_*` and `process_*` prefixes (memory, goroutines, GC, etc.).

## Prometheus Configuration

Configure Prometheus to scrape the dedicated metrics port (no authentication required):

```yaml
scrape_configs:
  - job_name: 'qui'
    static_configs:
      - targets: ['localhost:9074']
    metrics_path: /metrics
    scrape_interval: 30s
    #basic_auth:
      #username: prometheus
      #password: yourpassword
```

All metrics are labeled with `instance_id` and `instance_name` for multi-instance monitoring.

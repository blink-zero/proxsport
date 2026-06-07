# proxsport

A Prometheus exporter for Proxmox VE. Pulls cluster, node, guest (VM + LXC), and storage metrics from the Proxmox API and exposes them on `/metrics` in Prometheus exposition format.

Pair it with Prometheus + Grafana to get familiar dashboards of CPU / memory / disk / network for every guest and host across your Proxmox cluster, without leaning on the built-in InfluxDB integration.

## Why another Proxmox exporter

- **One binary, no dependencies.** Static Go binary or single Docker image.
- **Polls once per interval, serves many scrapes.** Heavy Prometheus scrape pressure does not turn into per-scrape pressure on Proxmox.
- **API tokens first-class.** Designed around scope-limited read-only tokens (PVEAuditor), not root credentials.
- **Both VMs and LXC.** Proxmox treats both as guests; so does proxsport.
- **Auto-discovery.** No static VM ID lists — `/cluster/resources` is the source of truth.

## Quick start

### Docker

```yaml
services:
  proxsport:
    image: ghcr.io/blink-zero/proxsport:latest
    restart: unless-stopped
    ports:
      - "9221:9221"
    environment:
      PROXMOX_URL: "https://pve.example.com:8006"
      PROXMOX_TOKEN_ID: "monitoring@pve!proxsport"
      PROXMOX_TOKEN_SECRET: "00000000-0000-0000-0000-000000000000"
      PROXMOX_INSECURE_SKIP_VERIFY: "true"
```

Then scrape it from Prometheus:

```yaml
scrape_configs:
  - job_name: "proxsport"
    static_configs:
      - targets: ["proxsport:9221"]
```

### Binary

```bash
PROXMOX_URL="https://pve.example.com:8006" \
PROXMOX_TOKEN_ID="monitoring@pve!proxsport" \
PROXMOX_TOKEN_SECRET="..." \
./proxsport
```

A `systemd` unit example lives in [`examples/proxsport.service`](./examples/proxsport.service), and a Prometheus scrape snippet in [`examples/prometheus-scrape.yml`](./examples/prometheus-scrape.yml).

## Configuration

All configuration is via environment variables. No config file.

| Variable                       | Default     | Description                                                                 |
| ------------------------------ | ----------- | --------------------------------------------------------------------------- |
| `PROXMOX_URL`                  | *(required)*  | Base URL of the Proxmox API, e.g. `https://pve.example.com:8006`            |
| `PROXMOX_TOKEN_ID`             |             | API token ID, e.g. `monitoring@pve!proxsport`. Preferred over password.     |
| `PROXMOX_TOKEN_SECRET`         |             | Token secret (UUID).                                                        |
| `PROXMOX_USERNAME`             |             | Fallback PAM/PVE username, e.g. `monitoring@pam`.                           |
| `PROXMOX_PASSWORD`             |             | Fallback password.                                                          |
| `PROXMOX_INSECURE_SKIP_VERIFY` | `false`     | Skip TLS verification (useful for self-signed Proxmox certs).               |
| `PROXSPORT_LISTEN`             | `:9221`     | Address proxsport binds to.                                                 |
| `PROXSPORT_METRICS_PATH`       | `/metrics`  | Path serving Prometheus metrics.                                            |
| `PROXSPORT_POLL_INTERVAL`      | `30s`       | How often proxsport polls Proxmox. Independent of Prometheus scrape rate.   |
| `PROXSPORT_REQUEST_TIMEOUT`    | `15s`       | HTTP timeout for a single Proxmox API request.                              |
| `PROXSPORT_COLLECT_NODES`      | `true`      | Toggle node metrics.                                                        |
| `PROXSPORT_COLLECT_GUESTS`     | `true`      | Toggle guest (VM + LXC) metrics.                                            |
| `PROXSPORT_COLLECT_STORAGE`    | `true`      | Toggle storage pool metrics.                                                |
| `PROXSPORT_COLLECT_CLUSTER`    | `true`      | Toggle cluster-level metrics.                                               |

## Permissions

Create a dedicated monitoring user with the minimum privilege required:

1. *Datacenter → Permissions → Users* → Add user `monitoring@pve` (or `monitoring@pam`).
2. *Datacenter → Permissions → Add* → Path `/`, User `monitoring@pve`, Role `PVEAuditor`.
3. *Datacenter → Permissions → API Tokens* → Add token. Leave **Privilege Separation** off, *or* grant `PVEAuditor` on the token explicitly.
4. Copy the token ID (`user@realm!tokenname`) and secret into the env vars above.

`PVEAuditor` is a built-in read-only role. proxsport never writes to the API.

## Metrics

All metric names are prefixed with `proxsport_`.

### Health

| Metric                              | Type    | Description                                                        |
| ----------------------------------- | ------- | ------------------------------------------------------------------ |
| `proxsport_up`                      | gauge   | `1` if the last poll succeeded, `0` otherwise.                     |
| `proxsport_scrape_duration_seconds` | gauge   | Time since the last successful poll, in seconds.                    |
| `proxsport_scrape_errors_total`     | counter | Total failed poll cycles since proxsport started.                  |

### Cluster (labels: `cluster`)

| Metric                          | Type  | Description                                |
| ------------------------------- | ----- | ------------------------------------------ |
| `proxsport_cluster_quorate`     | gauge | `1` if the cluster is quorate.             |
| `proxsport_cluster_nodes_total` | gauge | Number of nodes in the cluster.            |

### Node (labels: `node`)

| Metric                                | Type  | Description                              |
| ------------------------------------- | ----- | ---------------------------------------- |
| `proxsport_node_up`                   | gauge | `1` if the node is online.               |
| `proxsport_node_cpu_usage_ratio`      | gauge | Current CPU usage ratio (0..1).          |
| `proxsport_node_cpu_cores`            | gauge | Reported CPU cores.                      |
| `proxsport_node_memory_used_bytes`    | gauge | Memory in use.                           |
| `proxsport_node_memory_total_bytes`   | gauge | Total memory installed.                  |
| `proxsport_node_rootdisk_used_bytes`  | gauge | Root-disk space in use.                  |
| `proxsport_node_rootdisk_total_bytes` | gauge | Root-disk total capacity.                |
| `proxsport_node_uptime_seconds`       | gauge | Uptime in seconds.                       |

### Guest (labels: `vmid`, `name`, `node`, `type` = `qemu`/`lxc`, `tags`)

| Metric                                          | Type    | Description                                      |
| ----------------------------------------------- | ------- | ------------------------------------------------ |
| `proxsport_guest_status`                        | gauge   | `1` if running.                                  |
| `proxsport_guest_cpu_usage_ratio`               | gauge   | Current CPU usage (0..1 of allocated).           |
| `proxsport_guest_cpu_cores`                     | gauge   | Allocated vCPUs.                                 |
| `proxsport_guest_memory_used_bytes`             | gauge   | Memory in use (meaningful while running).        |
| `proxsport_guest_memory_total_bytes`            | gauge   | Memory allocated.                                |
| `proxsport_guest_disk_used_bytes`               | gauge   | Disk used (LXC fs-aware; QEMU agent-dependent).  |
| `proxsport_guest_disk_total_bytes`              | gauge   | Disk allocated.                                  |
| `proxsport_guest_uptime_seconds`                | gauge   | Uptime in seconds (`0` if stopped).              |
| `proxsport_guest_network_receive_bytes_total`   | counter | RX bytes since boot.                             |
| `proxsport_guest_network_transmit_bytes_total`  | counter | TX bytes since boot.                             |
| `proxsport_guest_disk_read_bytes_total`         | counter | Disk bytes read since boot.                      |
| `proxsport_guest_disk_write_bytes_total`        | counter | Disk bytes written since boot.                   |

Templates are intentionally excluded — they have no runtime metrics.

### Storage (labels: `node`, `storage`, `plugin`)

| Metric                         | Type  | Description                                |
| ------------------------------ | ----- | ------------------------------------------ |
| `proxsport_storage_used_bytes` | gauge | Used capacity.                             |
| `proxsport_storage_total_bytes`| gauge | Total capacity.                            |
| `proxsport_storage_active`     | gauge | `1` if the storage pool is available.      |

## Grafana dashboard

A drop-in dashboard ships in [`grafana/dashboard.json`](./grafana/dashboard.json) — cluster overview, node, guest, and storage rows, with template variables for datasource, node filter, and guest type. Import via *Dashboards → Import* and pick your Prometheus datasource. See [`grafana/README.md`](./grafana/README.md).

## Roadmap

- Per-VM disk-device metrics (each virtual disk separately).
- Per-guest network-interface breakdown (each NIC separately).
- Backup job / replication status metrics.
- `arm64` Docker images alongside `amd64`.

## License

MIT. See `LICENSE`.

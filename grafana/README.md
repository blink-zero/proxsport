# Grafana dashboard

`dashboard.json` is a drop-in Grafana dashboard for proxsport.

## Importing

1. In Grafana, **Dashboards → Import**.
2. Click **Upload JSON file** and pick `dashboard.json`, or paste the contents into the textarea.
3. Pick your Prometheus datasource when prompted.
4. **Import**.

## Layout

Four rows of panels:

- **Cluster** — overall up/quorum status, node and guest counts, exporter health.
- **Nodes** — per-node CPU / memory / disk usage timeseries, plus an uptime table.
- **Guests** — top 10 by CPU, memory, network RX/TX, disk read/write, plus a full inventory table.
- **Storage** — per-pool usage gauge and capacity table.

## Variables

The dashboard ships with three template variables:

- `DS_PROMETHEUS` — picks the Prometheus datasource.
- `node` — multi-select filter by Proxmox node (defaults to All).
- `type` — multi-select filter by guest type (`qemu`/`lxc`, defaults to All).

## Tested against

- Grafana 10.x
- Prometheus 2.x (via standard scrape config — see `../examples/prometheus-scrape.yml`)

If a panel renders blank, double-check that the `DS_PROMETHEUS` variable
resolved to your actual Prometheus datasource and that proxsport's
`/metrics` endpoint is being scraped.

# Changelog

All notable changes are recorded here. Format loosely follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

## [0.1.0] — 2026-06-07

Initial release.

### Added
- Prometheus exporter binary (`proxsport`) for Proxmox VE
- Cluster, node, guest (QEMU + LXC), and storage metric collectors
- Read-only Proxmox API client supporting API tokens and PAM password auth
- Background polling so Prometheus scrape pressure does not amplify Proxmox API load
- Docker image published to GHCR (multi-stage build, non-root user)
- Grafana dashboard (`grafana/dashboard.json`) with cluster overview, node, guest, and storage rows
- systemd unit example (`examples/proxsport.service`) with hardening directives for direct install on a Proxmox host or sidecar VM
- Prometheus scrape config example (`examples/prometheus-scrape.yml`)
- GitHub Actions CI (Go vet + build + test) and Docker publish workflow with semver / latest / dev / sha tagging

[Unreleased]: https://github.com/blink-zero/proxsport/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/blink-zero/proxsport/releases/tag/v0.1.0

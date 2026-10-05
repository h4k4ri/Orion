# Changelog

All notable changes to Orion are recorded here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project adopts [Semantic Versioning](https://semver.org/) starting from `v1.0.0`.

## [Unreleased]

### Added
- Skeleton of 9 Go services in the control plane: `orion-api`, `orion-identity`, `orion-compute`, `orion-network`, `orion-image`, `orion-volume`, `orion-placement`, `orion-baremetal`, `orion-operation`.
- Three Rust host agents: `orion-node-agent`, `orion-network-host-agent`, `orion-volume-host-agent`.
- Shared domain core in `core/` (adapters, domain, scheduler, reconciliation, runtime).
- Identity connectors: `azure`, `keycloak`, `ldap`.
- Per-domain plugins (kvm, network, ceph, cephfs, glusterfs, nfs, smb, zfs, iscsi, nvmeof, s3, image, dns, loadbalancer, messaging, telemetry, orchestration, baremetal, backup, dbaas, containers, openbao, openbao-keys).
- Official SDKs in Go, Python, and Rust.
- Architecture documentation, OpenStack mapping, execution plan, and plugin system in `docs/`.

### Changed
- README rewritten to reflect Orion as a simpler OpenStack substitute, with corrected relative links.

## [0.1.0] - 2026-04-01

### Added
- Initial monorepo bootstrap with Go and Rust workspaces.
- First cut of `orion-identity` with local `username + password` authentication.
- Initial Postgres schema in `migrations/`.
- Unified `orion` CLI in `cli/orion-cli`.
- `docker-compose.dev.yml` with Postgres and NATS for local development.
# Orion

Open source IaaS platform inspired by OpenStack, with a smaller, opinionated scope.

## Purpose

Orion exists to deliver the operational concepts that make OpenStack useful — identity, public API, compute, networking, image, and volume — without inheriting the historical complexity accumulated over more than a decade of overlapping projects.

The goal is not to reproduce OpenStack or to compete with it on surface area. It is to ship a smaller, coherent, operable core for anyone who needs a federated IaaS and does not want to assemble a dozen interdependent services just to provision a VM.

### What Orion reuses from OpenStack

- a clean separation between control plane, executor, and host;
- an identity model with `project` and `system` scopes;
- a scheduler driven by traits and inventory;
- asynchronous tasks for long-running operations;
- strong host agents that apply real state.

### What Orion deliberately simplifies

- no competing hypervisors in the first cut (only `KVM/libvirt`);
- no live migration in the MVP;
- no external identity federation out of the box (the interface exists and connectors are added on demand);
- no trust chains, MFA, or distributed token provider;
- no legacy Nova service topology;
- no hundreds of network and volume drivers — only what the target scenario needs.

The full mapping between OpenStack services and the Orion model is in [`docs/openstack-service-model.md`](docs/openstack-service-model.md).

## Architecture

Orion is organized in three layers:

1. **Global plane** — `orion-identity`, `orion-api`, `orion-compute`, `orion-network`, `orion-image`, `orion-volume`, `orion-placement`, `orion-baremetal`, and `orion-operation`, all in Go. They run the public API, policies, catalog, and aggregated placement.
2. **Cells** — local instances that handle detailed placement and operational isolation, coordinated through `NATS`.
3. **Hosts** — Rust agents (`orion-node-agent`, `orion-network-host-agent`, `orion-volume-host-agent`) that apply real state over `KVM/libvirt`, `OVS/OVN`, and `LVM`.

The communication fabric between layers uses `NATS` as the default broker for tasks, events, and reconciliation.

## Technical direction

- Control plane in Go 1.27+.
- Host agents in Rust 2021 edition.
- `PostgreSQL` as the primary database.
- `NATS` as the default broker for tasks, events, and reconciliation.
- `OpenTelemetry` + Prometheus for control plane observability.
- `OVS/OVN` as the network backend.
- `LVM` as the default volume backend in the first cut.
- Unified `orion` CLI for day-to-day operations.
- Official `.deb` packaging for control plane and host agents.
- Pluggable identity connectors (`azure`, `keycloak`, `ldap`).
- Per-domain plugins (compute, network, storage, etc.) compiled as separate binaries when needed.

## Execution plan

The detailed plan with phases `MVP -> v0.1 -> v0.2 -> v1`, minimum contracts, and recommended implementation order is in [`docs/execution-plan.md`](docs/execution-plan.md).

The per-release implementation roadmap is in [`docs/IMPLEMENTATION_ROADMAP_v11.1.md`](docs/IMPLEMENTATION_ROADMAP_v11.1.md).

The plugin model and the criteria for using real backends are in [`docs/plugins-real-backends.md`](docs/plugins-real-backends.md) and [`docs/plugin-system-architecture.md`](docs/plugin-system-architecture.md).

An operational guide for installing Orion on two VMs, separating `control plane` and `edge host`, is in [`docs/two-vm-install.md`](docs/two-vm-install.md).

## Development bootstrap

Local dependencies:

- `Go 1.27+`
- `Rust 1.80+` (stable toolchain)
- `Docker` and `Docker Compose`
- `libvirt`, `ovs`, `ovn`, `lvm2` on the execution host
- `nats-server`, `postgres` (started via `make dev-up`)

Initial commands:

```bash
make dev-up     # start postgres, nats, and local dependencies
make check      # build + lint + tests
make build      # compile control plane and host agents
```

## Repository layout

Monorepo with explicit boundaries between Go and Rust:

```text
services/       control plane in Go (api, identity, compute, network, image,
                volume, placement, baremetal, operation)
agents/         host agents in Rust (node, network-host, volume-host)
core/           shared domain core in Go (adapters, domain, scheduler,
                reconciliation, runtime)
connectors/     external identity connectors (azure, keycloak, ldap)
plugins/        per-domain plugins (kvm, network, ceph, nfs, ...)
contracts/      shared gRPC contracts and protos
proto/          `.proto` files for internal and public APIs
gen/            Go code generated from protos
sdk/            official SDKs in Go, Python, and Rust
libs/           shared libraries per language
cli/            unified `orion` CLI (binary `orion-cli`)
migrations/     Postgres migrations
deploy/         manifests, scripts, and development environment
deployments/    examples of real-environment deployments
packaging/      `.deb` packaging recipes (control plane and host agents)
docs/           architecture, execution, OpenStack mapping, plugins
docs-site/      documentation site (Starlight/Astro)
tests/          conformance tests and end-to-end scenarios
docker-compose.dev.yml  local dependency stack (postgres, nats)
```

Each area has a short `README.md` so its purpose is visible from the repository layout itself.

## Status

The project is in early construction. Not all services, agents, and plugins are complete. The `services/`, `agents/`, `plugins/`, and `connectors/` trees represent the target skeleton; what is actually ready is described in [`docs/execution-plan.md`](docs/execution-plan.md).

## License

Apache-2.0.
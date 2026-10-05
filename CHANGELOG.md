# Changelog

Todas as mudancas relevantes do Orion sao registradas aqui. O formato segue [Keep a Changelog](https://keepachangelog.com/pt-BR/1.1.0/) e o projeto adota [Versionamento Semantico](https://semver.org/lang/pt-BR/) a partir da `v1.0.0`.

## [Unreleased]

### Added
- Esqueleto de 9 servicos Go no control plane: `orion-api`, `orion-identity`, `orion-compute`, `orion-network`, `orion-image`, `orion-volume`, `orion-placement`, `orion-baremetal`, `orion-operation`.
- Tres agentes Rust: `orion-node-agent`, `orion-network-host-agent`, `orion-volume-host-agent`.
- Nucleo compartilhado em `core/` (adapters, domain, scheduler, reconciliation, runtime).
- Conectores de identidade: `azure`, `keycloak`, `ldap`.
- Plugins por dominio (kvm, network, ceph, cephfs, glusterfs, nfs, smb, zfs, iscsi, nvmeof, s3, image, dns, loadbalancer, messaging, telemetry, orchestration, baremetal, backup, dbaas, containers, openbao, openbao-keys).
- SDKs oficiais em Go, Python e Rust.
- Documentacao de arquitetura, mapeamento OpenStack, plano de execucao e sistema de plugins em `docs/`.

### Changed
- README reescrito para refletir o Orion como substituto mais simples do OpenStack, com links relativos corrigidos.

## [0.1.0] - 2026-04-01

### Added
- Bootstrap inicial do monorepo com workspaces Go e Rust.
- Primeiro corte do `orion-identity` com autenticacao local `username + password`.
- Schema inicial do Postgres em `migrations/`.
- CLI unificada `orion` em `cli/orion-cli`.
- `docker-compose.dev.yml` com Postgres e NATS para desenvolvimento local.
# Orion

Orion e uma plataforma IaaS open source desenhada como uma cloud federada, com plano de controle global, celulas de execucao e agentes fortes nos hosts.

O objetivo do projeto nao e reproduzir a complexidade historica do OpenStack, e sim entregar um nucleo coerente e opinativo para operacao de compute, rede, imagens e volumes em uma arquitetura hierarquica:

- Plano global para identidade, API publica, politicas, catalogo e placement agregado.
- Celulas para execucao local, placement detalhado e isolamento operacional.
- Hosts com agentes em Rust para aplicar estado real sobre `KVM/libvirt`, `OVS/OVN` e `LVM`.

## Direcao tecnica

- Control plane em Go.
- Host agents em Rust.
- `PostgreSQL` como banco principal.
- `NATS` como broker padrao para tasks, eventos e reconciliacao.
- `OpenTelemetry` + Prometheus para observabilidade do control plane.
- `OVS/OVN` como backend de rede.
- `LVM` como unico backend inicial de volume.
- Empacotamento oficial em `.deb`.
- Operacao por CLI unificada `orion`.

## Plano de execucao

O plano inicial de execucao, com fases `MVP -> v0.1 -> v0.2 -> v1`, contratos minimos e ordem recomendada de implementacao, esta em [docs/execution-plan.md](/home/horizon/orion/docs/execution-plan.md).

O mapeamento entre os conceitos do OpenStack e as simplificacoes deliberadas do Orion esta em [docs/openstack-service-model.md](/home/horizon/orion/docs/openstack-service-model.md).

Um guia operacional para instalar o Orion em duas VMs, separando `control plane` e `edge host`, esta em [docs/two-vm-install.md](/home/horizon/orion/docs/two-vm-install.md).

## Bootstrap de desenvolvimento

Dependencias locais:

- `Go 1.24+`
- `Rust 1.93+`
- `Docker`

Comandos iniciais:

```bash
make dev-up
make check
make build
```

## Layout inicial

O repositorio nasce como monorepo com fronteiras explicitas:

```text
services/   control plane em Go
agents/     agentes de host em Rust
cli/        CLI unificada
libs/       bibliotecas compartilhadas por linguagem
packaging/  empacotamento oficial
docs/       documentos de arquitetura e execucao
docs-site/  site de documentacao em Starlight
deploy/     manifests, exemplos e automacao de ambiente
```

Cada area possui um `README.md` curto para manter o proposito visivel no proprio layout do repositorio.

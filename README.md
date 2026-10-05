# Orion

Plataforma IaaS open source inspired em OpenStack, mas com escopo menor e opinativo.

## Proposito

O Orion nasce para oferecer os conceitos operacionais que tornam o OpenStack util — identidade, API publica, compute, rede, imagem e volume — sem herdar a complexidade historica acumulada de mais de uma decada de projetos sobrepostos.

A ideia nao e reproduzir o OpenStack nem competir com ele em superficie. E entregar um nucleo menor, coerente e operavel para quem precisa de uma IaaS federada e nao quer montar 12 servicos interdependentes para provisionar uma VM.

### O que o Orion reaproveita do OpenStack

- separacao clara entre plano de controle, executor e host;
- modelo de identidade com escopos `project` e `system`;
- scheduler com traits e inventario;
- tasks assincronas para operacoes longas;
- agentes fortes nos hosts aplicando estado real.

### O que o Orion simplifica deliberadamente

- sem hypervisors concorrentes no primeiro corte (so `KVM/libvirt`);
- sem live migration no MVP;
- sem federacao de identidade externa (a interface existe e conectores sao adicionados sob demanda);
- sem trust chains, MFA nem token provider distribuido;
- sem herdar a topologia historica de servicos do Nova;
- sem centenas de drivers de rede e volume — apenas os necessarios para o cenario alvo.

O mapeamento completo entre servicos OpenStack e o modelo Orion esta em [`docs/openstack-service-model.md`](docs/openstack-service-model.md).

## Arquitetura

O Orion e organizado em tres camadas:

1. **Plano global** — `orion-identity`, `orion-api`, `orion-compute`, `orion-network`, `orion-image`, `orion-volume`, `orion-placement`, `orion-baremetal` e `orion-operation` em Go. Operam API publica, politicas, catalogo e placement agregado.
2. **Celulas** — instanciacoes locais que cuidam do placement detalhado e isolamento operacional, coordenadas por `NATS`.
3. **Hosts** — agentes em Rust (`orion-node-agent`, `orion-network-host-agent`, `orion-volume-host-agent`) que aplicam estado real sobre `KVM/libvirt`, `OVS/OVN` e `LVM`.

A malha de comunicacao entre as camadas usa `NATS` como broker padrao de tasks, eventos e reconciliacao.

## Direcao tecnica

- Control plane em Go 1.27+.
- Host agents em Rust 2021 edition.
- `PostgreSQL` como banco principal.
- `NATS` como broker de tasks, eventos e reconciliacao.
- `OpenTelemetry` + Prometheus para observabilidade do control plane.
- `OVS/OVN` como backend de rede.
- `LVM` como backend padrao de volume no primeiro corte.
- CLI unificada `orion` para operacao do dia a dia.
- Empacotamento oficial em `.deb` para control plane e host agents.
- Conectores de identidade plugaveis (`azure`, `keycloak`, `ldap`).
- Plugins por dominio (compute, network, storage, etc.) compilados como binarios separados quando necessario.

## Plano de execucao

O plano detalhado com fases `MVP -> v0.1 -> v0.2 -> v1`, contratos minimos e ordem recomendada de implementacao esta em [`docs/execution-plan.md`](docs/execution-plan.md).

O roadmap de implementacao por release esta em [`docs/IMPLEMENTATION_ROADMAP_v11.1.md`](docs/IMPLEMENTATION_ROADMAP_v11.1.md).

O modelo de plugins e o criterio para usar backends reais esta em [`docs/plugins-real-backends.md`](docs/plugins-real-backends.md) e [`docs/plugin-system-architecture.md`](docs/plugin-system-architecture.md).

Um guia operacional para instalar o Orion em duas VMs, separando `control plane` e `edge host`, esta em [`docs/two-vm-install.md`](docs/two-vm-install.md).

## Bootstrap de desenvolvimento

Dependencias locais:

- `Go 1.27+`
- `Rust 1.80+` (toolchain estavel)
- `Docker` e `Docker Compose`
- `libvirt`, `ovs`, `ovn`, `lvm2` no host de execucao
- `nats-server`, `postgres` (subidos via `make dev-up`)

Comandos iniciais:

```bash
make dev-up     # sobe postgres, nats e dependencias locais
make check      # build + lint + testes
make build      # compila control plane e host agents
```

## Layout do repositorio

Monorepo com fronteiras explicitas entre Go e Rust:

```text
services/       control plane em Go (api, identity, compute, network, image,
                volume, placement, baremetal, operation)
agents/         agentes de host em Rust (node, network-host, volume-host)
core/           nucleo de dominio compartilhado em Go (adapters, domain,
                scheduler, reconciliation, runtime)
connectors/     conectores externos de identidade (azure, keycloak, ldap)
plugins/        plugins por dominio (kvm, network, ceph, nfs, ...)
contracts/      contratos gRPC e protos compartilhados
proto/          arquivos `.proto` das APIs internas e publicas
gen/            codigo Go gerado a partir dos protos
sdk/            SDKs oficiais em Go, Python e Rust
libs/           bibliotecas compartilhadas por linguagem
cli/            CLI unificada `orion` (binario `orion-cli`)
migrations/     migrations de Postgres
deploy/         manifests, scripts e ambiente de desenvolvimento
deployments/    exemplos de implantacao em ambientes reais
packaging/      receita de empacotamento `.deb` (control plane e host agents)
docs/           arquitetura, execucao, mapeamento OpenStack, plugins
docs-site/      site de documentacao em Starlight/Astro
tests/          testes de conformidade e cenarios end-to-end
docker-compose.dev.yml  stack de dependencias locais (postgres, nats)
```

Cada area possui um `README.md` curto para manter o proposito visivel no proprio layout do repositorio.

## Status

O projeto esta em fase de construcao inicial. Nem todos os servicos, agentes e plugins estao completos. A arvore `services/`, `agents/`, `plugins/` e `connectors/` representa o esqueleto alvo; o que esta pronto de fato e descrito no [`docs/execution-plan.md`](docs/execution-plan.md).

## Licenca

Apache-2.0.
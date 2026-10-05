---
title: Visão Geral
description: Como ler a estrutura do Orion e por onde começar.
---

O Orion e uma plataforma IaaS open source desenhada como **cloud federada**, nao como um unico cluster monolitico de hypervisores.

## Ideia central

- o plano global cuida de identidade, API, catalogo, politicas e placement agregado;
- as celulas executam compute, rede e volume localmente;
- os hosts aplicam o estado real usando agentes fortes.

## Stack inicial

- control plane em `Go`;
- agentes de host em `Rust`;
- `PostgreSQL` como banco principal;
- `NATS` para tasks e eventos;
- `KVM/libvirt` como backend de virtualizacao;
- `OVS/OVN` como backend de rede;
- `LVM` como backend inicial de block storage.

## Estrutura do monorepo

```text
services/   control plane em Go
agents/     agentes de host em Rust
cli/        CLI unificada
libs/       bibliotecas compartilhadas
docs/       documentacao de engenharia
docs-site/  site publicado em Starlight
packaging/  empacotamento .deb
deploy/     exemplos e automacao
```

## Leitura recomendada

1. [Arquitetura federada](/architecture/federated-cloud/)
2. [Fases de execução](/architecture/execution-phases/)
3. [Modelo de serviços inspirado no OpenStack](/reference/service-model/)

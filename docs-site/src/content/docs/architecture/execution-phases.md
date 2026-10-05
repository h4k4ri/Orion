---
title: Fases de Execução
description: Ordem realista de entrega do Orion.
---

O Orion cresce em fases para evitar repetir a complexidade historica do OpenStack.

## MVP

Objetivo:

- provar o fluxo completo de uma VM em uma unica celula.

Escopo:

- `orion-api`
- `orion-identity`
- `orion-compute`
- `orion-placement` local
- `orion-network`
- `orion-image`
- `orion-volume` com `LVM`
- `orion-node-agent`
- `orion-cli`

## v0.1

Objetivo:

- endurecer a celula unica e consolidar contratos.

Entram:

- reconciliacao;
- health checks;
- drain e disable de hosts;
- observabilidade;
- empacotamento `.deb`.

## v0.2

Objetivo:

- ativar o primeiro recorte real de cloud federada.

Entram:

- multiplas celulas;
- catalogo global;
- placement global agregado;
- scheduler global escolhendo a celula.

## v1

Objetivo:

- baseline operacional consistente para uso real controlado.

Entram:

- RBAC mais completo;
- quotas basicas;
- snapshots e resize onde o modelo permitir;
- fluxo de upgrade e rollback;
- testes de integracao por fluxo principal.

## Regra de escopo

O que fica fora das primeiras fases:

- live migration;
- multi-attach;
- storage compartilhado entre celulas;
- multiplos hypervisors;
- politicas sofisticadas demais antes de provar o nucleo.

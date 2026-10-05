---
title: E2E
description: Validacao ponta a ponta do Orion no lab atual.
---

O Orion agora inclui um executavel de E2E real no proprio monorepo:

- `go run ./cli/orion-cli/cmd/orion-e2e`
- `make e2e`

## O que ele valida

- health checks de `api`, `identity`, `placement`, `image`, `network`, `node-agent`, `volume` e `volume-host-agent`;
- emissao de token no `orion-identity`;
- criacao de `network` e `subnet`;
- boot de duas VMs reais no `libvirt`;
- `Logical_Switch_Port` com `up=true` no `OVN`;
- interface conectada ao `br-int`;
- login no console serial do CirrOS;
- IP correto dentro do guest;
- ping guest-to-guest na mesma rede.

## Volume

O binario suporta tres modos:

- `-volume auto`
- `-volume on`
- `-volume off`

`auto` e o modo mais util para o lab atual. Ele tenta o fluxo de volume, mas faz `skip` se o backend `LVM` estiver indisponivel no host.

## Exemplos

```bash
make e2e
go run ./cli/orion-cli/cmd/orion-e2e -volume auto
go run ./cli/orion-cli/cmd/orion-e2e -volume on -keep-resources
```

## Observacao

Hoje o E2E assume `PostgreSQL` e `NATS` disponiveis para os services que dependem deles. Ele limpa as VMs no fim, mas `network` e `subnet` ainda ficam no backend porque o `orion-network` ainda nao expoe `delete` para esses recursos.

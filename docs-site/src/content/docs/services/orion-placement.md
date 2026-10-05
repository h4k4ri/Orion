---
title: orion-placement
description: Placement local inicial do Orion.
---

`orion-placement` e o servico responsavel por manter o inventario detalhado dos hosts de uma celula e selecionar o host adequado para uma nova carga.

## Inspiracao OpenStack

- `Placement`
- inventario e `traits` usados pelo `Nova`

## O que o Orion reaproveita

- inventario por host;
- `traits`;
- separacao entre capacidade agregada e escolha final de host;
- update otimista por `generation`.

## O que o Orion simplifica

- uma API pequena;
- sem microversions;
- sem superficie completa de `resource providers`;
- sem hierarquia complexa de providers.

## Estado atual

O primeiro corte implementa:

- listagem de hosts;
- registro de host;
- selecao de host por `vCPU`, `RAM`, `disk` e `traits`;
- drain e disable de host;
- persistencia em `PostgreSQL`.

No bootstrap do servico existe um host local padrao:

- `host_local`
- `cell_local`
- grupo `general`
- traits `general` e `kvm`

## Endpoints iniciais

### `GET /v1/hosts`

Lista o inventario conhecido.

### `POST /v1/hosts`

Registra ou atualiza um host.

### `POST /v1/selections/hosts`

Recebe uma requisicao de alocacao simplificada e retorna o host escolhido.

### `GET /v1/hosts/:id`

Retorna um host especifico.

### `POST /v1/hosts/:id/enable`

Reabilita o host para agendamento.

### `POST /v1/hosts/:id/disable`

Remove o host do agendamento.

### `POST /v1/hosts/:id/drain`

Mantem o host registrado, mas impede novas alocacoes.

### `POST /v1/hosts/:id/undrain`

Remove o estado de drain.

## Integracao atual

O `orion-compute` usa o `orion-placement` para transformar `flavor` em recursos concretos e escolher o host antes de acionar o `orion-node-agent`.

O inventario agora sobrevive a restart do servico porque o estado fica em `PostgreSQL`.

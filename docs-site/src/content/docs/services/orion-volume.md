---
title: orion-volume
description: Servico de block storage do Orion, inspirado no Cinder com backend LVM.
---

`orion-volume` e o servico de block storage do Orion. Ele concentra o ciclo de vida administrativo do volume e delega a operacao real do host para o `orion-volume-host-agent`.

## Inspiracao OpenStack

- `Cinder`
- separacao entre API/controle e backend de host

## O que o Orion reaproveita

- create, delete, attach e detach como operacoes explicitas;
- servico dedicado para block storage;
- interface de backend, mesmo com um unico driver no inicio.

## O que o Orion simplifica

- um unico backend real: `LVM`;
- volumes locais a celula e ao host;
- sem multi-attach;
- sem storage compartilhado;
- sem scheduler proprio de volume neste primeiro corte.

## Endpoints atuais

- `GET /healthz`
- `GET /v1/volumes`
- `POST /v1/volumes`
- `GET /v1/volumes/:id`
- `DELETE /v1/volumes/:id`
- `POST /v1/volumes/:id/attach`
- `POST /v1/volumes/:id/detach`

## Fluxo atual

1. o `orion-api` recebe a operacao autenticada;
2. o `orion-volume` cria ou consulta o volume no contexto da celula;
3. o `orion-volume-host-agent` executa `lvcreate` ou `lvremove` reais;
4. o `orion-compute` pede attach ou detach quando a operacao envolve uma VM;
5. o `orion-node-agent` aplica `attach-disk` ou `detach-disk` no `libvirt`.

## Infra obrigatoria

- `PostgreSQL` persiste o estado administrativo dos volumes;
- `NATS` publica eventos de create, delete, attach e detach;
- `ORION_DB_DSN` e `ORION_NATS_URL` sao obrigatorios para o service subir.
- um loop periodico de reconciliacao compara o estado persistido com o estado observado no host.
- `GET /metrics` expõe metricas Prometheus e spans HTTP via `OpenTelemetry`.

## Execucao real no host

- os volumes reais sao `Logical Volumes` do `LVM`;
- o path de dados fica em `/dev/<vg>/orion-<volume-id>`;
- o diretorio local do agente e usado so para marcadores e estado auxiliar;
- o backend exige `ORION_VOLUME_LVM_VG` configurado.

## Limitacoes atuais

- o host agent precisa de privilegio real sobre `LVM`;
- se o processo rodar sem acesso a `lvcreate` ou aos locks do `LVM`, a API devolve `volume_backend_unavailable`;
- ainda sem snapshot, resize e migration baseada em storage compartilhado.

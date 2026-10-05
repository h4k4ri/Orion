---
title: orion-volume-host-agent
description: Agente de host que executa LVM real para volumes locais do Orion.
---

`orion-volume-host-agent` roda no host e executa as operacoes reais de block storage para o Orion.

## Papel no desenho

- recebe pedidos do `orion-volume`;
- cria e remove `Logical Volumes` reais;
- entrega `device_path` para o control plane;
- prepara a base para attach e detach no `orion-node-agent`.

## Backend atual

- `LVM`
- `lvcreate`
- `lvremove`

## Endpoints atuais

- `GET /healthz`
- `POST /v1/volumes`
- `GET /v1/volumes/:id`
- `DELETE /v1/volumes/:id`

## Variaveis importantes

- `ORION_VOLUME_LVM_VG`: volume group usado pelo Orion
- `ORION_VOLUME_HOST_AGENT_LISTEN_ADDR`: endereco HTTP do agente
- `ORION_VOLUME_HOST_AGENT_STATE_DIR`: diretorio de marcadores locais

## Estado real atual

- nao ha backend alternativo ou loopback para mascarar ausencia de `LVM`;
- se o host nao tiver um `VG` valido ou se o processo nao tiver privilegio suficiente, o erro sobe como indisponibilidade do backend;
- o diretorio local nao guarda o volume em si, apenas estado auxiliar;
- o endpoint `GET /v1/volumes/:id` existe para o `orion-volume` reconciliar estado observado contra estado persistido.

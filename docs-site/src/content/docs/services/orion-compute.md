---
title: orion-compute
description: Orquestração local do ciclo de vida de servidores no Orion.
---

`orion-compute` concentra o ciclo de vida dos servidores dentro da celula. No desenho atual, ele e o servico que transforma um pedido de VM em selecao de host, task de build e estado final observado da instancia.

## Inspiracao OpenStack

- `Nova`
- separacao entre API de entrada, placement e execucao

## O que o Orion reaproveita

- operacoes longas como `tasks`;
- separacao entre porta de entrada publica e orquestracao de compute;
- integracao com placement para escolha de host;
- modelo de celula como dominio operacional da execucao.

## O que o Orion simplifica

- sem `conductor` separado;
- sem scheduler proprio;
- sem RPC interno complexo.

## Estado atual

O corte atual implementa:

- `GET /v1/servers`
- `POST /v1/servers`
- `GET /v1/servers/:id`
- `DELETE /v1/servers/:id`
- `POST /v1/servers/:id/attach-volume`
- `POST /v1/servers/:id/detach-volume`
- `GET /v1/tasks/:id`

Fluxo atual:

1. recebe a requisicao de create server vinda do `orion-api`;
2. converte `flavor` em recursos;
3. consulta o `orion-placement`;
4. pede uma porta por rede ao `orion-network`;
5. cria a `task`;
6. chama o `orion-node-agent`;
7. o agente define e inicia um dominio no `libvirt`;
8. retorna o servidor como `active` quando o build termina.

Fluxos adicionais ja implementados:

1. `delete server` com cleanup de dominio, disco local e portas OVN;
2. `attach-volume` e `detach-volume` coordenando `orion-volume` e `orion-node-agent`;
3. `list servers` para operacao e troubleshooting pela CLI.
4. reconciliacao periodica do estado observado dos dominios no host.

## Por que existe agora

Antes deste servico, a selecao de host e o estado do servidor estavam presos no `orion-api`. Isso era util para provar o primeiro slice, mas errado como arquitetura.

Com `orion-compute`, o `orion-api` volta a ser uma camada de entrada autenticada e o compute passa a ser o dono da orquestracao da VM.

## Estado atual do build

- boot real por imagem vindo do `orion-image`;
- selecao de host no `orion-placement`;
- alocacao de portas no `orion-network`;
- VIF OVS no `br-int` via `interfaceid=<port-id>`;
- build real no `libvirt`;
- delete real no `libvirt`;
- attach e detach reais de disco no `libvirt`.
- persistencia obrigatoria em `PostgreSQL` para `servers` e `tasks`;
- publicacao obrigatoria em `NATS` para eventos `task.*`.
- `GET /metrics` com metricas Prometheus e spans HTTP via `OpenTelemetry`.

## Proximo passo natural

Ampliar a reconciliacao para cleanup de recursos orfaos, retries e ampliar o modelo de rede com router e north/south.

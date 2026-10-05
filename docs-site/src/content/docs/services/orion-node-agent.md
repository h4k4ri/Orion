---
title: orion-node-agent
description: Agente de host que aplica execução real via libvirt.
---

`orion-node-agent` e o agente que roda no host e aplica a execucao concreta da VM no hypervisor.

## Inspiracao OpenStack

- papel operacional que, no ecossistema OpenStack, fica proximo do `nova-compute` no host;
- integracao com `libvirt` como backend principal.

## O que ele faz hoje

- recebe um pedido de build do `orion-compute`;
- expoe estado observado de um dominio especifico para reconciliacao;
- cria um overlay `qcow2` local via storage volume do `libvirt`;
- gera o XML do dominio;
- conecta NICs no `br-int` quando recebe portas OVN;
- faz `define` e `start` do dominio no `libvirt`;
- executa `destroy` e `undefine` no delete;
- executa attach e detach de volume pela API do `libvirt`;
- retorna o estado observado para o compute.

## Estado atual

O primeiro corte usa:

- API do `libvirt` via crate `virt`;
- storage local em `/var/tmp/orion-node-agent`;
- dominios `kvm` simples;
- overlay `qcow2` com backing file vindo do `orion-image`;
- VIF em `Open vSwitch` com `virtualport type='openvswitch'`;
- attach de NIC no `br-int` com `interfaceid` alinhado a `Logical_Switch_Port` do OVN;
- attach e detach reais de volume via `libvirt`.

Isso ja fecha uma fronteira importante do Orion: o control plane decide e coordena, mas a execucao concreta nasce no host.

## O que ainda falta

- stop, start e reboot por API;
- metadados mais ricos de dominio expostos para diagnostico via API.

## Endpoints atuais

- `GET /healthz`
- `POST /v1/servers/build`
- `GET /v1/servers/:id`
- `DELETE /v1/servers/:id`
- `POST /v1/servers/:id/volumes/attach`
- `POST /v1/servers/:id/volumes/detach`

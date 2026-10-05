---
title: Arquitetura Federada
description: Topologia e princípios operacionais do Orion.
---

O Orion e estruturado em **regioes, celulas, grupos de hosts e hosts**.

## Plano global

O plano global concentra:

- identidade;
- API publica;
- catalogo de servicos;
- politicas;
- placement agregado por celula.

Ele decide **onde** um workload deve nascer, mas nao executa diretamente a carga.

## Celulas

Cada celula e uma unidade operacional relativamente autonoma com seus proprios servicos locais:

- compute;
- placement;
- network;
- volume;
- banco de dados;
- barramento de mensagens.

Isso reduz gargalo central e limita blast radius.

## Hosts

Nos hosts ficam os agentes que aplicam o estado desejado:

- `orion-node-agent`
- `orion-network-host-agent`
- `orion-volume-host-agent`

Essa camada fica mais proxima de privilegios locais, dispositivos e concorrencia, por isso a direcao principal e usar `Rust`.

## Principio de continuidade

As VMs em execucao **nao devem depender do plano global** para continuar rodando. Se o plano global ficar indisponivel, as celulas devem continuar executando suas cargas ja ativas.

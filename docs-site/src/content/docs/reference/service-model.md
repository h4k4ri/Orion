---
title: Modelo de Serviços
description: O que o Orion reaproveita e simplifica a partir do OpenStack.
---

O Orion usa o OpenStack como referencia de **modelo operacional**, nao como alvo de compatibilidade.

## Identity

Inspiracao:

- `Keystone`

Reaproveitado:

- autenticacao centralizada;
- tokens escopados;
- papeis padrao;
- catalogo de servicos.

Simplificado:

- dominio unico `Default`;
- escopos `project` e `system`;
- tokens opacos no primeiro corte.

## Compute

Inspiracao:

- `Nova`

Reaproveitado:

- separacao entre API, scheduling e execucao;
- modelo celular;
- operacoes assincronas.

Simplificado:

- uma celula funcional primeiro;
- sem live migration inicial;
- sem multiplos hypervisors.

## Placement

Inspiracao:

- `Placement`

Reaproveitado:

- inventario de host;
- traits;
- capacidade agregada vs. alocacao final.

Simplificado:

- placement detalhado por celula primeiro;
- placement global so em fase posterior.

## Network

Inspiracao:

- `Neutron` com `OVN`

Reaproveitado:

- `network`, `subnet`, `port`, `security group`;
- separacao entre controle logico e aplicacao no host.

Simplificado:

- backend opinativo `OVS/OVN`;
- menos agentes e menos plugins;
- foco em conectividade basica e boot.

## Image

Inspiracao:

- `Glance`

Reaproveitado:

- catalogo de imagens;
- separacao entre metadado e dados;
- upload/import.

Simplificado:

- menos workflows no inicio;
- sem ecossistema grande de backends logo no primeiro corte.

## Volume

Inspiracao:

- `Cinder`

Reaproveitado:

- servico dedicado de block storage;
- attach/detach explicitos;
- modelo de driver.

Simplificado:

- um unico driver `LVM`;
- volumes locais a celula;
- sem multi-attach;
- attach e detach feitos no host pelo `orion-node-agent`;
- dependencia explicita de privilegio real no host para operar `LVM`.

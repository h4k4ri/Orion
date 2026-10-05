---
title: orion-image
description: Catálogo e store inicial de imagens do Orion.
---

`orion-image` e o servico que entrega as imagens usadas no boot das VMs.

## Inspiracao OpenStack

- `Glance`
- separacao entre metadados da imagem e o blob real
- catalogo central de imagens para os demais servicos

## O que o Orion reaproveita

- imagem como recurso catalogado;
- identificador estavel da imagem;
- metadados como formato, arquitetura e tamanho minimo de disco;
- separacao entre control plane e consumo no host.

## O que o Orion simplifica

- store local em filesystem;
- sem upload arbitrario no primeiro corte;
- sem backend pluggable no primeiro corte;
- bootstrap de uma imagem publica pequena para validar boot real.

## Estado atual

O servico sobe com uma imagem oficial pequena:

- `img_cirros_0_6_3_x86_64`
- `CirrOS 0.6.3 x86_64`
- formato `qcow2`
- store local em `/var/tmp/orion-image`

No primeiro start, o servico baixa a imagem, valida `SHA256` e passa a expo-la por API.

## Integracao atual

O `orion-compute` resolve o `image_id` com o `orion-image` antes de chamar o `orion-node-agent`.

O `orion-node-agent` usa essa imagem como backing file de um overlay `qcow2` local, entao a VM deixa de nascer de disco vazio e passa a bootar de uma imagem real.

## Proximo passo natural

- upload de imagens por API;
- metadados mais ricos;
- copy/import por celula;
- cache e distribuicao mais eficiente para hosts.

---
title: CLI
description: Escopo inicial da CLI do Orion para operacao e troubleshooting.
---

A CLI do Orion ja tem um slice funcional para o nucleo atual da plataforma. Ela cobre autenticacao, health, imagens, placement local, rede, servidores, volumes e leitura de tasks.

## Comandos implementados

- `orion token issue`
- `orion token validate`
- `orion health check`
- `orion image list`
- `orion image get <image-id>`
- `orion host list`
- `orion host get <host-id>`
- `orion host enable <host-id>`
- `orion host disable <host-id>`
- `orion host drain <host-id>`
- `orion host undrain <host-id>`
- `orion network list`
- `orion network get <network-id>`
- `orion network create --name <name>`
- `orion subnet list`
- `orion subnet get <subnet-id>`
- `orion subnet create --network <network-id> --name <name> --cidr <cidr>`
- `orion port list`
- `orion port get <port-id>`
- `orion port create --network <network-id>`
- `orion volume list`
- `orion volume get <volume-id>`
- `orion volume create --token <token> --name <name> --size-gb <n>`
- `orion volume delete <volume-id>`
- `orion server list`
- `orion server create --token <token> --name <name> --image <image-id>`
- `orion server get <server-id>`
- `orion server delete <server-id>`
- `orion server attach-volume <server-id> --volume <volume-id>`
- `orion server detach-volume <server-id> --volume <volume-id>`
- `orion task get <task-id>`

## Exemplos

```bash
./bin/orion health check
./bin/orion token issue --value-only
./bin/orion image list
./bin/orion host list
./bin/orion host drain host_local
./bin/orion host undrain host_local
NET_ID=$(./bin/orion network create --name tenant-net-a --field id)
./bin/orion subnet create --network "$NET_ID" --name tenant-subnet-a --cidr 10.50.0.0/24
./bin/orion server list --token "$TOKEN" --wide
./bin/orion volume create --token "$TOKEN" --name data-a --size-gb 1
./bin/orion server create --token "$TOKEN" --name vm-a --image img_cirros_0_6_3_x86_64 --flavor tiny --networks "$NET_ID"
./bin/orion server attach-volume --token "$TOKEN" <server-id> --volume <volume-id>
./bin/orion port list
```

## Por que isso entra cedo

Mesmo num MVP, operador de IaaS precisa inspecionar:

- onde a VM foi colocada;
- qual porta foi criada;
- qual IP foi alocado;
- se a task falhou ou completou;
- se o servico de rede refletiu `active` ou `down`.
- se um volume ficou `available`, `in-use` ou falhou por backend indisponivel.

Sem isso, cada diagnostico acaba voltando para `curl` e `journalctl`.

Para observabilidade de processo, os servicos HTTP do Orion tambem expoem `GET /metrics`.

## Formato de saida

A CLI fala com os servicos HTTP do Orion por padrao e hoje expoe:

- saida humana legivel em tabelas ASCII;
- `--output json` nos comandos implementados;
- `--field <campo>` para extrair valores simples sem `jq`;
- `get`, `list` e alguns `create` do caminho ja validado.
- `delete`, `attach-volume` e `detach-volume` para o caminho que ja existe hoje.

YAML ainda nao foi implementado. Quando entrar, deve manter o mesmo contrato logico do JSON.

## E2E real

O repositório tambem expoe um binario separado para validacao ponta a ponta do lab atual:

- `orion-e2e`

Ele executa:

- health checks de todos os servicos;
- emissao de token;
- criacao de rede e subnet;
- boot de duas VMs no mesmo `Logical_Switch`;
- validacao de porta no `OVN`;
- validacao de interface no `libvirt`;
- login no console serial do CirrOS;
- validacao do IP dentro do guest;
- ping guest-to-guest para provar o dataplane.

Uso:

```bash
go run ./cli/orion-cli/cmd/orion-e2e -volume auto
```

Modos de volume:

- `-volume auto`: tenta validar volume, mas faz `skip` se o backend `LVM` estiver indisponivel;
- `-volume on`: exige volume real e falha se o backend nao estiver pronto;
- `-volume off`: roda so compute e rede.

## Cobertura atual

O corte atual ja foi validado contra os servicos reais do Orion:

- `orion-identity`
- `orion-placement`
- `orion-image`
- `orion-network`
- `orion-api`
- `orion-compute`
- `orion-node-agent`
- `orion-volume`
- `orion-volume-host-agent`

Isso significa que os comandos de CLI nao estao so montando requests teoricos. Eles ja conversam com a malha real que hoje cria rede, porta, scheduling e VM em `libvirt` com `OVN/OVS`.

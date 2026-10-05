---
title: orion-network
description: Servico de rede do Orion, inspirado no Neutron com backend OVN/OVS.
---

`orion-network` e o servico de rede do Orion. O modelo segue o corte mais util do `Neutron` no inicio: `network`, `subnet` e `port`, com realizacao em `OVN` no plano logico e `OVS` no host.

## Escopo inicial

- `network` vira `Logical_Switch` no OVN Northbound.
- `subnet` define CIDR, gateway e `DHCP_Options`.
- `port` aloca MAC/IP e vira `Logical_Switch_Port`.
- o servico fala com o OVN NB por `libovsdb`, sem shelling out para `ovn-nbctl`.
- o `compute` pede uma porta por rede durante o build da VM.
- o `node-agent` conecta a interface da VM no `br-int` usando `interfaceid=<port-id>`.
- `GET /v1/ports` e `GET /v1/ports/:id` reconciliam `status` a partir do campo `up` do OVN.
- `PostgreSQL` guarda `network`, `subnet` e `port`.
- `NATS` publica eventos de ciclo de vida de rede.
- um loop periodico de reconciliacao atualiza `port.status` a partir do OVN.
- `GET /metrics` expõe metricas Prometheus e as rotas HTTP sao instrumentadas com `OpenTelemetry`.

## Inspiracao no OpenStack

- `Neutron` continua sendo o servico de API/controle.
- `OVN` fica com a realizacao logica e com o DHCP nativo.
- `OVS` fica no host, com `ovn-controller` aplicando os fluxos no `br-int`.

Esse e o mesmo desenho geral usado pelo backend `ML2/OVN` do OpenStack, mas aqui com escopo bem menor.

## Endpoints atuais

- `GET /healthz`
- `GET /v1/networks`
- `POST /v1/networks`
- `GET /v1/networks/:id`
- `GET /v1/subnets`
- `POST /v1/subnets`
- `GET /v1/subnets/:id`
- `GET /v1/ports`
- `POST /v1/ports`
- `GET /v1/ports/:id`
- `DELETE /v1/ports/:id`

## Exemplo

Cria uma rede:

```bash
curl -s -X POST http://127.0.0.1:8086/v1/networks \
  -H 'Content-Type: application/json' \
  -d '{
    "project_id":"proj_admin",
    "name":"tenant-net-a"
  }' | jq
```

Cria uma subnet:

```bash
curl -s -X POST http://127.0.0.1:8086/v1/subnets \
  -H 'Content-Type: application/json' \
  -d '{
    "project_id":"proj_admin",
    "network_id":"<NETWORK_ID>",
    "name":"tenant-subnet-a",
    "cidr":"10.42.0.0/24",
    "gateway_ip":"10.42.0.1",
    "enable_dhcp":true
  }' | jq
```

Depois disso, o `POST /v1/servers` pode receber `networks: ["<NETWORK_ID>"]`.

## Limitacoes atuais

- sem router e sem north/south networking ainda;
- sem security groups;
- sem API global de rede no `orion-api`;
- depende de `OVN NB`, `ovn-controller` e `br-int` reais no host.
- `ORION_DB_DSN` e `ORION_NATS_URL` sao obrigatorios para o service subir.
- uma porta criada isoladamente pela API tende a continuar `down` ate ser ligada a uma VIF real no host.

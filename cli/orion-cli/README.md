# orion-cli

CLI unificada do Orion.

Estado atual:

- subcomandos orientados a recurso;
- saida humana por padrao com tabelas ASCII;
- suporte a `--output json`;
- suporte a `--field <campo>` em comandos de recurso unico e `create`;
- acompanhamento de tasks como parte do fluxo normal de operacao.

Comandos implementados:

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

Exemplos:

```bash
./bin/orion health check
./bin/orion token issue --value-only
./bin/orion image list
./bin/orion host drain host_local
./bin/orion host undrain host_local
NET_ID=$(./bin/orion network create --name tenant-net-a --field id)
./bin/orion subnet create --network "$NET_ID" --name tenant-subnet-a --cidr 10.50.0.0/24
./bin/orion volume create --token "$TOKEN" --name data-a --size-gb 1
./bin/orion server create --token "$TOKEN" --name vm-a --image img_cirros_0_6_3_x86_64 --flavor tiny --networks "$NET_ID"
./bin/orion server attach-volume --token "$TOKEN" <server-id> --volume <volume-id>
```

Motivacao:

- o backend ja tem `identity`, `placement`, `compute`, `image` e `network` reais;
- o backend ja tem `network-host-agent`, `volume` e `volume-host-agent` reais;
- o operador precisa de comandos de inspecao antes de uma CLI mutadora mais ampla;
- isso reduz dependencia de `curl` no fluxo normal.

## E2E

O monorepo tambem inclui um binario de validacao ponta a ponta:

```bash
go run ./cli/orion-cli/cmd/orion-e2e -volume auto
```

Ele valida:

- servicos em `health`;
- token;
- rede e subnet;
- duas VMs reais no `libvirt`;
- portas `OVN` com `up=true`;
- login no console serial do CirrOS;
- ping guest-to-guest;
- caminho de volume em modo `auto`, `on` ou `off`.

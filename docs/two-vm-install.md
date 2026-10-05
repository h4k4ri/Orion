# Orion em Duas VMs

Guia de instalacao manual do Orion em duas VMs:

- `vm-control-plane`: API, identity, placement, compute, image, network, volume, PostgreSQL 18 e NATS.
- `vm-edge-01`: host que executa as VMs via `libvirt/qemu` e expõe os host agents.

Este guia cobre o que ja esta operacional hoje com os pacotes `.deb`.

## Topologia

Exemplo:

- `vm-control-plane`: `10.20.0.10`
- `vm-edge-01`: `10.20.0.21`

Portas relevantes:

- control plane:
  - `8080` `orion-api`
  - `8081` `orion-identity`
  - `8082` `orion-placement`
  - `8083` `orion-compute`
  - `8085` `orion-image`
  - `8086` `orion-network`
  - `8087` `orion-volume`
- edge host:
  - `8084` `orion-node-agent`
  - `8087` `orion-network-host-agent`
  - `8088` `orion-volume-host-agent`

## Estado atual

Funciona hoje:

- control plane distribuido em uma VM;
- edge host separado para `node-agent`, `network-host-agent` e `volume-host-agent`;
- boot de instancias no host remoto via `libvirt/qemu`;
- volumes `LVM` no host remoto;
- attach e detach de volume via API;
- metricas, healthz e OTLP no control plane.

Limites atuais que precisam ficar explicitos:

- o `compute` agora envia ao `node-agent` um descritor de imagem com `source_path`, `checksum` e `source_url`;
- se o `source_path` existir no edge host, ele usa esse caminho diretamente;
- se o `source_path` nao existir no edge host, o `node-agent` baixa a imagem do `orion-image` para um cache local no host e valida o checksum;
- para fluxo com interfaces de rede reais na VM convidada, voce ainda precisa de `OVN/OVS` funcional e `br-int` no host de ponta;
- o packaging atual cobre `orion-common`, `orion-control-plane`, `orion-host-agents` e `orion-cli`.

## 1. Gerar os pacotes

No repositorio:

```bash
make build
./packaging/deb/build.sh
ls -1 packaging/deb/out
```

Arquivos esperados:

- `orion-common_0.1.0_amd64.deb`
- `orion-control-plane_0.1.0_amd64.deb`
- `orion-host-agents_0.1.0_amd64.deb`
- `orion-cli_0.1.0_amd64.deb`

Copie:

- para `vm-control-plane`: `orion-common`, `orion-control-plane`, `orion-cli`
- para `vm-edge-01`: `orion-common`, `orion-host-agents`

## 2. Preparar a VM do control plane

Assumindo Debian 13 `trixie`.

### 2.1 Instalar dependencias base

```bash
sudo apt-get update
sudo apt-get install -y curl jq ca-certificates gnupg procps nats-server postgresql-common
curl -fsSL https://www.postgresql.org/media/keys/ACCC4CF8.asc | \
  sudo gpg --dearmor -o /usr/share/keyrings/postgresql.gpg
echo "deb [signed-by=/usr/share/keyrings/postgresql.gpg] https://apt.postgresql.org/pub/repos/apt trixie-pgdg main" | \
  sudo tee /etc/apt/sources.list.d/pgdg.list >/dev/null
sudo apt-get update
sudo apt-get install -y postgresql-18
```

### 2.2 Instalar os pacotes Orion

```bash
sudo dpkg -i ./orion-common_0.1.0_amd64.deb
sudo dpkg -i ./orion-control-plane_0.1.0_amd64.deb
sudo dpkg -i ./orion-cli_0.1.0_amd64.deb
```

### 2.3 Bootstrap de diretorios

```bash
sudo orion-bootstrap --ensure-env
sudo orion-bootstrap --ensure-dirs
```

### 2.4 Criar banco e usuario

```bash
sudo pg_ctlcluster 18 main start
sudo -u postgres psql -v ON_ERROR_STOP=1 <<'SQL'
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'orion') THEN
    CREATE ROLE orion LOGIN PASSWORD 'orion';
  END IF;
END
$$;
SQL
sudo -u postgres psql -tAc "SELECT 1 FROM pg_database WHERE datname = 'orion'" | grep -q 1 || \
  sudo -u postgres createdb -O orion orion
```

### 2.5 Ajustar `/etc/orion/orion.env`

Edite [orion.env](/etc/orion/orion.env) para apontar os host agents remotos. O image store pode ser compartilhado com a VM de edge, mas isso agora e opcional.

No modelo atual, `ORION_NODE_AGENT_URL` funciona como fallback de compatibilidade. O caminho padrao para multi-host e registrar cada host no `placement` com seu proprio `node_agent_url`.

`ORION_VOLUME_HOST_AGENT_URL` segue a mesma regra: funciona como fallback de compatibilidade. Em multi-host, o `orion-volume` resolve o `volume_host_agent_url` do host pelo `placement`.

Exemplo:

```bash
sudo tee /etc/orion/orion.env >/dev/null <<'EOF'
ORION_DB_DSN=postgres://orion:orion@127.0.0.1:5432/orion?sslmode=disable
ORION_NATS_URL=nats://127.0.0.1:4222
ORION_IDENTITY_URL=http://127.0.0.1:8081
ORION_PLACEMENT_URL=http://127.0.0.1:8082
ORION_COMPUTE_URL=http://127.0.0.1:8083
ORION_NODE_AGENT_URL=http://10.20.0.21:8084
ORION_NODE_AGENT_GRPC_ADDR=10.20.0.21:50051
ORION_IMAGE_URL=http://127.0.0.1:8085
ORION_NETWORK_URL=http://127.0.0.1:8086
ORION_VOLUME_URL=http://127.0.0.1:8087
ORION_VOLUME_HOST_AGENT_URL=http://10.20.0.21:8088
ORION_IMAGE_STORE_DIR=/srv/orion/image
ORION_NODE_AGENT_STORAGE_DIR=/var/lib/orion/node-agent
ORION_VOLUME_HOST_AGENT_STATE_DIR=/var/lib/orion/volume-host-agent
OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:4318

# Necessario para redes reais via OVN.
# Se voce ainda nao tiver OVN central, deixe comentado e nao tente criar portas/rede para as VMs.
# ORION_NETWORK_OVN_NB_DB=unix:/var/run/ovn/ovnnb_db.sock
# ou:
# ORION_NETWORK_OVN_NB_DB=tcp:10.20.0.10:6641
EOF
```

Crie o image store:

```bash
sudo mkdir -p /srv/orion/image
sudo chown -R "$USER":"$USER" /srv/orion
```

Modelos suportados:

- com storage compartilhado:
  - monte o mesmo diretorio na `vm-edge-01` com o mesmo path absoluto e o `node-agent` vai usar a imagem diretamente;
- sem storage compartilhado:
  - nao monte nada no edge;
  - o `node-agent` vai baixar a imagem do `orion-image` para `/var/lib/orion/node-agent/image-cache` quando precisar;
  - os boots seguintes no mesmo host reutilizam o cache local.

### 2.6 Subir os servicos

Com `systemd`:

```bash
sudo systemctl daemon-reload
sudo systemctl restart nats-server
sudo systemctl restart orion-identity orion-placement orion-image orion-network orion-volume orion-compute orion-api
```

### 2.7 Validar o control plane

```bash
curl -fsS http://127.0.0.1:8080/healthz | jq
curl -fsS http://127.0.0.1:8081/healthz | jq
curl -fsS http://127.0.0.1:8082/healthz | jq
curl -fsS http://127.0.0.1:8083/healthz | jq
curl -fsS http://127.0.0.1:8085/healthz | jq
curl -fsS http://127.0.0.1:8086/healthz | jq
curl -fsS http://127.0.0.1:8087/healthz | jq
curl -fsS http://127.0.0.1:8085/v1/images | jq
```

Credencial seeded por default:

- usuario: `admin`
- senha: `orion-admin`
- projeto: `proj_admin`

## 3. Preparar a VM de edge host

### 3.1 Instalar dependencias

```bash
sudo apt-get update
sudo apt-get install -y \
  ca-certificates \
  curl \
  jq \
  procps \
  lvm2 \
  libvirt-daemon-system \
  libvirt-daemon-driver-qemu \
  qemu-system-x86 \
  qemu-utils \
  openvswitch-switch
```

`openvswitch-switch` entra aqui porque, quando voce criar instancias com portas de rede, o `node-agent` espera `br-int` presente no host.

### 3.2 Instalar os pacotes Orion

```bash
sudo dpkg -i ./orion-common_0.1.0_amd64.deb
sudo dpkg -i ./orion-host-agents_0.1.0_amd64.deb
```

### 3.3 Bootstrap de diretorios

```bash
sudo orion-bootstrap --ensure-env
sudo orion-bootstrap --ensure-dirs
```

### 3.4 Criar o volume group para volumes

Exemplo usando um disco extra em `/dev/vdb`:

```bash
sudo pvcreate /dev/vdb
sudo vgcreate orion-vg /dev/vdb
```

Se voce quiser usar arquivo loopback para laboratorio, faca isso conscientemente:

```bash
sudo truncate -s 20G /var/lib/orion/orion-lvm.img
sudo losetup --find --show /var/lib/orion/orion-lvm.img
# assuma que retornou /dev/loop0
sudo pvcreate /dev/loop0
sudo vgcreate orion-vg /dev/loop0
```

### 3.5 Ajustar `/etc/orion/orion.env`

Exemplo:

```bash
sudo tee /etc/orion/orion.env >/dev/null <<'EOF'
ORION_NODE_AGENT_LISTEN_ADDR=0.0.0.0:8084
ORION_NETWORK_HOST_AGENT_LISTEN_ADDR=0.0.0.0:8087
ORION_VOLUME_HOST_AGENT_LISTEN_ADDR=0.0.0.0:8088
ORION_NETWORK_URL=http://10.20.0.10:8086
ORION_NATS_URL=nats://10.20.0.10:4222
ORION_NODE_AGENT_STORAGE_DIR=/var/lib/orion/node-agent
ORION_NETWORK_HOST_AGENT_STATE_DIR=/var/lib/orion/network-host-agent
ORION_VOLUME_HOST_AGENT_STATE_DIR=/var/lib/orion/volume-host-agent
ORION_VOLUME_LVM_VG=orion-vg
EOF
```

Storage compartilhado de imagens no edge e opcional.

Se voce quiser evitar download sob demanda no primeiro boot, monte no edge o mesmo diretorio configurado em `ORION_IMAGE_STORE_DIR` no control plane, por exemplo via NFS, 9p, CephFS ou outro compartilhamento.

Se nao quiser storage compartilhado, nao precisa criar nada alem de `/var/lib/orion/node-agent`; o `node-agent` baixa e guarda a imagem em cache local automaticamente.

### 3.6 Subir `libvirt`, `OVS` e os agents

```bash
sudo systemctl restart libvirtd
sudo systemctl restart openvswitch-switch
sudo systemctl restart orion-node-agent orion-network-host-agent orion-volume-host-agent
```

O `network-host-agent` agora garante `br-int` automaticamente quando houver portas bound para o host. Ainda assim, `openvswitch-switch` precisa estar funcional.

### 3.7 Validar o edge host

```bash
curl -fsS http://127.0.0.1:8084/healthz | jq
curl -fsS http://127.0.0.1:8087/healthz | jq
curl -fsS http://127.0.0.1:8088/healthz | jq
sudo virsh -c qemu:///system list --all
sudo vgs
```

## 4. Validacao cruzada

Da `vm-control-plane`, valide os agents remotos:

```bash
curl -fsS http://10.20.0.21:8084/healthz | jq
curl -fsS http://10.20.0.21:8088/healthz | jq
```

Emita um token:

```bash
curl -fsS \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"orion-admin","scope":{"type":"project","project_id":"proj_admin"}}' \
  http://127.0.0.1:8081/v1/auth/tokens | jq
```

Cheque o placement:

```bash
curl -fsS http://127.0.0.1:8082/v1/hosts/host_local | jq
```

Registre o edge host remoto com os endpoints dos agents:

```bash
curl -fsS \
  -H 'Content-Type: application/json' \
  -d '{
    "host_id":"vm-edge-01",
    "cell_id":"cell_local",
    "group":"general",
    "enabled":true,
    "drained":false,
    "node_agent_url":"http://10.20.0.21:8084",
    "network_host_agent_url":"http://10.20.0.21:8087",
    "volume_host_agent_url":"http://10.20.0.21:8088",
    "traits":["general","kvm","edge"],
    "vcpus_total":8,
    "memory_mb_total":16384,
    "disk_gb_total":100
  }' \
  http://127.0.0.1:8082/v1/hosts | jq
```

Se voce quiser forcar o scheduler a usar apenas o edge remoto, desabilite o host local seeded:

```bash
curl -fsS -X POST http://127.0.0.1:8082/v1/hosts/host_local/disable | jq
```

## 5. Primeiro teste funcional sem rede de instancia

Se voce ainda nao tiver `OVN/OVS` pronto, faca o primeiro teste sem VIF.

### 5.1 Criar token

```bash
TOKEN=$(
  curl -fsS \
    -H 'Content-Type: application/json' \
    -d '{"username":"admin","password":"orion-admin","scope":{"type":"project","project_id":"proj_admin"}}' \
    http://127.0.0.1:8081/v1/auth/tokens | jq -r '.token.value'
)
```

### 5.2 Criar servidor sem redes

```bash
curl -fsS \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"name":"vm-edge-test","image_id":"img_cirros_0_6_3_x86_64","flavor":"tiny","networks":[],"traits_required":["general"]}' \
  http://127.0.0.1:8080/v1/servers | jq
```

### 5.3 Criar volume

```bash
curl -fsS \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"name":"vol-edge-test","size_gb":1}' \
  http://127.0.0.1:8080/v1/volumes | jq
```

### 5.4 Fazer attach do volume

```bash
curl -fsS \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"volume_id":"vol_xxx"}' \
  http://127.0.0.1:8080/v1/servers/srv_xxx/attach-volume | jq
```

Esse caminho valida o principal da topologia:

- control plane em uma VM;
- `node-agent` em outra VM;
- `volume-host-agent` em outra VM;
- `libvirt` remoto;
- `LVM` remoto.

## 6. Para subir com rede de instancia de verdade

O minimo adicional e:

- `orion-network` apontando para um `OVN Northbound DB` valido via `ORION_NETWORK_OVN_NB_DB`;
- `OVS` funcional no host de ponta;
- `br-int` existente no host de ponta;
- ou path de imagem compartilhado entre control plane e edge, ou reachability HTTP do edge host para o `orion-image`;
- reachability entre control plane e edge.

Sem isso:

- `healthz` do `orion-network` pode subir;
- mas criacao de rede, sub-rede, porta e boot com NIC vao falhar ou ficar inconsistentes.

## 7. Troubleshooting rapido

`node-agent` nao consegue bootar a VM:

- confira `curl http://vm-edge-01:8084/healthz`;
- confira `sudo virsh -c qemu:///system list --all`;
- confira se o edge consegue acessar o `source_path` compartilhado, ou se consegue baixar do `orion-image`;
- confira o cache local em `/var/lib/orion/node-agent/image-cache`.

`volume-host-agent` falha:

- confira `curl http://vm-edge-01:8088/healthz`;
- confira `sudo vgs`;
- confira `echo $ORION_VOLUME_LVM_VG` no env do host.

`create server` falha depois do placement:

- confira reachability do control plane para `8084` e `8088`;
- confira logs de [orion-compute](/var/log/orion/orion-compute.log);
- confira logs de [orion-node-agent](/var/log/orion/orion-node-agent.log).

Rede da instancia falha:

- confira `ORION_NETWORK_OVN_NB_DB`;
- confira `sudo ovs-vsctl show`;
- confira se `br-int` existe;
- confira se a porta foi criada no OVN.

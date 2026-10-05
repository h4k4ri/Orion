# Lab em `virsh`

Script para subir duas VMs Debian 13 no `libvirt`:

- `vm-control-plane`
- `vm-edge-01`

Uso:

```bash
chmod +x deploy/libvirt/two-vm-lab.sh
sudo ./deploy/libvirt/two-vm-lab.sh up
sudo ./deploy/libvirt/two-vm-lab.sh status
sudo ./deploy/libvirt/two-vm-lab.sh down
sudo ./deploy/libvirt/two-vm-lab.sh destroy
```

O script faz:

- cria uma rede NAT `10.20.0.0/24` no `libvirt`;
- baixa a imagem cloud Debian 13 `genericcloud` em `qcow2`;
- cria discos overlay para as duas VMs;
- cria seed `cloud-init` para hostname, usuario, senha e pacotes base;
- prepara a VM de edge com `libvirt`, `openvswitch` e um disco extra para `LVM`.

Defaults importantes:

- control plane: `10.20.0.10`
- edge host: `10.20.0.21`
- usuario SSH: `orion`
- senha SSH: `orion`

Se existir chave publica em `~/.ssh/id_ed25519.pub`, `~/.ssh/id_ecdsa.pub` ou `~/.ssh/id_rsa.pub`, ela tambem e injetada.

Variaveis uteis:

```bash
LAB_DIR=/var/lib/libvirt/images/orion-lab
BASE_IMAGE_URL=https://cloud.debian.org/images/cloud/trixie/latest/debian-13-genericcloud-amd64.qcow2
VM_SSH_USER=orion
VM_SSH_PASSWORD=orion
SSH_PUBLIC_KEY_FILE=$HOME/.ssh/id_ed25519.pub
```

Limitacoes:

- o edge host fica pronto para rodar `libvirt/qemu`, mas guests aninhados com `KVM` real ainda dependem de nested virtualization habilitada no host fisico;
- o script sobe o laboratorio base; a instalacao dos pacotes `.deb` do Orion continua separada e deve seguir [docs/two-vm-install.md](/home/horizon/orion/docs/two-vm-install.md).

Para validar especificamente a semantica atual de rede em dois hosts de edge, use tambem [THREE_VM_NETWORK_LAB.md](/home/horizon/orion/deploy/libvirt/THREE_VM_NETWORK_LAB.md) e [three-vm-network-lab.sh](/home/horizon/orion/deploy/libvirt/three-vm-network-lab.sh).

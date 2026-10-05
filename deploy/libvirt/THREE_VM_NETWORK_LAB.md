# Lab de Rede em 3 VMs

Script para validar a semantica atual de rede do Orion com:

- `control-plane-01`
- `host-edge-01`
- `host-edge-02`

Uso:

```bash
chmod +x deploy/libvirt/three-vm-network-lab.sh
./deploy/libvirt/three-vm-network-lab.sh up
./deploy/libvirt/three-vm-network-lab.sh install
./deploy/libvirt/three-vm-network-lab.sh verify-network
```

Ou fluxo direto:

```bash
./deploy/libvirt/three-vm-network-lab.sh all
```

O que ele valida:

- mesma rede logica criada no `orion-network`;
- uma porta bound para `host-edge-01`;
- outra porta bound para `host-edge-02`;
- cada `orion-network-host-agent` reconciliando apenas as portas do proprio host;
- a mesma rede aparecendo localmente nos dois hosts.

Isso responde a pergunta operacional sobre replicacao de rede entre computes.

Limite importante:

- o script continua focado em validacao de rede e bindings por host;
- ele nao exercita ainda o fluxo completo de boot de guests via `orion-compute`.

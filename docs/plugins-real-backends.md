# Plugins com backends reais

Os plugins abaixo executam operações no backend externo; o estado autoritativo
fica no backend e não em um mapa simulado do plugin.

| Plugin | Backend | Configuração mínima |
|---|---|---|
| `ceph` | Ceph RBD | `ORION_CEPH_POOL` e credenciais/configuração do `rbd` |
| `nfs` | `exportfs` + NFSv4 | `ORION_NFS_SERVER` |
| `glusterfs` | Gluster volume/snapshot/quota | `ORION_GLUSTER_SERVER` |
| `buckets` | S3 compatível, incluindo MinIO e Ceph RGW | `ORION_S3_ENDPOINT`, `ORION_S3_ACCESS_KEY`, `ORION_S3_SECRET_KEY` |
| `openbao` | OpenBao KV v2 | `ORION_OPENBAO_ADDR`, `ORION_OPENBAO_TOKEN` |
| `designate` | OpenStack Designate v2 | `ORION_DESIGNATE_ENDPOINT`, `ORION_DESIGNATE_TOKEN` |
| `zfs` | ZFS local | `zfs` instalado e um pool configurado |
| `cephfs` | CephFS subvolumes | `ORION_CEPHFS_NAME`, `ORION_CEPHFS_MONITORS` |
| `smb` | Samba/SMB | `ORION_SMB_SERVER` |
| `iscsi` | Linux LIO via `targetcli` | `ORION_ISCSI_PORTAL` |
| `nvmeof` | Linux NVMe target via `nvmetcli` | `ORION_NVMEOF_TRADDR` |

Todos aceitam `ORION_PLUGIN_ENDPOINT` para evitar colisão de portas quando
mais de um plugin é executado no mesmo host.

## Variáveis específicas

Ceph aceita `ORION_CEPH_NAMESPACE`, `ORION_CEPH_CONF`, `ORION_CEPH_USER` e
`ORION_CEPH_KEYRING`. As operações de attach/detach exigem permissões para
mapear ou desmontar RBDs.

NFS usa `ORION_NFS_EXPORT_ROOT` (padrão `/srv/orion/nfs`),
`ORION_NFS_CLIENTS` e `ORION_NFS_EXPORT_OPTIONS`. Os exports são registrados
com `exportfs` e precisam ser persistidos pelo operador se devem sobreviver a
um reboot.

GlusterFS usa `ORION_GLUSTER_BRICK_ROOT` e, para réplicas, `ORION_GLUSTER_REPLICA`
e `ORION_GLUSTER_BRICKS` no formato `host:/caminho/base`. Resize aplica quota
real no volume, pois GlusterFS não é um dispositivo de bloco.

Buckets usa `ORION_S3_REGION` e `ORION_S3_INSECURE=true` somente para endpoints
HTTP de laboratório.

OpenBao usa o mount KV v2 `secret` por padrão; `ORION_OPENBAO_MOUNT`,
`ORION_OPENBAO_NAMESPACE` e `ORION_OPENBAO_TLS_SKIP_VERIFY=true` são opcionais.

Designate acrescenta `/v2` ao endpoint quando necessário e usa
`X-Auth-Token`. `ORION_DESIGNATE_INSECURE_TLS=true` é destinado apenas a
ambientes de laboratório.

CephFS cria subvolumes com `ceph fs subvolume` e monta via kernel CephFS.
SMB gerencia blocos marcados no `smb.conf`, valida-os com `testparm` e recarrega
o Samba com `smbcontrol`. iSCSI cria backstores fileio e LUNs no Linux LIO.
NVMe-oF mantém a configuração `nvmetcli`, cria namespaces sobre arquivos
esparsos e conecta clientes usando `nvme connect`.

Os plugins `mock` e `mock-compute` continuam deliberadamente simulados para
testes de protocolo e compensação. `my-plugin` continua sendo um template.

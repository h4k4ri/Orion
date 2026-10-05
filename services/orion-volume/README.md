# orion-volume

Servico de block storage do Orion.

Escopo inicial:

- create/delete;
- attach/detach;
- backend unico `LVM`;
- localidade restrita a celula;
- `volume-host-agent` resolvido por `host_id` via `placement`;
- volume local nao troca de host silenciosamente no attach.

# Debian Packaging

Empacotamento oficial do Orion em `.deb`.

Estado atual:

- `build.sh` gera um primeiro conjunto de pacotes `.deb`;
- `orion-bootstrap.sh` prepara diretórios e um `orion.env` base;
- as units `systemd` são emitidas pelo build para control plane e host agents;
- o layout ainda é inicial, mas já é utilizável para instalação local previsível.

Pacotes gerados:

- `orion-common`
- `orion-control-plane`
- `orion-host-agents`
- `orion-cli`

Uso:

```bash
make build
./packaging/deb/build.sh
ls -1 packaging/deb/out
```

Bootstrap:

```bash
./packaging/deb/orion-bootstrap.sh --ensure-env
./packaging/deb/orion-bootstrap.sh --print-env
```

Smoke e2e em Docker:

```bash
./packaging/deb/e2e/run.sh
```

O teste instala os `.deb` gerados, executa `orion-bootstrap`, sobe PostgreSQL e NATS no container e valida:

- `healthz` de todos os serviços empacotados;
- `healthz` do `orion-network-host-agent`;
- `/metrics` do placement;
- auth básico no identity;
- bootstrap do image service;
- inventory enriquecido do `host_local` no placement.
- upgrade de schemas legados para `identity`, `network` e `placement`.

Instalacao manual em duas VMs:

- [docs/two-vm-install.md](/home/horizon/orion/docs/two-vm-install.md)

Próximos passos naturais:

- separar pacotes por serviço quando houver fronteiras estáveis;
- adicionar usuários dedicados, `tmpfiles.d` e scripts `postinst` mais estritos;
- versionar templates de `systemd` em vez de emiti-los apenas no build;
- integrar assinatura, changelog Debian e pipeline de release.

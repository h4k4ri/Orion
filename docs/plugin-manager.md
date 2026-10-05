# Orion plugin manager

Os plugins Orion continuam sendo executáveis independentes. O SDK agora gera automaticamente um manifesto antes de abrir o servidor gRPC quando `ORION_PLUGIN_MANIFEST` está definido. O gerenciador usa esse manifesto para confirmar que o processo iniciou e reserva uma porta TCP livre quando `ORION_PLUGIN_ENDPOINT` não é informado.

## Empacotar, instalar e iniciar

Gere um pacote nativo do Orion, sem Docker:

```bash
orion plugin package \
  --id=nfs \
  --name=NFS \
  --version=1.0.0 \
  --binary=/opt/orion/plugins/nfs \
  --output=nfs-1.0.0.orion-plugin.tar.gz
orion plugin install-package nfs-1.0.0.orion-plugin.tar.gz
```

O pacote contém o executável, `plugin.json` e checksum SHA-256. A instalação
extrai o plugin para o diretório gerenciado pelo Orion e valida os caminhos do
arquivo antes de escrever no disco.

Depois, inicie o plugin:

```bash
export ORION_PLUGIN_STATE="$HOME/.config/orion/plugins.json"
orion plugin install \
  --id=nfs \
  --name=NFS \
  --version=1.0.0 \
  /opt/orion/plugins/nfs
orion plugin start nfs
orion plugin doctor nfs
orion plugin stop nfs
```

`install` aceita `--env KEY=VALUE` e `--endpoint host:port`. Sem endpoint, cada processo recebe uma porta local livre. O estado, PID, endpoint e caminho do manifesto são persistidos, portanto uma nova invocação da CLI consegue inspecionar ou parar um processo iniciado anteriormente.

## Descoberta

Coloque descritores `*.plugin.json` em um diretório:

```json
{
  "id": "nfs",
  "name": "NFS",
  "version": "1.0.0",
  "vendor": "Linux NFS",
  "binary": "orion-nfs",
  "env": {"ORION_NFS_EXPORT_ROOT": "/srv/exports"}
}
```

Depois execute:

```bash
orion plugin discover /opt/orion/plugins
```

O arquivo é apenas o descritor de instalação. O manifesto de capacidades é gerado pelo próprio plugin a partir dos handlers realmente registrados, o que evita divergência entre documentação e implementação.

## Limites da arquitetura

Os serviços centrais do Orion continuam sendo os donos dos seus dados e
contratos. Em particular, identidade/autorização pertence ao `orion-identity` e
agendamento pertence ao `orion-placement`. Plugins não recriam esses serviços;
eles implementam backends de capacidades adicionais, como armazenamento,
rede, compute, balanceamento, orquestração e bare metal.

# Plano de Execucao Inicial

## Objetivo

Transformar a arquitetura atual do Orion em um caminho de entrega realista, com escopo controlado, contratos estaveis cedo e crescimento progressivo de uma celula unica para uma cloud federada funcional.

O principio central deste plano e simples:

- Provar primeiro uma celula operacional.
- Manter o desenho federado desde o inicio.
- Adiar tudo que aumenta complexidade sem validar o nucleo.

## Principios de escopo

- O Orion nao tenta ser um "OpenStack menor" na primeira fase.
- Toda operacao longa nasce como `task` assincrona com estados e eventos.
- O plano global decide onde a carga nasce, mas nao opera diretamente o hypervisor.
- Cada celula precisa continuar executando workloads existentes mesmo com indisponibilidade do plano global.
- O primeiro backend de volume e exclusivamente `LVM`.
- A primeira versao de rede deve cobrir apenas o essencial para boot e conectividade basica.

## Nao objetivos iniciais

- Live migration.
- Multi-attach.
- Storage compartilhado entre celulas.
- Quotas avancadas.
- Politicas sofisticadas de placement global.
- Suporte a multiplos hypervisors.
- Suporte a multiplos backends de block storage.
- Recursos avancados de SDN alem do necessario para redes, portas, IPAM, security groups e roteamento basico.

## Fases

### MVP

Meta: provar o fluxo completo de uma VM em uma unica celula, com uma API unica, uma CLI unica e um node agent funcional.

Escopo do MVP:

- `orion-api` como porta de entrada unica.
- `orion-identity` minimo para autenticacao, autorizacao simples, tenants/projetos e tokens.
- `orion-compute` com create/get/list/delete de servidores.
- `orion-placement` local na celula com inventario de CPU, RAM, disco e traits basicos.
- `orion-network` com redes, sub-redes, portas e binding minimo para hosts via `OVS/OVN`.
- `orion-image` com catalogo, upload e distribuicao basica de imagens.
- `orion-volume` com create/delete/attach/detach usando `LVM`.
- `orion-node-agent` para aplicar estado no host via `libvirt`.
- `orion-cli` cobrindo os fluxos operacionais essenciais.
- `PostgreSQL` e `NATS` como dependencias padrao.

Resultado esperado do MVP:

- Subir imagem.
- Criar rede e subnet.
- Criar volume.
- Criar VM com boot por imagem.
- Anexar volume a VM.
- Consultar task e estado do servidor.
- Remover VM e limpar recursos associados.

Exclusoes do MVP:

- Federation real com multiplas celulas em producao.
- Scheduling global sofisticado.
- HA forte.
- Snapshots de volume.
- Resize.
- IPv6.
- Security groups complexos.

### v0.1

Meta: endurecer a celula unica, consolidar contratos e tornar o sistema operacionalmente repetivel.

Escopo adicional:

- Reconciliacao explicita em compute, network e volume.
- Drain e disable de hosts.
- Estados de erro mais claros e limpeza automatica de recursos orfaos.
- Health checks por servico, celula e host.
- Politica minima de retry e idempotencia de tasks.
- Inventario mais rico no placement local: NUMA, GPUs e traits estaveis.
- Observabilidade basica: logs estruturados, metricas e correlacao por `task_id` e `request_id`.
- Empacotamento `.deb` inicial para servicos principais, agentes e CLI.

Resultado esperado:

- Uma celula unica operavel por CLI, com instalacao previsivel e diagnostico basico.
- Falhas transitorias tratadas por reconciliacao em vez de reparo manual frequente.

### v0.2

Meta: ativar o primeiro recorte real de cloud federada.

Escopo adicional:

- Registro de multiplas celulas no plano global.
- Catalogo global de regioes, celulas e endpoints.
- Placement global agregado por celula.
- Scheduler global escolhendo a celula antes do scheduler local escolher o host.
- Roteamento de tasks do plano global para o dominio operacional correto.
- Visao global agregada de capacidade, saude e workloads.
- Restricoes explicitas de localidade para attach e operacoes de volume.

Resultado esperado:

- Criar workloads em mais de uma celula via uma unica API.
- Isolar falhas por celula.
- Preservar continuidade de workloads existentes quando o plano global estiver indisponivel.

Exclusoes de v0.2:

- Mobilidade transparente entre celulas.
- Balanceamento inteligente multi-regiao.
- Politicas complexas de afinidade e anti-afinidade.

### v1

Meta: entregar um baseline de plataforma utilizavel de forma consistente por operadores.

Escopo adicional:

- RBAC mais completo.
- Quotas basicas por organizacao/projeto.
- Snapshots e resize de volume onde o modelo permitir.
- API e CLI mais estaveis, com contratos versionados.
- Hardening de seguranca nos agentes.
- Fluxos de upgrade e rollback empacotados.
- Documentacao operacional completa.
- Testes de integracao por fluxo principal.

Resultado esperado:

- Produto coerente para operacao controlada em ambientes reais, ainda com escopo opinativo e sem perseguir paridade com OpenStack.

## Ordem recomendada de implementacao

O erro mais caro aqui seria comecar por servicos demais em paralelo. A ordem abaixo preserva dependencias reais e reduz retrabalho:

1. Fundacao compartilhada
2. Identidade minima
3. API unificada
4. Tasks e eventos
5. Image service
6. Placement local
7. Node agent
8. Compute local
9. Network service e host bindings
10. Volume service e volume host agent
11. CLI
12. Placement global e scheduler global
13. Catalogo global de celulas e regioes
14. Empacotamento `.deb`, observabilidade e hardening

## Fundacao compartilhada

Entregar antes de tudo:

- Configuracao padrao por servico.
- Logging estruturado.
- Cliente `PostgreSQL`.
- Cliente `NATS`.
- Tipos comuns de `task`, `event`, `resource_ref`, `error`.
- Middleware HTTP com autenticacao, `request_id` e tracing.
- Convencao de estados para recursos e tasks.

Sem isso, os servicos nascem com contratos inconsistentes.

## Contratos minimos iniciais

Os contratos abaixo devem existir cedo, mesmo que simplificados. Eles sao mais importantes que o volume de features.

### 1. Envelope de task

Toda operacao longa deve ser materializada como task:

```text
task_id
kind
scope (global|cell|host)
target_ref
status (pending|running|succeeded|failed|cancelled)
requested_by
request_id
cell_id
host_id
error_code
error_message
created_at
updated_at
```

Eventos minimos:

- `task.created`
- `task.started`
- `task.progress`
- `task.succeeded`
- `task.failed`

### 2. Inventario de host

Publicado pelo host para o placement local:

```text
host_id
cell_id
enabled
drained
cpu.total
cpu.allocated
memory_mb.total
memory_mb.allocated
disk_gb.total
disk_gb.allocated
traits[]
numa[]
gpus[]
nics[]
heartbeat_at
generation
```

Regras iniciais:

- Update otimista por `generation`.
- Heartbeat e inventario sao coisas diferentes.
- Placement nunca deve inferir host saudavel apenas por inventario antigo.

### 3. Contrato global -> celula para create server

O plano global escolhe a celula e entrega uma requisicao canonica:

```text
server_id
project_id
cell_id
name
image_id
flavor
networks[]
boot_volume
data_volumes[]
metadata
traits_required[]
host_group_hint
request_id
```

A celula responde com:

```text
task_id
cell_id
accepted_at
```

### 4. Contrato compute -> node agent

A interacao com o host deve ser declarativa o suficiente para suportar reconciliacao:

```text
desired_server_state
server_id
revision
power_state
vcpus
memory_mb
image_ref
root_disk
attached_volumes[]
interfaces[]
metadata
```

Resposta do agente:

```text
server_id
revision
observed_state
power_state
libvirt_domain_id
attachments[]
interfaces[]
last_error
observed_at
```

### 5. Contrato volume

Modelo inicial:

- Volume pertence a uma unica celula.
- Volume pode estar `available`, `attaching`, `in-use`, `detaching`, `deleting`, `error`.
- Attach so e permitido dentro da mesma celula.
- Cada attach gera um registro explicito de anexo com `volume_id`, `server_id`, `host_id`, `device_name`.

### 6. Contrato de rede

Modelo inicial:

- Rede.
- Subnet.
- Porta.
- Security group basico.
- Binding de porta para host.

Informacoes minimas por porta:

```text
port_id
network_id
subnet_id
project_id
mac_address
fixed_ips[]
security_groups[]
device_owner
device_id
binding_host_id
binding_vif_type
status
```

## Sequencia de servicos por fase

### MVP

- `services/orion-identity`
- `services/orion-api`
- `services/orion-image`
- `services/orion-placement`
- `agents/orion-node-agent`
- `services/orion-compute`
- `services/orion-network`
- `agents/orion-network-host-agent`
- `services/orion-volume`
- `agents/orion-volume-host-agent`
- `cli/orion-cli`

### v0.1

- hardening dos servicos existentes
- observabilidade
- packaging `.deb`
- health e reconciliacao

### v0.2

- extensao global de `orion-placement`
- catalogo global
- scheduler global
- federacao de celulas

### v1

- quotas
- RBAC mais fino
- snapshots e resize compativeis com o modelo inicial
- upgrade e rollback

## Layout inicial do monorepo

```text
services/
  orion-api/
  orion-identity/
  orion-compute/
  orion-placement/
  orion-network/
  orion-image/
  orion-volume/
agents/
  orion-node-agent/
  orion-network-host-agent/
  orion-volume-host-agent/
cli/
  orion-cli/
libs/
  go/
  rust/
packaging/
  deb/
docs/
deploy/
```

## Regras para o monorepo

- `libs/go` e `libs/rust` nao recebem regra de dominio; apenas codigo transversal e estavel.
- Integracoes concretas ficam perto do servico que as usa, nao em `libs/`.
- Eventos, contratos e tipos compartilhados devem ser pequenos, versionaveis e explicitamente donos de um contexto.
- Cada servico principal deve seguir hexagonal pragmatica:
  - `internal/domain`
  - `internal/application`
  - `internal/ports`
  - `internal/adapters`
  - `cmd/<service>`

## Backlog de fundacao sugerido

Antes de implementar o fluxo de VM, vale fechar estes itens:

1. Padrao de configuracao por arquivo e env vars.
2. Convencao de IDs (`srv_`, `vol_`, `tsk_`, `cell_`, `host_`).
3. Estados canonicos por recurso.
4. Envelope de erro de API.
5. Envelope de task e eventos em `NATS`.
6. Convencao de autenticacao para CLI e servicos internos.
7. Modelo de heartbeat de servicos e hosts.

## Definicao de pronto por fase

Uma fase so deve encerrar quando:

- os fluxos principais estiverem operaveis por CLI;
- o estado puder ser observado via API e task tracking;
- os contratos centrais estiverem documentados e refletidos em codigo;
- a reconciliacao basica estiver funcionando para os recursos da fase.

Se isso nao estiver fechado, adicionar feature nova aumenta divida em vez de ampliar capacidade real.

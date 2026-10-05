# Orion — Plano de Consolidação Arquitetural

Documento vivo. Cada fase entrega algo executável e mantém a regra central:

> **O control plane declara o estado desejado. Os agents materializam e reconciliam esse estado.**

Convenções:

- Status: `[ ]` pendente · `[~]` em andamento · `[x]` concluído
- Toda mensagem deve carregar `request_id` e `operation_id`
- Toda mutação deve ser idempotente
- Toda chamada síncrona interna: **gRPC + Protobuf**
- Toda chamada assíncrona: **NATS JetStream**
- API pública externa: **REST + JSON** (não muda para o cliente)
- Separar sempre **Resource** (server, volume, port, image) de **Operation** (ação assíncrona sobre um resource)

### Fronteira de transporte (regra explícita)

| Tipo de interação | Transporte |
|---|---|
| Queries e comandos síncronos de controle | **gRPC** |
| Workflows assíncronos, comandos duráveis, eventos | **NATS JetStream** |

Exemplos:

- `ObserveInstance`, `Heartbeat`, `RegisterNode`, `GetInventory` → **gRPC**
- `instance.create`, `instance.created`, `volume.attached`, `port.allocated` → **NATS**

Após M3, `BuildInstance` (gRPC) deixa de ser o caminho principal de provisionamento. Provisionamento passa a ser:

```
orion-compute → Operation → NATS command → agent reconcile → libvirt
```

`BuildInstance` gRPC continua existindo para casos síncronos administrativos (ex: debug), mas o fluxo default é via Operation + reconciliação.

---

## Fase 0 — Contratos Protobuf (`proto/`)

Objetivo: fonte única de verdade para Go ↔ Rust.

- [ ] Criar `proto/` na raiz do workspace
- [ ] Criar `proto/common/v1/*.proto` com tipos compartilhados:
  - `ResourceID`, `ProjectID`, `RequestID`, `OperationID`
  - `Timestamp`, `Error`, `AuditInfo`
- [ ] Criar `proto/compute/v1/compute.proto`:
  - `Instance`, `Flavor`, `InstanceState` enum
- [ ] Criar `proto/node/v1/node.proto`:
  - `Node`, `NodeResources`, `NodeTraits`, `NodeTopology`
  - `NodeRegistration`, `Heartbeat`, `ReconcileRequest`
- [ ] Criar `proto/network/v1/network.proto`:
  - `Network`, `Subnet`, `Router`, `Port`, `FloatingIP`
- [ ] Criar `proto/volume/v1/volume.proto`:
  - `Volume`, `Snapshot`, `Attachment`, `VolumeBackend`
- [ ] Criar `proto/placement/v1/placement.proto`:
  - `Inventory`, `Allocation`, `Trait`, `SchedulerHints`
- [ ] Criar `proto/operation/v1/operation.proto`:
  - `Operation`, `OperationState` enum, `OperationStep` enum
- [ ] Adicionar `buf.gen.yaml` gerando:
  - Go → `gen/go/...`
  - Rust → `gen/rust/...` (tonic + prost)
- [ ] Adicionar `Makefile` target `proto-gen`
- [ ] Versionar tudo em `v1` desde o dia 1

**Critério de aceite:** `make proto-gen` gera código Go importável e código Rust compilável.

---

## Fase 1 — Transporte interno (HTTP → gRPC) + observabilidade base `[x]`

Objetivo: eliminar JSON/HTTP entre control plane e agents. **Observabilidade nasce aqui**, não em M8.

- [ ] Adicionar `google.golang.org/grpc` direto (hoje só indirect) no `go.mod`
- [ ] Adicionar `tonic` + `prost` no workspace Rust (`Cargo.toml`)
- [ ] Reescrever `services/orion-compute/internal/adapters/nodeagent/client.go`:
  - Trocar `*http.Client` por cliente gRPC gerado
  - Endpoints atuais → RPCs:
    - `POST /v1/servers/build` → `ComputeService.BuildInstance`
    - `DELETE /v1/servers/{id}` → `ComputeService.DestroyInstance`
    - `POST /v1/servers/{id}/volumes/attach` → `VolumeService.Attach`
    - `POST /v1/servers/{id}/volumes/detach` → `VolumeService.Detach`
    - `GET /v1/servers/{id}` → `ComputeService.ObserveInstance`
- [ ] Reescrever `agents/orion-node-agent/src/main.rs`:
  - Substituir `axum` por servidor `tonic`
  - Manter libvirt crate `virt 0.4` (já ok)
- [ ] Idem para `orion-network-host-agent` e `orion-volume-host-agent`
- [ ] Manter axum apenas para health/readiness/metrics em `/healthz`, `/metrics`
- [ ] Adicionar middleware gRPC: tracing, request_id, retry, timeout

**Observabilidade base (entra em M1, não espera M8):**
- [ ] `request_id` propagado em todo handler
- [ ] `trace_id` via OpenTelemetry interceptor (gRPC)
- [ ] `operation_id` em logs estruturados quando conhecido
- [ ] Logs JSON estruturados em todos os serviços Go e agents Rust
- [ ] Métricas gRPC: `orion_grpc_request_duration_seconds{service,method,status}`
- [ ] Endpoint `/metrics` Prometheus exposto em todo serviço

**Critério de aceite:**
- `orion-compute` se comunica com node-agent exclusivamente via gRPC em produção
- Trace de uma request REST aparece como um único trace até o libvirt
- Logs estruturados JSON com `request_id` end-to-end

**Status de validação (2026-10-02):** `[x]`

Compute → node-agent e compute → operation usam gRPC, os host-agents de rede/volume expõem contratos gRPC, o volume-service usa cliente gRPC, há propagação de `request_id`, tracing gRPC e endpoints Prometheus nos agents.

Correções concluídas:

- [x] Compute → node-agent usando gRPC/Protobuf.
- [x] Compute/API → operation usando gRPC/Protobuf.
- [x] `request_id` propagado no cliente gRPC do node-agent.
- [x] Métricas gRPC do node-agent para requisições, duração e erros.

---

## Fase 2 — NATS JetStream + subjects versionados `[x]`

Objetivo: comunicação assíncrona, durável, com replay e dedup persistente.

- [ ] Subir NATS JetStream no `docker-compose.dev.yml`
- [ ] Criar biblioteca `libs/go/kit/natsx`:
  - Wrapper de conexão com retry + backoff
  - Publisher com confirmação de JetStream
  - Subscriber com queue group + ack/nack
  - Publisher injeta `Nats-Msg-Id: <command-id>` em toda mensagem
- [ ] Criar biblioteca `libs/rust/orion-host-kit/src/nats.rs` (ou similar)
- [ ] Definir convenção de subjects:
  - Commands: `orion.command.<domínio>.<recurso>.<ação>.v1`
    - `orion.command.compute.instance.create.v1`
    - `orion.command.compute.instance.delete.v1`
    - `orion.command.network.port.allocate.v1`
    - `orion.command.volume.create.v1`
  - Events: `orion.event.<domínio>.<recurso>.<verbo-passado>.v1`
    - `orion.event.compute.instance.created.v1`
    - `orion.event.compute.instance.failed.v1`
    - `orion.event.compute.instance.deleted.v1`
    - `orion.event.placement.allocated.v1`
- [x] **Streams de base e snapshot desired:**
  - `ORION_COMMANDS` (retenção 7d, keyspace por operation_id)
  - `ORION_EVENTS` (retenção 30d, keyspace por resource_id)
- [x] `ORION_DESIRED_STATE` (retenção 30d, snapshots por host/domínio)
- [x] **Idempotência persistente, não em memória:**
  - Tabela `processed_commands`:
    ```sql
    CREATE TABLE processed_commands (
      message_id    UUID PRIMARY KEY,
      operation_id  UUID,
      handler       TEXT,
      processed_at  TIMESTAMPTZ,
      result        JSONB
    );
    ```
  - Consumer consulta antes de processar
  - TTL configurável (ex: 7d) com cleanup job

**Critério de aceite:** um command `create instance` reproduzido 2x não cria 2 instâncias, mesmo após restart do consumer.

**Status de validação (2026-10-02):** `[x]`

Streams, subjects versionados, confirmação JetStream, desired state e deduplicação persistente estão implementados. O serviço de placement foi iniciado contra PostgreSQL e NATS JetStream temporários; migrations, streams e consumers foram criados com sucesso.

Correção aplicada: `ORION_DESIRED_STATE` está formalizado como stream de snapshots de estado desejado, separado semanticamente dos streams de commands/events; a idempotência persistida agora usa o schema do serviço (`orion_network`, `orion_volume`, `orion_placement`).

Correções concluídas:

- [x] Subjects versionados e publicação com confirmação JetStream.
- [x] Deduplicação persistente por serviço/schema.
- [x] Stream de desired state documentado como snapshot operacional.

---

## Fase 3 — Serviço `orion-operation` `[x]`

Objetivo: máquina de estados explícita para operações distribuídas. **API pública continua REST sobre o recurso** (`/v1/servers`), não sobre `/v1/operations`.

- [ ] Criar `services/orion-operation/`
- [ ] Tabelas próprias (schema `orion_operation`):
  - `operations` — snapshot atual
  - `operation_events` — histórico append-only

- [ ] Modelo `operations` (snapshot atual, mutável):
  ```sql
  CREATE TABLE operations (
    id             UUID PRIMARY KEY,
    resource_type  TEXT,           -- 'server', 'volume', 'port', 'image'
    resource_id    UUID,
    project_id     UUID,
    request_id     UUID,
    operation_type TEXT,           -- 'server.create', 'volume.attach', ...
    state          TEXT,           -- estado externo: PENDING|RUNNING|SUCCEEDED|FAILED|CANCELLED|TIMED_OUT
    current_step   TEXT,           -- step interno atual
    attempt        INT,
    created_at     TIMESTAMPTZ,
    started_at     TIMESTAMPTZ,
    finished_at    TIMESTAMPTZ,
    error_code     TEXT,
    error_message  TEXT
  );
  ```
- [ ] Modelo `operation_events` (histórico append-only, nunca atualizado):
  ```sql
  CREATE TABLE operation_events (
    id           UUID PRIMARY KEY,
    operation_id UUID,
    sequence     BIGINT,           -- monotonic, sequence por operation_id
    event_type   TEXT,             -- 'STATE_TRANSITION', 'COMMAND_SENT', 'EVENT_RECEIVED', 'STEP_STARTED', 'STEP_FINISHED'
    from_state   TEXT,
    to_state     TEXT,
    step         TEXT,
    payload      JSONB,
    created_at   TIMESTAMPTZ
  );
  ```
- [ ] State machine:
  ```
  PENDING
     ↓
  VALIDATING       (identity, quota, flavor)
     ↓
  SCHEDULING       (placement)
     ↓
  ALLOCATING_NETWORK  (orion-network)
     ↓
  PREPARING_STORAGE   (orion-volume)
     ↓
  SPAWNING            (orion-node-agent via NATS command)
     ↓
  VERIFYING            (espera observed state = desired state)
     ↓
  SUCCEEDED
  ```
  Estados terminais alternativos em qualquer estágio:
  - `FAILED`
  - `CANCELLED`
  - `TIMED_OUT`
- [ ] `VERIFYING` é necessário: receber `instance.created` não basta — o agent precisa confirmar `desired=RUNNING, observed=RUNNING`. Isso casa com M5.
- [ ] Workers que consomem `orion.event.*` e avançam o step
- [ ] Cada `orion-compute` API deixa de orquestrar e apenas cria `Operation`
- [ ] **API pública mantém recurso como contrato externo:**
  ```
  POST /v1/servers
  → 202 Accepted
  → {
       "server_id":    "srv-...",
       "operation_id": "op-...",
       "status":       "BUILD"
     }

  GET /v1/servers/{server_id}
  GET /v1/operations/{operation_id}
  ```
  O cliente nunca chama `/v1/operations` para criar; ele cria o recurso e recebe o `operation_id` na resposta. `GET /v1/operations/{id}` é para inspeção/diagnóstico.
- [ ] Métricas: `operation_duration_seconds{type,state}`, `operation_step_seconds{type,step}`

**Critério de aceite:**
- `POST /v1/servers` retorna `202` com `server_id` + `operation_id`
- `GET /v1/operations/{id}` mostra timeline completa via `operation_events`
- Falha em qualquer step antes de VERIFYING transita para `FAILED` com reason
- Após M5, `VERIFYING` só fecha quando observed state == desired state

**Status de validação (2026-10-02):** `[x]`

O serviço de operações, state machine, eventos append-only e fluxo assíncrono interno estão ativos. A API pública retorna `202`, expõe `/v1/operations/{id}`, consulta o serviço via gRPC e entrega a timeline no contrato Protobuf. O fluxo publica command durável e desired state com `operation_id` estável por request.

Correções concluídas:

- [x] `POST /v1/servers` retorna `202` com `server_id` e `operation_id`.
- [x] `GET /v1/operations/{id}` expõe snapshot e timeline.
- [x] API consulta `orion-operation` por gRPC.
- [x] Criação assíncrona deduplicada por `request_id`.

---

## Fase 4 — Placement como inventário real `[x]`

Objetivo: sair do scheduler tosco e habilitar affinity, NUMA, GPU, etc.

- [ ] Modelo `Node`:
  ```
  CPU
    total, reserved, allocated
  RAM
    total, reserved, allocated
  DISK
    total, reserved, allocated
  Traits
    KVM, SRIOV, GPU, NUMA, HUGEPAGES,
    CEPH, CPU_VENDOR_AMD, CPU_VENDOR_INTEL
  Topology
    datacenter, availability_zone, rack, host_aggregate
  ```
- [ ] API:
  - `POST /v1/allocations` (consulta, não reserva ainda)
  - `POST /v1/reservations` (aloca de fato)
  - `DELETE /v1/reservations/{id}`
- [ ] Algoritmo de scoring com pesos configuráveis:
  - Bin-packing vs. spread
  - Filtros por trait
  - Filtros por topology
- [ ] Suporte a:
  - [ ] affinity / anti-affinity
  - [ ] availability zones
  - [ ] host aggregates
  - [ ] overcommit (configurável por host)
- [ ] Futuro (deixar modelado, não implementar agora):
  - NUMA awareness
  - CPU pinning
  - Hugepages
  - GPU passthrough
  - SR-IOV
  - Maintenance/drain
  - Live migration
- [ ] Heartbeat do node-agent atualiza inventário a cada N segundos
- [ ] Reservas expiram por TTL (lock distribuído)

**Critério de aceite:** scheduler retorna nó válido respeitando trait + availability zone + anti-affinity.

**Status de validação (2026-10-02):** `[x]`

O modelo contém inventário, traits, NUMA/GPU, AZ, topology, affinity/anti-affinity, scoring bin-packing/spread e reservas com TTL. O store reserva/libera capacidade sob lock transacional, retorna conflito para capacidade insuficiente e usa IDs textuais coerentes. Pesos são configuráveis por ambiente e heartbeat gRPC atualiza inventário preservando drain/disable.

Correções concluídas:

- [x] SQL de consulta de hosts corrigido.
- [x] Reservas e liberações protegidas por transação e lock.
- [x] Capacidade insuficiente retorna conflito explícito.
- [x] IDs de reservas alinhados como `TEXT`.

---

## Fase 5 — Reconciliação nos agents `[x]`

Objetivo: agents são "kubelet-like". Loop desired/observed tolerante a falhas.

**Princípio estrutural:** o que muda em M5 é a transição de orquestração imperativa para control loop.

```
HOJE                              ALVO (após M5)
─────                             ──────────────

API                               API
 ↓                                 ↓
compute                           Operation
 ↓                                 ↓
agent                             Desired State
 ↓                                 ↓
libvirt                           Command/Event Bus (NATS)
                                   ↓
                                   Agent Reconciler
                                   ↓
                                   libvirt / OVS / LVM
                                   │
                                   ▼
                                   Observed State
                                   │
                                   └────────► Control Plane
```

- [ ] Loop de reconciliação em `orion-node-agent`:
  - Lê desired state do control plane (snapshot local + atualizações via NATS)
  - Compara com estado observado (libvirt `Domain`, `StoragePool`, etc.)
  - Cria/atualiza/remove conforme necessário
  - Reporta observed state periodicamente
- [ ] Idempotência: cada reconcile é seguro de repetir
- [ ] Retry exponencial com jitter
- [ ] Circuit breaker para erros transitórios do libvirt
- [ ] Heartbeat periódico via **gRPC** (`Heartbeat`) com `NodeResources` e `NodeTraits`
- [ ] Self-registration no boot via **gRPC** (`RegisterNode`)
- [ ] Graceful shutdown: finaliza reconcile em curso, libera locks
- [ ] Health/readiness separados:
  - `/healthz` → processo vivo
  - `/readyz` → libvirt conectado + última reconcile < threshold
- [ ] `orion-network-host-agent` — escopo **host-local apenas**:
  - Valida readiness de OVS/OVN
  - Garante bridge mappings
  - Publica capabilities/inventory
  - Reconcilia configuração host-local
  - **NÃO** implementa logical switching — isso fica a cargo do `ovn-controller`
- [ ] `orion-volume-host-agent`: reconcilia volumes anexados (LVM agora, drivers em M7)

**Critério de aceite:**
- Matar o agent (`kill -9`) e reiniciar converge sem intervenção humana
- `/readyz` vira `false` se libvirt cair
- Operation em `VERIFYING` só fecha quando observed == desired

**Status de validação (2026-10-02):** `[x]`

O node-agent e os host-agents possuem desired state via JetStream, observação local, loop de reconcile, dedup persistente, backoff com jitter, circuit breaker, heartbeat/self-registration gRPC, readiness e shutdown gracioso. A reconciliação propaga falhas e os builds são idempotentes para recursos existentes; volume e rede consomem desired state/eventos.

Correções concluídas:

- [x] Falhas de ações deixam de ser registradas como reconcile bem-sucedido.
- [x] Backoff com jitter implementado.
- [x] Build de domínio/disco existente tornou-se idempotente.
- [x] `/readyz` diferencia libvirt indisponível e inicialização.
- [x] Métricas gRPC incluem duração e erros.

---

## Fase 6 — Ownership de dados por serviço `[x]`

Objetivo: cada microserviço é dono do seu schema PostgreSQL.

- [ ] Schema por serviço no mesmo cluster:
  - `orion_identity` → users, projects, roles
  - `orion_compute` → instances, flavors
  - `orion_placement` → inventories, allocations, traits
  - `orion_network` → networks, subnets, routers, ports, floating_ips
  - `orion_volume` → volumes, snapshots, attachments
  - `orion_image` → images, image_metadata
  - `orion_operation` → operations, operation_steps, operation_events
- [ ] Proibir JOIN cross-schema — integrar via gRPC
- [ ] Migrations versionadas em `services/<svc>/migrations/`
- [ ] Cada serviço só lê seu schema, exceto jobs administrativos
- [x] Auditoria: tabela `audit_log` no schema `orion_identity` (ou schema próprio `orion_audit`)

**Critério de aceite:** `grep -r "JOIN.*FROM orion_"` no repo retorna zero.

**Status de validação (2026-10-02):** `[x]`

O runner cria schemas por serviço, as migrations de compatibilidade movem tabelas legadas para o schema dono, os stores usam tabelas qualificadas, a idempotência também é segregada por schema e não há JOIN entre schemas de serviços. A tabela `orion_identity.audit_log` foi adicionada. A migration preserva upgrades de bancos existentes sem apagar tabelas. A execução das migrations em PostgreSQL temporário confirmou as tabelas nos schemas esperados e os IDs textuais das reservas.

Correções concluídas:

- [x] Tabelas legadas movidas para os schemas donos.
- [x] Migrations de movimentação corrigidas para banco novo e upgrade.
- [x] `processed_commands` isolada por serviço.
- [x] `orion_identity.audit_log` criada.

---

## Fase 7 — Drivers plugáveis (volume e rede) [x]

Objetivo: não amarrar a Ceph nem a OVN. **A abstração principal de driver vive no host agent (Rust), não no control plane (Go).**

**Por que no agent:** o que executa `lvcreate` é o agent Rust hoje. O control plane Go cuida de **política e catálogo** (`backend_type`, `availability_zone`, `capabilities`, `desired_state`); o agent cuida de **mecanismo**.

- [x] Trait Rust em `agents/orion-volume-host-agent/src/backend/mod.rs`:
  ```rust
  #[async_trait]
  pub trait VolumeBackend {
      fn name(&self) -> &'static str;

      async fn create(
          &self,
          spec: &VolumeSpec,
      ) -> Result<VolumeHandle, VolumeError>;

      async fn delete(
          &self,
          id: &str,
      ) -> Result<(), VolumeError>;

      async fn attach(
          &self,
          volume: &VolumeHandle,
          node: &NodeRef,
      ) -> Result<DevicePath, VolumeError>;

      async fn detach(
          &self,
          volume: &VolumeHandle,
          node: &NodeRef,
      ) -> Result<(), VolumeError>;
  }
  ```
- [x] Estrutura do agent:
  ```
  agents/orion-volume-host-agent/src/
  ├── backend/
  │   ├── mod.rs
  │   ├── lvm.rs
  │   ├── ceph.rs
  │   └── mock.rs
  ├── reconcile.rs
  ├── nats.rs
  └── main.rs
  ```
- [ ] Implementações:
  - [x] LVM (refatorado atrás do trait)
  - [x] Ceph RBD (via CLI `rbd`, selecionável por configuração)
  - [ ] iSCSI (prioridade média; driver reservado)
  - [ ] NVMe-oF (prioridade média; driver reservado)
- [x] `orion-volume` (Go) fica responsável por:
  - Catálogo: `volume(id, backend_type, availability_zone, capabilities, desired_state)`
  - Política: roteamento de backend por AZ, quota, capability matching
  - Não fala direto com LVM/RBD — publica desired state no NATS
- [x] Mesmo princípio para rede: agent cuida de host-local, OVN control plane cuida do logical switching distribuído:
  ```
  orion-network (Go)
       │ desired logical topology
       ▼
  OVN Northbound
       ▼
  OVN Southbound
       ▼
  ovn-controller
       ▼
  OVS (host-local, gerenciado parcialmente pelo agent)
  ```

**Critério de aceite:** trocar backend (LVM → Ceph) sem alterar `orion-volume`.

---

## Fase 8 — Observabilidade (padronização e fechamento) [x]

> Observabilidade **mínima nasce em M1** (request_id, trace_id, operation_id, structured logs, gRPC metrics). Esta fase é **padronização** e fechamento, não implantação do zero. transversal

- [x] OpenTelemetry tracing em todos os serviços Go (otlptracehttp)
- [x] Trace context propagado gRPC + NATS headers
- [x] Prometheus metrics padronizadas:
  - `orion_http_request_duration_seconds`
  - `orion_grpc_request_duration_seconds`
  - `orion_nats_publish_total`, `orion_nats_consume_total`
  - `orion_operation_duration_seconds{type,state}`
  - `orion_node_reconcile_total{result}`
  - `orion_libvirt_error_total{op}`
- [x] Logs estruturados JSON com `request_id`, `operation_id`, `project_id`
- [x] `/metrics` exposto em todo serviço
- [x] Dashboard Grafana inicial em `deploy/grafana/`

**Critério de aceite:** um request REST mostra trace completo até o libvirt.

---

## Fase 9 — Hardening [~]

- [x] Graceful shutdown em todo serviço (signal handler, drain NATS, fechar gRPC)
- [x] Health/readiness distintos
- [x] Rate limiting na API externa (per project)
- [x] AuthN/AuthZ com `orion-identity` (token bearer)
- [x] Quotas por projeto (instances, vcpus, ram, volumes, networks)
- [x] Retry com timeout em todos os adapters HTTP/RPC (kit comum com backoff/jitter)
- [x] Auditoria: eventos de mutação gravados (consumer `orion.event.>` + tabela append-only)
- [~] Testes:
  - Unit por domínio (regras de negócio)
  - Integration com testcontainers (postgres + nats)
  - E2E com 2 VMs em `docs/two-vm-install.md` (já existe)

**Critério de aceite:** `make test` roda suite completa em CI em < 10 min.

**Implementado nesta rodada:** quotas transacionais por projeto em compute/volume/network, finalizers de server/volume/network/port, delete de network com liberação de quota, `Idempotency-Key` durável via request/task id, auditoria append-only consumindo eventos, eleição por advisory lock no outbox, UUIDv7, subject catalog, provider de secrets, retry com backoff/jitter nos adapters HTTP/RPC e configuração mTLS opcional para gRPC.

**Pendente para fechar M9 (`[~]`):** chaos/E2E automatizado de duas VMs; a suíte E2E existente continua separada de `make test` por depender de libvirt/OVN instalados no ambiente de execução.

---

## Sequenciamento sugerido (revisado)

```
M0 Contracts
   ↓
M1 gRPC baseline + observability mínima
   ↓
M2 JetStream foundation (ORION_COMMANDS + ORION_EVENTS)
   ↓
M3 Operations
   ├──────────────┐
   ▼              ▼
M4 Placement   M5 Reconcile    ← paralelizam
   │              │
   └──────┬───────┘
          ▼
M6 Data ownership
          ↓
M7 Drivers (abstração no agent Rust)
          ↓
M8 Observability hardening (padronização)
          ↓
M9 Hardening
```

| Fase | Título | Bloqueio |
|---|---|---|
| 0 | Contratos Protobuf | nenhuma |
| 1 | HTTP → gRPC + observability base | bloqueada por 0 |
| 2 | NATS JetStream (2 streams) | independente (paralelo a 1) |
| 3 | `orion-operation` | bloqueada por 1 e 2 |
| 4 | Placement real | bloqueada por 3 |
| 5 | Reconciliação | bloqueada por 1 e 2 (paralelo a 4) |
| 6 | Ownership de dados | bloqueada por 3 |
| 7 | Drivers plugáveis (no agent) | bloqueada por 5 |
| 8 | Observability hardening | transversal |
| 9 | Hardening | transversal, ao final |

### Três saltos arquiteturais

```
HOJE
HTTP imperative orchestration

        ↓ M0-M3

CONTROL PLANE DISTRIBUÍDO (v0.4.0)
gRPC + commands/events + persistent operations

        ↓ M4-M5

CONTROL LOOP (v0.6.0)
desired state + observed state + reconciliation

        ↓ M6-M9

PLATAFORMA (v1.0.0)
ownership + drivers + observability + hardening
```

### Marcos de release (revisado)

| Tag | Inclui | Significado arquitetural |
|---|---|---|
| `v0.2.0` | M0 + M1 | gRPC + contratos, ainda imperativo |
| `v0.3.0` | M2 | JetStream + idempotência persistente |
| **`v0.4.0`** | **M3** | **Primeiro marco arquitetural real**: API → Operation → JetStream → Agent → libvirt, com replay/idempotência |
| `v0.5.0` | M4 + M5 | Control loop com placement rico |
| `v0.6.0` | M6 | Ownership de dados por serviço |
| `v0.7.0` | M7 | Drivers plugáveis no agent |
| `v0.8.0` | M8 | Observabilidade padronizada |
| `v1.0.0` | M9 + 2-VM E2E + auditoria | Plataforma pronta |

---

## E2E permanente baseado em `two-vm-install.md`

O fluxo documentado em `docs/two-vm-install.md` vira **teste E2E automatizado obrigatório** a partir de M3:

```
criar projeto
   ↓
criar network
   ↓
criar 2 volumes
   ↓
criar VM-A
   ↓
criar VM-B
   ↓
validar placement
   ↓
validar conectividade
   ↓
restart agent
   ↓
validar convergence
   ↓
destruir VMs
   ↓
validar cleanup
```

Esse teste é a **definição de saúde arquitetural** durante toda a migração. Cada milestone deve preservar este cenário passando.

---

## Anti-objetivos (não fazer)

- ❌ Reescrever `orion-node-agent` em outra linguagem
- ❌ Tornar `orion-compute` cliente de libvirt
- ❌ Compartilhar tabelas entre serviços
- ❌ Replicar regras de negócio Go ↔ Rust
- ❌ Copiar OpenStack inteiro
- ❌ Suportar multi-cloud / federated
- ❌ Adicionar Kubernetes / container runtime (escopo: IaaS VM puro)
- ❌ Mudar API pública REST para `/v1/operations` — recurso continua sendo `server`, `volume`, etc.
- ❌ Dedup em memória — usar `processed_commands` no Postgres
- ❌ Criar 5 streams NATS upfront — começar com 2 (`ORION_COMMANDS`, `ORION_EVENTS`)
- ❌ Substituir `ovn-controller` por código próprio no agent

---

## Métrica de sucesso

O Orion deixa de ser "frontend moderno para libvirt" quando:

1. Um agent pode cair e voltar sozinho convergindo o estado
2. Operações de criar VM passam por state machine rastreável via `operation_events`
3. Scheduler considera traits + topology + availability zone
4. Storage e rede têm drivers intercambiáveis sem alterar control plane
5. Tracing mostra uma request da CLI até o libvirt em um único trace
6. Reinício do control plane não causa inconsistência (idempotência + desired state)
7. Deduplicação de messages sobrevive a replay do JetStream
8. **E2E two-vm-install passa após cada milestone**

---

# M9 — Platform Hardening

Objetivo: Orion tolera **falhas parciais, restart, duplicidade, concorrência e upgrade** sem corromper estado. É o que separa um MVP distribuído de um control plane de produção.

> Algumas peças de M9 (Outbox, Inbox, finalizers, optimistic locking) devem começar a ser introduzidas **durante M2/M3**, não esperar M9.

## M9.1 — Transactional Outbox + Inbox

Resolve o problema clássico:

```
BEGIN
  UPDATE operation ...
  -- 💥 processo morre aqui
COMMIT
-- NATS Publish nunca acontece
```

ou o inverso:

```
NATS Publish
  mensagem enviada
  -- 💥 processo morre
COMMIT
-- DB rollback
```

Resultado: `DB != Event Bus`.

Solução:

```sql
CREATE TABLE outbox_events (
    id              UUID PRIMARY KEY,
    aggregate_type  TEXT NOT NULL,
    aggregate_id    UUID NOT NULL,
    subject         TEXT NOT NULL,
    payload         JSONB NOT NULL,
    trace_id        TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at    TIMESTAMPTZ,
    attempts        INT NOT NULL DEFAULT 0
);
```

Na mesma transação:

```
BEGIN
  UPDATE operation ...
  INSERT outbox_events ...
COMMIT

Outbox Worker
  │
  ▼
NATS JetStream
  │
  ▼
published_at
```

O `processed_commands` (de M2) vira o **Inbox Pattern** no consumer.

Resultado:

```
Outbox + JetStream + Inbox + idempotência de recurso
```

## M9.2 — Saga + compensação

O fluxo atual de M3 não lida com falha parcial:

```
Network criado ✓
Volume criado ✓
VM creation ✗
```

State machine ganha:

```
PENDING
VALIDATING
SCHEDULING
ALLOCATING_NETWORK
PREPARING_STORAGE
SPAWNING
VERIFYING
SUCCEEDED

FAILED
CANCELLED
TIMED_OUT

COMPENSATING
COMPENSATED
```

Compensação (não necessariamente exposta na API externa):

```
SPAWNING falhou
  DELETE volume
  RELEASE network
  RELEASE placement reservation
  FAILED
```

## M9.3 — Desired State como modelo formal

```
instance
├── desired_state
├── observed_state
├── generation
└── observed_generation
```

Exemplo:

```
desired_state       RUNNING
observed_state      STOPPED
generation          12
observed_generation 11
```

Reconciler sabe imediatamente que há trabalho. Aplica-se a:

```
Instance
Volume
Port
Network
Reservation
```

## M9.4 — Resource Version / Optimistic Locking

```sql
UPDATE instances
SET
    desired_state = $1,
    version = version + 1
WHERE
    id = $2
AND
    version = $3;
```

Se afetar `0 rows` → `OptimisticLockConflict`. Evita worker A sobrescrever worker B.

## M9.5 — Fencing Token

Lock com TTL sozinho não basta:

```
Scheduler A recebe lock
  congelado
  TTL expira
Scheduler B pega lock
Scheduler A volta
```

Dois schedulers achando que são donos. Use **fencing tokens**:

```
reservation:
    token = 581

próxima:
    token = 582

comando com token 581 → rejeitado (581 < 582)
```

Crítico para: placement, volume attach, network allocation, instance ownership.

## M9.6 — Contrato padrão de erro

Erro comum em `common/v1/common.proto`:

```protobuf
message Error {
  string code = 1;
  string message = 2;
  string request_id = 3;
  string operation_id = 4;
  bool retryable = 5;
  map<string, string> metadata = 6;
}
```

Códigos estáveis:

```
ORION_NOT_FOUND
ORION_ALREADY_EXISTS
ORION_CONFLICT
ORION_INVALID_ARGUMENT
ORION_UNAVAILABLE
ORION_TIMEOUT
ORION_QUOTA_EXCEEDED
ORION_CAPACITY_EXHAUSTED
ORION_BACKEND_FAILURE
ORION_OPTIMISTIC_LOCK_CONFLICT
ORION_FENCING_TOKEN_REJECTED
ORION_COMPENSATION_FAILED
```

## M9.7 — Traits vs Capabilities

Separar:

**Traits** (descrição estática):

```
CPU_VENDOR_AMD
GPU_NVIDIA
STORAGE_CEPH
AZ_SAO_PAULO_1
```

**Capabilities** (o que o nó/agent suporta):

```
supports_live_migration
supports_volume_snapshot
supports_sriov
supports_secure_boot
supports_hugepages
```

Request futura:

```yaml
required_traits:
  - GPU_NVIDIA

required_capabilities:
  - supports_live_migration
```

## M9.8 — Agent capability negotiation

`RegisterNode` transmite:

```json
{
  "agent_version": "0.7.1",
  "protocol_version": "v1",
  "features": ["compute.snapshot", "compute.live_migration"],
  "drivers": ["libvirt", "lvm"]
}
```

Control plane nunca envia comando incompatível durante rolling upgrade.

## M9.9 — API idempotency key

```http
POST /v1/servers
Idempotency-Key: 2a7bc...
```

Tabela:

```
api_idempotency_keys
├── project_id
├── key
├── request_hash
├── operation_id
├── resource_id
└── expires_at
```

Essencial para Terraform/OpenTofu e automações.

## M9.10 — Quotas consistentes

Não usar `SELECT COUNT(*)` ad-hoc. Modelo explícito:

```
project_quotas

instances
vcpus
ram_mb
volumes
volume_gb
ports
floating_ips
snapshots
```

Com:

```
limit
used
reserved
```

Placement reserva: `reserved += N`. Quando VM nasce: `reserved -= N; used += N`. Evita oversubscription por concorrência.

## M9.11 — Dead Letter Queue

Nunca retry infinito. Definir:

```
max_attempts
retry_policy
DLQ
```

Exemplo:

```
orion.command.compute.instance.create.v1
após 10 falhas
→ orion.dlq.compute.instance.create.v1
```

Persistindo: `message_id`, `operation_id`, `error`, `attempts`, `first_seen_at`, `last_seen_at`, `payload`.

## M9.12 — Retry policy única

Centralizar em kit:

```
attempt 1 → 1s
attempt 2 → 2s
attempt 3 → 4s
attempt 4 → 8s
attempt 5 → 16s
```

Com exponential backoff + jitter + max delay + max attempts.

Erros classificados:

```
network timeout   → retry
invalid size      → não retry
capacity exhausted → retry/re-schedule
```

## M9.13 — Timeout / deadline propagation

```
API request
  → request deadline
    → Operation
      → NATS metadata
        → gRPC
          → agent
            → driver
```

Exemplo:

```
Operation timeout: 10 min
├── placement:    15s
├── network:      30s
├── volume:      120s
├── spawn:       180s
└── verify:      300s
```

Cada camada não decide arbitrariamente.

## M9.14 — Configuração padronizada

Prioridade:

```
defaults
   ↓
config file
   ↓
environment
   ↓
CLI flags
```

Validação no startup (fail-fast). Nunca subir com `NATS URL vazio`, `DB inválido`, `timeout negativo`, `backend desconhecido`.

## M9.15 — Secrets separados de config

`SecretProvider` (interface). Inicialmente:

```
EnvSecretProvider
FileSecretProvider
```

Futuramente:

```
Vault
KMS
Kubernetes Secret
```

Domínio recebe **secret references**, não secrets persistidos:

```
volume:
    encryption_key_ref: secret://01K...
```

## M9.16 — mTLS entre serviços

Após M1, formalizar:

```
API → services
services → services
services → agents
```

Com TLS, client certificate, service identity, certificate rotation. Especialmente crítico: `control plane → node-agent`.

## M9.17 — RBAC + policy engine

Separar Authentication de Authorization:

```
authentication → identity
authorization  → policy
```

Ações:

```
server:create
server:delete
volume:create
network:admin
project:admin
```

Policy contextual futura:

```
project == resource.project
region  == allowed_region
role contains compute_admin
```

OPA/Rego como opção coerente.

## M9.18 — Audit realmente imutável

Cada evento contém:

```
actor
project_id
resource_type
resource_id
operation
request_id
operation_id
source_ip
timestamp
before
after
result
```

Audit consome `orion.event.>` e **não bloqueia** operações.

## M9.19 — Image como domínio formal

Buraco relevante hoje. Modelo:

```
images
├── id
├── project_id
├── name
├── format
├── size_bytes
├── checksum
├── storage_backend
├── storage_key
├── state
└── created_at
```

Estados:

```
QUEUED
UPLOADING
VERIFYING
ACTIVE
FAILED
DELETED
```

Backends:

```
local filesystem
S3/MinIO
Ceph RBD
Ceph RGW
```

Pipeline:

```
UPLOAD → VERIFY → HASH → SCAN → STORE → ACTIVE
```

## M9.20 — Network model mais completo

Modelar formalmente mesmo antes de OVN completo:

```
Network
Subnet
Port
Router
FloatingIP
SecurityGroup
SecurityGroupRule
```

**Port como entidade central** da conexão:

```
VM
 │
 ▼
Port
 │
 ▼
Logical Switch
```

Não associar `VM → network_id` direto (limita multi-NIC, SG, fixed IPs).

## M9.21 — Storage attachment como recurso

Evitar `volume.instance_id` direto. Criar:

```
volume_attachments
├── id
├── volume_id
├── instance_id
├── node_id
├── device
├── state
└── generation
```

Prepara: multi-attach, reattach, migration, failure recovery.

## M9.22 — Soft delete + finalizers

`DELETE server` não apaga imediatamente:

```
deletion_timestamp = now()
finalizers:
  network.cleanup
  volume.detach
  compute.destroy
  placement.release

quando todos terminarem:
  DELETE físico
```

Evita resource leak.

## M9.23 — Health vs Readiness padronizado

Todos serviços expõem:

```
/healthz   processo vivo
/readyz    DB conectado, NATS conectado, deps OK
/metrics   Prometheus
```

Agent:

```
libvirt conectado
reconcile recente
NATS conectado
```

## M9.24 — Leader election

Workers que **não** devem rodar simultaneamente:

```
outbox dispatcher
cleanup worker
reservation expiry worker
reconciler global
```

Inicialmente: **PostgreSQL advisory locks**. Não precisa etcd/Kubernetes Lease agora.

## M9.25 — IDs e relógio padronizados

```
UUIDv7  para IDs novos
        ordenável temporalmente
        bom para índices
        globally unique

UTC em toda persistência
RFC3339Nano nos contratos
UI converte timezone
```

## M9.26 — Subject catalog formal

`docs/nats-subjects.md` ou código/proto gerado:

```
orion.<kind>.<domain>.<resource>.<action>.v1
```

Exemplos:

```
orion.command.compute.instance.create.v1
orion.command.compute.instance.destroy.v1
orion.event.compute.instance.created.v1
orion.event.compute.instance.failed.v1
orion.event.node.heartbeat.v1
```

Proibir strings espalhadas. Usar constantes geradas/kit.

## M9.27 — Compatibility policy

Definir agora:

```
Proto fields   nunca reutilizam tag
               removed → reserved
               novos   → backward compatible
               breaking → v2

NATS           ...create.v1 / ...create.v2

API            /v1/

DB             forward-only migrations
```

Evita sofrimento no upgrade.

## M9.28 — Upgrade strategy

Antes de v1, durante rolling upgrade:

```
control plane N
agent         N-1
```

Devem coexistir. Por isso capability negotiation, protobuf backward compat e NATS subject versioning são fundamentais.

## M9.29 — Chaos / failure testing

E2E não testa só happy path:

```
kill compute
kill operation
kill agent
restart NATS
restart PostgreSQL
duplicate command
delay command
drop response
timeout libvirt
agent reconnect
```

Requisitos:

```
nenhum recurso duplicado
nenhuma operation travada permanentemente
eventual convergence
```

## M9.30 — SLOs

Definir antes de v1:

```
API availability            99.9%
POST /servers → 202         p95 < 500ms
placement                   p95 < 250ms
operation create VM         p95 < X min
agent heartbeat freshness   < 90s
reconcile convergence       p95 < 60s
```

Números podem mudar. Contrato operacional deve existir.

---

## Os cinco itens obrigatórios de M9

Se for preciso priorizar:

1. **Transactional Outbox/Inbox** (consistência DB ↔ event bus)
2. **Saga + compensação** (falha parcial em operações distribuídas)
3. **desired/observed + generation** (reconciliation robusta)
4. **optimistic locking + fencing** (concorrência segura)
5. **finalizers para cleanup** (zero resource leak)

Esses cinco resolvem:

```
mensagem duplicada
processo morrendo no meio
duas operações concorrentes
agent atrasado
cleanup parcial
recurso órfão
```

---

# M10–M19 — Catálogo completo de serviços Orion

Após M0–M9, Orion entra em uma segunda etapa: sair de IaaS VM/rede/bloco e virar **cloud platform completa**. Inspirado nos domínios do OpenStack, mas com arquitetura interna própria.

## Catálogo de 20 serviços Orion

| Orion | Equivalente OpenStack | Responsabilidade |
|---|---|---|
| `orion-api` | — | REST gateway, BFF |
| `orion-identity` | Keystone | users, projects, roles, auth |
| `orion-secret` | Barbican | secrets, keys, certificados |
| `orion-policy` | (OPA/Rego) | autorização contextual |
| `orion-compute` | Nova | VMs e lifecycle |
| `orion-placement` | Placement | inventário e scheduling |
| `orion-network` | Neutron | redes, subnets, ports, routers, SG |
| `orion-volume` | Cinder | block storage |
| `orion-image` | Glance | catálogo e armazenamento de imagens |
| `orion-share` | Manila | filesystem compartilhado |
| `orion-object` | Swift | object storage (control plane) |
| `orion-dns` | Designate | DNSaaS |
| `orion-loadbalancer` | Octavia | LBaaS |
| `orion-baremetal` | Ironic | provisioning físico |
| `orion-orchestration` | Heat | stacks/templates |
| `orion-kubernetes` | Magnum | clusters Kubernetes |
| `orion-database` | Trove | DBaaS |
| `orion-telemetry` | Ceilometer | coleta de métricas/events |
| `orion-alarm` | Aodh | alarmes |
| `orion-ha` | Masakari | HA/recovery de instâncias |
| `orion-optimizer` | Watcher | otimização/rebalanceamento |
| `orion-operation` | transversal | workflows/Sagas |
| `orion-console` | Horizon/Skyline | dashboard web |

---

## As quatro eras do Orion

```
M0 ─ M3        FOUNDATION          v0.4.0
                protobuf, gRPC, NATS, operations

M4 ─ M9        DISTRIBUTED IaaS    v1.0
                placement, reconciliation, ownership,
                drivers, observability, hardening

M10 ─ M14      CLOUD PLATFORM      v2.0
                secrets, OVN, image completo, DNS,
                load balancer, object, shared storage,
                bare metal, orchestration

M15 ─ M19      ADVANCED CLOUD      v3.0
                telemetry, alarms, HA, Kubernetes,
                DBaaS, optimization
```

### Marcos de release revisados (final)

| Tag | Inclui | Significado |
|---|---|---|
| `v0.2.0` | M0 + M1 | gRPC + contratos |
| `v0.3.0` | M2 | JetStream + idempotência |
| **`v0.4.0`** | **M3** | **Primeiro marco arquitetural real** |
| `v0.5.0` | M4 + M5 | Control loop |
| `v0.6.0` | M6 | Ownership de dados |
| `v0.7.0` | M7 | Drivers plugáveis |
| `v0.8.0` | M8 | Observability hardening |
| **`v1.0.0`** | **M9 + E2E + auditoria** | **Compute IaaS confiável** |
| `v1.5.0` | M10 | Cloud Core |
| `v2.0.0` | M11–M14 | Full IaaS Platform |
| `v2.5.0` | M15–M16 | Managed Services |
| **`v3.0.0`** | **M17–M19** | **Cloud Platform completa** |

---

## M10 — Cloud Core Completion

```
orion-secret
orion-image completo
orion-network completo/OVN
```

Entrega: `VM + image + network + security group + volume + secrets`.

## M11 — Network Services

```
orion-dns
orion-loadbalancer
```

Entrega: `VMs → LB → Floating IP → DNS`.

## M12 — Storage Services

```
orion-object
orion-share
```

Orion passa a oferecer **Block + Object + File**.

```
orion-object  → control plane sobre MinIO/Ceph RGW (NÃO reescrever Swift)
orion-share   → NFS, CephFS, SMB
```

## M13 — Bare Metal

```
orion-baremetal

PhysicalNode
BMC
Port
Disk
HardwareInventory
ProvisioningState
Firmware

Redfish
IPMI
PXE/iPXE
HTTP boot
cloud-init
```

States: `DISCOVER → INSPECT → CLEAN → DEPLOY → ACTIVE`.

## M14 — Orchestration

```
orion-orchestration

Stack
StackResource
StackOperation
StackOutput
```

DSL Orion declarativa (não Heat templates diretamente de início):

```yaml
version: v1

resources:

  web-network:
    type: network

  web-volume:
    type: volume
    properties:
      size: 40GiB

  web:
    type: server
    properties:
      flavor: c4.large
      image: ubuntu-26.04
      network:
        ref: web-network
      volumes:
        - ref: web-volume
```

Orchestration usa `orion-operation`. **Não** implementa segunda engine de workflow.

## M15 — Telemetry

```
orion-telemetry

CPU/RAM/disk/network bytes
instance uptime
volume usage
object usage
LB traffic
API consumption
```

Pipeline: `agents/services → NATS events → orion-telemetry → Prometheus + long-term`.

## M16 — High Availability

```
orion-ha

node failure detection
hypervisor failure detection
instance failure detection
fencing
evacuation
rebuild
```

Regra fundamental: **fencing antes de recovery**. Nunca permitir split-brain (`VM antiga rodando + VM recuperada em outro host`).

## M17 — Managed Kubernetes

```
orion-kubernetes

Cluster
NodePool
ClusterTemplate
KubernetesVersion
Addon
```

Orion provisiona infra e instala:

```
kubeadm
Talos
RKE2
```

como drivers. **Não** escreve Kubernetes.

## M18 — DBaaS

```
orion-database

PostgreSQL
MySQL
MariaDB
Redis/Valkey

DatabaseInstance
DatabaseCluster
DatabaseUser
DatabaseBackup
DatabaseParameterGroup
```

Cada banco roda em VM dedicada provisionada por `orion-compute` + `orion-volume`.

## M19 — Optimization

```
orion-optimizer

hosts subutilizados
hosts saturados
hotspots
NUMA
energia
distribution
```

Recomendações:

```
migrate vm-21 node-4 → node-8
```

ou operação automática via policy.

---

## Diferencial próprio do Orion

Após M19, adicionar:

```
orion-autoscaling

horizontal/vertical scaling
consome orion-telemetry + orion-alarm
separa de orchestration
```

Isso faz Orion deixar de ser "um OpenStack menor" e ganhar **arquitetura própria**.

---

# Orion Service Contract (regra estrutural)

Para chegar a 20+ serviços sem virar monólito distribuído, todo serviço Orion deve implementar:

```
Domain
Application
Ports (inbound / outbound)
Adapters (grpc / postgres / nats / http)

PostgreSQL próprio / schema próprio
Protobuf próprio (geração por buf)
gRPC server
NATS publisher/subscriber
Integração com orion-operation
Idempotência (outbox/inbox)
OpenTelemetry
Prometheus
Audit
Health/Readiness
RBAC
Config validation
Migrations versionadas
Testes (unit / integration / chaos)
```

## Template de serviço

```
tools/orion-service-template/
├── cmd/
│   └── orion-foo/
│           └── main.go
├── internal/
│   ├── domain/
│   ├── application/
│   ├── ports/
│   │   ├── inbound/
│   │   └── outbound/
│   └── adapters/
│       ├── grpc/
│       ├── postgres/
│       └── nats/
├── migrations/
├── proto/
├── tests/
└── Dockerfile
```

## Template de agent

```
agents/orion-*-agent/
├── src/
│   ├── driver/
│   ├── reconcile/
│   ├── nats/
│   ├── grpc/
│   ├── telemetry/
│   └── main.rs
```

Adicionar novo serviço = criar via template, **sem reinventar arquitetura**.

---

# Princípios invariáveis (regra de ouro)

> Novas funcionalidades devem ser **compostas usando capacidades Orion existentes**, e não furando as fronteiras dos serviços.

Isto é, nunca:

```sql
SELECT * FROM orion_network.ports;  -- em orion-loadbalancer
```

Sempre:

```
orion-loadbalancer
       │
       │ AllocatePort() (gRPC)
       ▼
orion-network
```

Mantido desde o dia 1:

- cada serviço é dono do próprio estado
- nenhum serviço acessa schema de outro
- chamadas síncronas usam gRPC
- workflows longos usam `orion-operation` + NATS
- agentes executam mecanismo, control plane decide política
- desired/observed state governa convergência
- drivers ficam atrás de interfaces
- eventos são versionados e idempotentes
- observabilidade e auditoria atravessam tudo desde o início

---

# Documentação auxiliar sugerida

```
docs/
├── architecture-roadmap.md       (este documento)
├── service-architecture.md       Orion Service Contract
├── agent-architecture.md         padrão de agent
├── messaging-contract.md         NATS subjects, outbox/inbox, idempotência
├── consistency-model.md          desired/observed, generation, fencing
├── api-guidelines.md             REST externo + versionamento
├── driver-development-guide.md   como criar driver de volume/rede
├── nats-subjects.md              catálogo de subjects
├── error-codes.md                catálogo de códigos ORION_*
└── chaos-testing.md              cenários de falha
```

---

# Estado final desejado

Quando todas as eras estiverem completas, a arquitetura Orion se apresenta como:

```
                       ORION CLOUD
                           │
              ┌────────────┴────────────┐
              │                         │
         Orion Console              Orion CLI
              │                         │
              └────────────┬────────────┘
                           ▼
                       orion-api
                           │
 ┌─────────────────────────┼───────────────────────────┐
 │                         │                           │
 ▼                         ▼                           ▼
IDENTITY                 COMPUTE                     DATA
 ├ identity                ├ compute                   ├ volume
 ├ secret                  ├ placement                 ├ image
 └ policy                  ├ baremetal                 ├ object
                           └ ha                        └ share

NETWORKING               PLATFORM                    OPERATIONS
 │                         │                           │
 ├ network                 ├ orchestration             ├ operation
 ├ dns                     ├ kubernetes                ├ telemetry
 └ loadbalancer            └ database                  ├ alarm
                                                       ├ audit
                                                       └ optimizer
```

Cada caixa é um **serviço independente** com seu schema, seus contratos, seu agent (quando aplicável), seus drivers. Compondo capacidades, não furando fronteiras.

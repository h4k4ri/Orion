# Orion Implementation Roadmap v11.1

## Visão Geral

Orion é uma plataforma de plugins extensível para infraestrutura cloud-native. O protocolo v1.0 está **congelado em v11.1**.

**Princípios fundamentais:**
- Core nunca conhece plugins concretos (sem `if provider.Type == "zfs"`)
- `ProviderContractSpec` é oficial Orion, não definido pelo plugin
- Saga stores `input/output/compensation_input` por step para rollback
- `capabilities.snapshots: true` requer `storage.snapshot/v1`

---

## Cadeia de Dependência Completa

```
                1. CONTRACTS
                     │
                     ▼
                 2. SDK GO
                     │
                     ▼
               3. MOCK PLUGIN
                     │
                     ▼
           4. CONFORMANCE TESTS
                     │
                     ▼
                5. STATE STORE
                     │
                     ▼
              6. PLUGIN REGISTRY
                     │
                     ▼
             7. PROVIDER REGISTRY
                     │
                     ▼
              8. PLUGIN EXECUTOR
                     │
                     ▼
              9. OPERATION ENGINE
                     │
                     ▼
              10. RESOURCE RUNTIME
                     │
                     ▼
              11. RESOURCE SERVICE
                     │
              ┌──────┼──────┐
              ▼      ▼      ▼
          Observe Discover Import
                     │
                     ▼
           12. RELATIONSHIP SERVICE
                     │
                     ▼
                13. SAGA ENGINE
                     │
                     ▼
              14. PLUGIN ZFS
                     │
                     ▼
            15. MOCK COMPUTE
                     │
                     ▼
       TEST MULTI-PROVIDER ATTACHMENT
                     │
                     ▼
            16. PLUGIN OPENBAO
                     │
                     ▼
          17. PLUGIN DESIGNATE
                     │
                     ▼
             PoC VALIDADO
                     │
                     ▼
             Reconciliation
                     │
                     ▼
                Scheduler
                     │
                     ▼
           Security / HA
                     │
                     ▼
          Rust / Python SDK
```

---

## 1. Monorepo e Estrutura Base

```text
orion/
├── contracts/          # Contratos congelados (proto + specs + validation)
├── sdk/
│   └── go/           # SDK Go para plugins
├── core/              # Runtime, services, adapters
├── plugins/
│   ├── mock/         # Mock plugin para testes
│   ├── zfs/          # Plugin ZFS real
│   ├── openbao/       # Plugin OpenBao real
│   └── designate/     # Plugin Designate real
├── deployments/        # Docker, K8s configs
├── migrations/        # PostgreSQL schemas
├── tests/
│   └── conformance/  # Suite de conformance
├── docs/              # Especificações v11.1
├── Makefile
├── go.work
└── docker-compose.yml
```

---

## 2. orion-contracts (Fundamento)

```
contracts/
├── proto/
│   └── orion/
│       ├── common/v1/
│       │   ├── error.proto
│       │   ├── context.proto
│       │   └── operation.proto
│       │
│       ├── plugin/v1/
│       │   ├── plugin.proto
│       │   ├── manifest.proto
│       │   ├── provider.proto
│       │   └── executor.proto
│       │
│       ├── resource/v1/
│       │   ├── resource.proto
│       │   └── spec.proto
│       │
│       └── relationship/v1/
│           ├── relationship.proto
│           └── saga.proto
│
└── specs/
    └── v1/
        ├── resources/
        │   ├── storage.volume.yaml
        │   ├── storage.snapshot.yaml
        │   ├── secret.secret.yaml
        │   ├── dns.zone.yaml
        │   └── dns.record.yaml
        │
        └── relationships/
            └── storage.attachment.yaml
```

### Conceitos Congelados

```
ResourceSpec
ProviderContractSpec
RelationshipSpec

Resource
Provider
PluginManifest

Operation
OrionError
RequestContext

SagaExecution
SagaStepExecution
```

---

## 3. Gerar Código Protobuf

```text
contracts
     ↓
protoc / buf
     ↓
gen/go/
     ├── orion/common/v1
     ├── orion/plugin/v1
     ├── orion/resource/v1
     └── orion/relationship/v1
```

Ferramentas: `buf.yaml`, `buf.gen.yaml`, `buf.lock`

---

## 4. Validador de Specifications

```
contracts/validation/
├── resource.go       # Valida ResourceSpec
├── relationship.go  # Valida RelationshipSpec
├── manifest.go      # Valida PluginManifest
├── capabilities.go  # Valida dependencies
└── schema.go        # Schema YAML validation
```

### Regra Crítica

```yaml
storage.volume.capabilities.snapshots = true
          ↓
requires storage.snapshot/v1
          ↓
manifest implementa?
    yes → OK
    no  → reject
```

---

## 5. orion-sdk-go

```text
sdk/go/
├── plugin/
│   ├── plugin.go      # Plugin struct, Config
│   ├── manifest.go    # ManifestBuilder
│   └── registration.go # Handler registration
│
├── resource/
│   ├── resource.go    # ResourceRequest/Response
│   └── handler.go     # ResourceOperationHandler
│
├── relationship/
│   ├── relationship.go
│   ├── role.go        # RoleSource, RoleTarget
│   └── handler.go
│
├── provider/
│   ├── config.go      # ProviderConfig
│   └── secrets.go     # SecretRef handling
│
├── request/
│   ├── request.go     # RequestContext decoding
│   └── decode.go
│
├── response/
│   └── response.go    # Response builders
│
├── errors/
│   └── errors.go       # OrionError
│
├── health/
│   └── health.go      # HealthCheck
│
└── server/
    └── grpc.go        # gRPC server
```

### API do Plugin SDK

```go
p := plugin.New(plugin.Config{
    ID:      "zfs-plugin",
    Name:    "ZFS",
    Version: "1.0.0",
})

p.Resource("orion.io/storage.volume", "v1").
    Handle("create", createVolume).
    Handle("delete", deleteVolume).
    Handle("resize", resizeVolume)

p.Relationship("orion.io/storage.attachment", "v1", plugin.RoleSource).
    Handle("prepare", prepareVolume).
    Handle("release", releaseVolume)

return p.Serve(ctx)
```

**O developer NÃO lida com:**
- grpc.Server
- protobuf marshal/unmarshal
- metadata
- health RPC
- manifest RPC
- trace propagation
- protocol negotiation
- OrionError protobuf

---

## 6. plugin-mock

```
plugins/mock/
├── cmd/main.go
├── volume.go           # storage.volume handlers
├── snapshot.go         # storage.snapshot handlers
├── relationship_source.go
├── relationship_target.go
└── manifest.yaml
```

### Recursos em memória

```go
map[string]*Volume
```

### Operations

```
storage.volume
    create
    delete
    resize

storage.snapshot
    create
    delete
    restore
    clone

source:
    prepare
    release

target:
    attach
    detach
```

---

## 7. Conformance Tests

```
tests/conformance/
├── suite.go           # TestSuite
├── manifest_test.go   # Validates manifest
├── resources_test.go  # Validates resources
├── relationships_test.go
├── capabilities_test.go
├── idempotency_test.go
└── errors_test.go
```

### Test Runner

```bash
orion-conformance ./plugin-zfs
orion-conformance ./plugin-openbao
orion-conformance ./plugin-designate
```

### Checks

- Plugin manifest válido
- Protocol version válida
- Handlers obrigatórios existem
- Capabilities são válidas
- Dependencies satisfeitas
- Create retorna resultado válido
- Idempotency funciona
- Errors seguem OrionError
- Observe funciona
- Health funciona

---

## 8. PostgreSQL State Store

```sql
resources
providers
plugins
operations
resource_relationships
relationship_executions
saga_steps
```

### Tabela resources

```sql
id                  -- Orion global ID
tenant_id
project_id

kind               -- e.g., orion.io/storage.volume
version            -- e.g., v1

provider_id
external_id        -- provider-specific ID (opaque to Orion)

state              -- PENDING, PROVISIONING, AVAILABLE, etc.

desired_spec jsonb
actual_state jsonb

generation
observed_generation
observed_at

created_at
updated_at
deleted_at
```

**Distinção central:** estado lógico (Orion) vs estado físico observado.

---

## 9. Ports/Repositories

```text
core/
├── domain/
├── application/
├── ports/
│   ├── resource_repository.go
│   ├── provider_repository.go
│   ├── plugin_repository.go
│   ├── operation_repository.go
│   └── relationship_repository.go
└── adapters/
    └── postgres/
```

### Interface Repository

```go
type ResourceRepository interface {
    Create(ctx context.Context, resource *Resource) error
    Get(ctx context.Context, id string) (*Resource, error)
    Update(ctx context.Context, resource *Resource) error
    Delete(ctx context.Context, id string) error
    List(ctx context.Context, filter Filter) ([]*Resource, error)
}
```

---

## 10. PluginRegistry

### Responsabilidades

```
RegisterPlugin
ValidateManifest
GetPlugin
ListPlugins
ValidateProtocolVersion
ValidateResourceImplementations
ValidateCapabilities
ValidateRelationshipRoles
```

### Fluxo

```
plugin starts
   ↓
GetManifest
   ↓
PluginRegistry
   ↓
protocolVersion?
   ↓
resources?
   ↓
capabilities?
   ↓
relationships?
   ↓
ACCEPTED
```

---

## 11. ProviderRegistry

**Plugin** = código.
**Provider** = configuração concreta.

```yaml
plugin: zfs-plugin

providers:
  zfs-dc1:
    endpoint: 10.0.0.1:50051
  zfs-dc2:
    endpoint: 10.0.0.2:50051
  zfs-backup:
    endpoint: 10.0.0.3:50051
```

### Controla

```
config
secretRefs
effectiveCapabilities
administrativeState
healthState
generation
observedGeneration
```

**Nunca grave secret real dentro de config.**

---

## 12. PluginExecutor

### Interface

```go
type PluginExecutor interface {
    Invoke(ctx context.Context, provider Provider, kind, version, operation string, input []byte) (*InvokeResult, error)
    Observe(ctx context.Context, provider Provider, externalID string) (*ObserveResult, error)
    Discover(ctx context.Context, provider Provider, kind, version string) (*DiscoverResult, error)
    Health(ctx context.Context, provider Provider) (*HealthResult, error)
}
```

### Fluxo

```
Resource Runtime
      ↓
Provider Registry
      ↓
resolve plugin/runtime
      ↓
PluginExecutor
      ↓ gRPC
plugin
```

**NUNCA conhecer:** ZFS, Ceph, OpenBao, Designate, Proxmox

---

## 13. OperationService

### Estados

```
PENDING
RUNNING
SUCCEEDED
FAILED
CANCELLING
CANCELLED
```

### Campos

```
operationId
requestId
idempotencyKey

tenantId
projectId

resourceId
providerId

kind
version
operation

state
attempt
error

createdAt
startedAt
completedAt
```

---

## 14. Idempotency

```
POST /resources
Idempotency-Key: create-postgres-volume-01
```

```
request 1 ───────┐
                 ├─→ mesma Operation
request 2 ───────┘
```

**Nunca:**
```
volume 1
volume 2
```

---

## 15. ResourceRuntime

### Fluxo CREATE

```
API
 ↓
ResourceService
 ↓
validate ResourceSpec
 ↓
Resource Runtime
 ↓
resolve provider
 ↓
validate capabilities
 ↓
create Orion Resource ID
 ↓
persist PROVISIONING
 ↓
create Operation
 ↓
transform ResourceSpec → ProviderContract
 ↓
PluginExecutor
 ↓
plugin.create()
 ↓
externalId
 ↓
persist
 ↓
AVAILABLE
```

---

## 16. ResourceService

### REST API

```
POST   /v1/resources
GET    /v1/resources
GET    /v1/resources/{id}
PATCH  /v1/resources/{id}
DELETE /v1/resources/{id}
POST   /v1/resources/{id}/observe
```

**Usuário nunca chama plugin diretamente.**

---

## 17. Observe

```
POST /resources/{id}/observe
        ↓
Resource Store
        ↓
providerId + externalId
        ↓
PluginExecutor.Observe
        ↓
provider
        ↓
actualState
        ↓
State Store
observedAt = now
```

**Observe() preenche actualState/observedAt, não create().**

---

## 18. Discover

```
POST /v1/providers/{id}/discover-unmanaged
```

**Regra fundamental:**
```
Discover
    ↓
consulta provider
    ↓
não cria Resource
    ↓
não altera State Store
```

---

## 19. Import

```
Discover
  ↓
tank/postgres
tank/gitlab
  ↓

POST /resources/import
  ↓

Orion Resource ID
```

---

## 20. RelationshipService

### REST API

```
POST   /v1/relationships
GET    /v1/relationships
GET    /v1/relationships/{id}
DELETE /v1/relationships/{id}
```

### Request

```json
{
  "relationshipKind": "orion.io/storage.attachment",
  "sourceResourceId": "orion-vol-123",
  "targetResourceId": "orion-vm-456",
  "config": {}
}
```

---

## 21. RelationshipRuntime / Saga Engine

### Fluxo

```
CreateRelationship
       ↓
load RelationshipSpec
       ↓
load source resource
       ↓
load target resource
       ↓
resolve providers
       ↓
build workflow
       ↓
Saga Engine
```

### Storage Attachment

```
source.prepare
      ↓ connection
target.attach
      ↓ attachmentHandle
Relationship ACTIVE
```

---

## 22. Persistir Saga Steps

### Tabela saga_steps

```sql
id
execution_id
step_index

role           -- source, target
operation       -- prepare, attach, detach, release

input jsonb
output jsonb

state

compensation_required
compensation_input

error
started_at
completed_at
```

---

## 23. Compensation

```
prepare ✓
attach ✗
```

**Resultado:**
```
release()
```

**Se attach produziu efeitos parciais:**
```
detach()
release()
```

**Sempre em ordem reversa.**

---

## 24. Plugin ZFS

```
plugins/zfs/
├── cmd/main.go
├── volume.go         # storage.volume
├── snapshot.go       # storage.snapshot
└── attachment/      # storage.attachment source
```

### Implements

```
storage.volume
storage.snapshot

storage.attachment
    role=source
```

---

## 25. mock-compute

```
plugins/mock-compute/
├── cmd/main.go
├── instance.go       # compute.instance
└── attachment/       # storage.attachment target
```

### Implements

```
compute.instance
storage.attachment
    role=target
```

### Primeira operação multi-provider

```
ZFS
  ↓ prepare
Orion
  ↓
mock-compute
  ↓ attach
```

---

## 26. Invariável Arquitetural

**Em CI:**

```bash
grep -Rni \
  -E 'zfs|ceph|openbao|designate|proxmox|vmware' \
  core/
```

**Resultado esperado:** zero matches

**Regra:**
```go
runtime.Invoke(ctx, provider, "orion.io/storage.volume", "v1", "create", payload)
```

**NUNCA:**
```go
if provider.Type == "zfs" {}
```

---

## 27. Plugin OpenBao

```
orion.io/secret.secret
```

### Operations

```
create
read
update
delete
```

**Prova que SDK não ficou contaminado por abstrações de storage.**

---

## 28. Plugin Designate

```
orion.io/dns.zone
orion.io/dns.record
```

**Prova integração REST externa e lifecycle diferente de ZFS/OpenBao.**

---

## 29. Conformance Suite Universal

```
plugin-zfs
        │
plugin-openbao
        ├──→ Orion Plugin Protocol v1 conformance
plugin-designate
        │
mock-compute
```

**Se qualquer teste precisar de `switch plugin.Name`, o desenho está errado.**

---

## 30. Reconciliation

```
desiredSpec
     ↓
Observe()
     ↓
actualState
     ↓
compare
     ↓
drift
```

### Modos

```
OBSERVE   -- só observa
REPORT    -- relata drift
ENFORCE   -- aplica mudanças
```

---

## 31. Provider Selection / Scheduler

**Inicial:** providerId explícito

**Depois:**
```
requirements
    ↓
capability filter
    ↓
health
    ↓
capacity
    ↓
policy
    ↓
scheduler
```

### Exemplo

```json
{
  "kind": "orion.io/storage.volume",
  "requirements": {
    "snapshots": true,
    "encryption": true
  }
}
```

---

## 32. Security

```
mTLS Core ↔ Plugin
plugin identity
certificate rotation
secret broker
RBAC
rate limiting
timeouts
circuit breaker
audit
OpenTelemetry
```

---

## 33-34. SDK Rust / Python

```
contracts
 ├── SDK Go
 ├── SDK Rust
 └── SDK Python
```

**Todos passam pelo mesmo conformance suite.**

---

## 35. ResourceSpecs Expandidos

Depois do PoC:

```
storage.filesystem
storage.share
storage.object

compute.instance

network.network
network.subnet
network.port
network.router

image.image

loadbalancer.*

backup.*

database.*
```

---

## Resumo Executivo

| # | Componente | Status |
|---|------------|--------|
| 1 | Monorepo | ✅ |
| 2 | Contracts | 🟡 |
| 3 | Protobuf gen | 🟡 |
| 4 | Validation | ✅ |
| 5 | SDK Go | ✅ |
| 6 | Mock plugin | ✅ |
| 7 | Conformance | 🟡 |
| 8 | State Store | ✅ |
| 9 | Ports/Repositories | ✅ |
| 10 | PluginRegistry | 🟡 |
| 11 | ProviderRegistry | 🟡 |
| 12 | PluginExecutor | ✅ |
| 13 | OperationService | ✅ |
| 14 | Idempotency | 🟡 |
| 15 | ResourceRuntime | ✅ |
| 16 | ResourceService | 🟡 |
| 17 | Observe | ✅ |
| 18 | Discover | 🟡 |
| 19 | Import | 🟡 |
| 20 | RelationshipService | 🟡 |
| 21 | Saga Engine | 🟡 |
| 22 | Saga persistence | ✅ |
| 23 | Compensation | 🟡 |
| 24 | Plugin ZFS | 🟡 |
| 25 | mock-compute | ✅ |
| 26 | Invariável test | ❌ |
| 27 | Plugin OpenBao | 🟡 |
| 28 | Plugin Designate | 🟡 |
| 29 | Conformance 3 plugins | ❌ |
| 30 | Reconciliation | 🟡 |
| 31 | Scheduler | 🟡 |
| 32 | Security | 🟡 |
| 33 | SDK Rust | 🟡 |
| 34 | SDK Python | 🟡 |
| 35 | Expanded specs | 🟡 |

**Status:** A base executável está funcional; os itens marcados como 🟡/❌ ainda exigem integração, testes de conformance ou implementação de produção.

# Orion Plugin System

> Versão: 11.1
> Data: 2026-10-02
> Status: **Orion Plugin Protocol v1.0 - CONGELADO**

---

## Conceito Central

> **"Para adicionar funcionalidade no OpenStack, preciso integrar com o ecossistema. Para adicionar funcionalidade no Orion, preciso implementar um contrato."**

O Orion é um **microkernel de infraestrutura** com um **State Store** no centro.

---

## Arquitetura de Contratos

```
┌─────────────────────────────────────────────────────────────┐
│                     PUBLIC CONTRACT                           │
│  ResourceSpec                                                 │
│  - modelo lógico do recurso                                   │
│  - usa ID global Orion                                       │
│  - inclui ProviderContractSpec (oficial, mantido pelo Orion) │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼ transform
┌─────────────────────────────────────────────────────────────┐
│                    INTERNAL CONTRACT                         │
│  ProviderContract (da ResourceSpec oficial)                  │
│  - inclui externalId                                         │
│  - Runtime resolve provider/externalId                       │
└─────────────────────────────────────────────────────────────┘
```

**Regra**: `ProviderContract` é **oficial**, mantido pelo Orion. Plugin **não define** contratos.

---

## Resource Specifications

### Resource Kinds: `orion.io/*`

```
orion.io/storage.volume
orion.io/storage.snapshot
orion.io/storage.filesystem
orion.io/storage.object
orion.io/storage.share

orion.io/compute.instance
orion.io/compute.host
orion.io/compute.baremetal

orion.io/network.network
orion.io/network.subnet
orion.io/network.port
orion.io/network.router
orion.io/network.floatingIp

orion.io/loadbalancer.loadbalancer
orion.io/loadbalancer.listener
orion.io/loadbalancer.pool
orion.io/loadbalancer.member
orion.io/loadbalancer.healthmonitor

orion.io/secret.secret
orion.io/secret.key
orion.io/secret.certificate

orion.io/dns.zone
orion.io/dns.record

orion.io/image.image
orion.io/backup.job
orion.io/database.instance
orion.io/monitoring.metric
orion.io/monitoring.alarm
orion.io/ha.protection
```

### Relationship Kinds: `orion.io/*`

```
orion.io/storage.attachment
orion.io/network.portAttachment
orion.io/network.floatingIpBinding
orion.io/loadbalancer.membership
orion.io/secret.attachment
orion.io/loadbalancer.certificateBinding
```

---

## ResourceSpec: `orion.io/storage.volume` v1

```yaml
kind: orion.io/storage.volume
version: v1

capabilitiesSchema:
  type: object
  properties:
    snapshots:
      type: boolean
      requires:
        kind: orion.io/storage.snapshot
        version: v1
    clones:
      type: boolean
    thinProvisioning:
      type: boolean
    multiAttach:
      type: boolean
    maxVolumeSizeGb:
      type: integer
    compression:
      type: object
      properties:
        supported:
          type: array
          items:
            type: string
            enum: [off, lz4, zstd, zstd-19, zstd-22]
        default:
          type: string
    encryption:
      type: object
      properties:
        supported:
          type: boolean
        default:
          type: boolean

operations:
  - name: create
    async: true
    cancellable: true
    inputSchema:
      type: object
      required: [name, sizeGb]
      properties:
        name:
          type: string
        sizeGb:
          type: integer
          minimum: 1
        compression:
          type: string
          enum: [off, lz4, zstd]
        encrypted:
          type: boolean
          default: false

  - name: delete
    async: true
    inputSchema:
      type: object
      required: [resourceId]
      properties:
        resourceId:
          type: string

  - name: resize
    async: true
    inputSchema:
      type: object
      required: [resourceId, sizeGb]
      properties:
        resourceId:
          type: string
        sizeGb:
          type: integer

# Official ProviderContract - maintained by Orion
providerContract:
  version: v1
  operations:
    - name: create
      inputSchema:
        type: object
        required: [name, sizeGb]
        properties:
          name:
            type: string
          sizeGb:
            type: integer
          compression:
            type: string
          encrypted:
            type: boolean
      outputSchema:
        type: object
        properties:
          externalId:
            type: string

    - name: delete
      inputSchema:
        required: [externalId]
        properties:
          externalId:
            type: string

    - name: resize
      inputSchema:
        required: [externalId, sizeGb]
        properties:
          externalId:
            type: string
          sizeGb:
            type: integer
```

---

## Capability Validation

```
ResourceSpec: snapshots: true
Requires: orion.io/storage.snapshot/v1

Registry valida:
  manifest declares storage.snapshot/v1?
  ├── sim → accepted
  └── não → REJECTED: snapshots requires storage.snapshot
```

---

## Plugin Manifest: Exemplo Correto

```yaml
pluginId: zfs-plugin
name: ZFS
version: 1.2.0
vendor: community

runtime:
  protocol: grpc
  protocolVersion: v1

implements:
  - kind: orion.io/storage.volume
    version: v1
    operations: [create, delete, resize]
    capabilities:
      snapshots: true
      compression:
        supported: [lz4, zstd, off]
        default: lz4
    providerConfigSchema:
      type: object
      required: [pool]
      properties:
        pool:
          type: string
        mountRoot:
          type: string
          default: /srv/volumes
    providerSecretsSchema:
      - name: sshKey
        kind: ssh.privateKey
        required: true

  - kind: orion.io/storage.snapshot
    version: v1
    operations: [create, delete, restore, clone]
    capabilities: {}

implementsRelationships:
  - kind: orion.io/storage.attachment
    version: v1
    role: source
    operations: [prepare, release]
```

---

## RelationshipSpec: `orion.io/storage.attachment` v1

```yaml
kind: orion.io/storage.attachment
version: v1

participants:
  - role: source
    resourceKind: orion.io/storage.volume
    requiredOperations: [prepare, release]
    operations:
      - name: prepare
        outputSchema:
          type: object
          required: [connection]
          properties:
            connection:
              type: object
      - name: release
        inputSchema:
          type: object
          required: [connection]
          properties:
            connection:
              type: object

  - role: target
    resourceKind: orion.io/compute.instance
    requiredOperations: [attach, detach]
    operations:
      - name: attach
        inputSchema:
          type: object
          required: [connection, config]
          properties:
            connection:
              type: object
            config:
              type: object
        outputSchema:
          type: object
          properties:
            attachmentHandle:
              type: string
      - name: detach
        inputSchema:
          type: object
          required: [attachmentHandle]
          properties:
            attachmentHandle:
              type: string

workflow:
  attach:
    - role: source
      operation: prepare
      compensateWith: release
    - role: target
      operation: attach
      compensateWith: detach

  detach:
    - role: target
      operation: detach
      compensateWith: attach
    - role: source
      operation: release
      compensateWith: prepare
```

---

## Saga / Compensation

```protobuf
message SagaStepExecution {
  int32 index = 1;
  string role = 2;
  string operation = 3;

  google.protobuf.Struct input = 4;      // what was passed to this step
  google.protobuf.Struct output = 5;     // what this step returned
  StepState state = 6;

  bool compensation_required = 7;        // step produced effects
  google.protobuf.Struct compensation_input = 8;  // what to pass to compensation

  OrionError error = 9;
}
```

**Compensation only for completed steps:**
```
step 0: source.prepare
  state: SUCCEEDED
  output: { connection: { protocol: "rbd", ... } }
  compensation_required: true

step 1: target.attach
  state: FAILED

Compensation (reverse order):
  step 1: NOT executed (failed)
  step 0: source.release(connection)
```

**If plugin reports partial effects:**
```
step 1: target.attach
  state: FAILED
  compensation_required: true
  compensation_input: { attachmentHandle: "..." }

Compensation:
  target.detach(attachmentHandle)
  source.release(connection)
```

---

## Resource Lifecycle

```
POST /resources
       ↓
Resource criado (id existe, externalId vazio)
state: PROVISIONING
       ↓
ProviderContract.create()
       ↓
Provider retorna externalId
       ↓
Resource: AVAILABLE, externalId preenchido
       ↓
Observe() later
       ↓
actualState preenchido
observedAt: now
```

---

## CreateResource Response

```json
POST /v1/resources
{
  "kind": "orion.io/storage.volume",
  "version": "v1",
  "providerId": "zfs-dc1",
  "spec": { "name": "postgres", "sizeGb": 500 }
}

→

{
  "resource": {
    "id": "orion-volume-01HZX7Q...",
    "state": "PROVISIONING",
    "externalId": null
  },
  "operation": {
    "id": "op-9a712",
    "state": "PENDING"
  }
}
```

---

## CreateRelationship: Multi-Provider

```json
POST /v1/relationships
{
  "relationshipKind": "orion.io/storage.attachment",
  "sourceResourceId": "orion-vol-123",
  "targetResourceId": "orion-vm-456",
  "config": { "device": "/dev/vdb" }
}
```

**Saga Execution:**
```
RelationshipExecution:
  steps:
    - index: 0
      role: source
      operation: prepare
      input: {}
      output: { connection: { protocol: "rbd", path: "rbd/vol-x" } }
      state: SUCCEEDED
      compensation_required: true
      compensation_input: { connection: {...} }

    - index: 1
      role: target
      operation: attach
      input: { connection: {...}, config: {...} }
      output: { attachmentHandle: "hw-0001", device: "/dev/vdb" }
      state: SUCCEEDED
      compensation_required: true
      compensation_input: { attachmentHandle: "hw-0001" }

  state: ACTIVE
```

---

## Estados

### ResourceState
```
PENDING, PROVISIONING, AVAILABLE, UPDATING, DELETING, DELETED, ERROR, IMPORTING
```

### RelationshipState
```
PENDING, PREPARING, ATTACHING, ACTIVE, DETACHING, COMPENSATING, DELETED, ERROR
```

### OperationState
```
PENDING, RUNNING, SUCCEEDED, FAILED, CANCELLING, CANCELLED
```

### StepState
```
PENDING, RUNNING, SUCCEEDED, FAILED, COMPENSATING, COMPENSATED, COMPENSATION_FAILED
```

---

## REST API

```
# Resources
POST   /v1/resources
GET    /v1/resources
GET    /v1/resources/{id}
PATCH  /v1/resources/{id}
DELETE /v1/resources/{id}
POST   /v1/resources/{id}/observe

# Import
POST   /v1/resources/import

# Relationships
POST   /v1/relationships
GET    /v1/relationships
GET    /v1/relationships/{id}
DELETE /v1/relationships/{id}

# Providers
POST   /v1/providers
GET    /v1/providers
GET    /v1/providers/{id}
PATCH  /v1/providers/{id}
DELETE /v1/providers/{id}

# Discover
POST   /v1/providers/{id}/discover-unmanaged
# Only queries provider, returns unmanaged resources, does NOT modify State Store

# Operations
GET    /v1/operations/{id}
POST   /v1/operations/{id}/cancel
```

---

## ADR-001: Core Must Be Provider-Agnostic

```go
// CORRETO
runtime.Invoke(ctx, provider, "orion.io/storage.volume", "v1", "create", payload)

// ERRADO
if provider.Type == "zfs" {
    // ...
}
```

---

## Roadmap PoC

### Fase 1: Foundation
- [ ] State Store
- [ ] ResourceService + RelationshipService
- [ ] PluginRegistry + ProviderRegistry
- [ ] PluginExecutor (internal)

### Fase 2: Relationships
- [ ] Multi-provider orchestration
- [ ] Saga/compensation
- [ ] Import/Discover

### Fase 3: Reference Plugins
- [ ] ZFS → storage.volume + storage.snapshot + attachment(source)
- [ ] OpenBao → secret.secret
- [ ] Designate → dns.zone, dns.record
- [ ] Mock compute (attachment target)

---

## Glossário

| Termo | Definição |
|-------|-----------|
| ResourceSpec | Contrato público + ProviderContractSpec oficial |
| ProviderContractSpec | Contrato interno mantido pelo Orion (não pelo plugin) |
| SagaStepExecution | input/output/state por step para compensation |
| compensation_required | Indica se step produziu efeitos que precisam ser revertidos |
| compensation_input | Dados necessários à compensação |
| observedAt | Só preenchido por Observe(), não por create() |

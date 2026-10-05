# OpenStack gap closure

Implementacoes que ja estao no codigo do Orion:

- Identity: usuarios, projetos e roles administrativos protegidos por token de
  sistema; senhas novas com bcrypt; persistencia em memoria e PostgreSQL.
- Image: criacao de metadados, upload limitado a 20 GiB, checksum SHA-256,
  download e remocao de imagens nao protegidas.
- Network: security groups e regras com persistencia; IDs de grupos em portas;
  ACLs OVN por porta, com deny de ingress por padrao.
- Messaging: subscription com ID unico, contexto independente da chamada RPC,
  recebimento explicito, consumers NATS/Kafka e lifecycle de unsubscribe.
- Telemetry: datapoints consultaveis, collector Prometheus, alertas com
  pending/firing/resolved e persistencia opcional em `ORION_TELEMETRY_STATE_FILE`.
- Identity providers: redirect URI restrita, client authentication, PKCE
  forwarding e tratamento correto de token invalido em Azure/Keycloak.

- Network: routers, interfaces L3 e floating IPs persistidos, com DNAT/SNAT no
  driver OVN.
- Volume: snapshots persistidos e create/delete/restore no gRPC do host-agent,
  com implementacoes reais para LVM e Ceph RBD.
- Placement: resource providers hierarquicos com pai/raiz, traits, inventories,
  geracoes e CRUD persistido.
- Heat/Orchestration: validacao declarativa de recursos/dependencias sem ciclos,
  preview, eventos e rollback do template anterior, alem da listagem sem panic
  quando o filtro de tenant nao e enviado; estado persistente opcional em
  `ORION_ORCHESTRATION_STATE_FILE`. O backend `orion` materializa recursos
  `orion.io/*` via SDK, em ordem topologica, com IDs persistidos, delete
  reverso e replacement quando o provider nao oferece `update`. Configure
  `ORION_HEAT_PROVIDER_ENDPOINT`, `ORION_HEAT_PROVIDER_ENDPOINT_<TIPO>` ou
  `provider_endpoint` por recurso; `provider` pode ser usado para selecionar
  `ORION_HEAT_PROVIDER_ENDPOINT_<PROVIDER>`.
- Octavia/LoadBalancer: TLS termination, ACLs L7 por host/path, backends e
  rate limiting efetivos na configuracao HAProxy, com estado persistente em
  `ORION_LOADBALANCER_STATE_FILE`.
- Manila/Swift: `plugins/manila` unifica o controle de shares e encaminha para
  CephFS/NFS/SMB/GlusterFS; CephFS expõe snapshots reais. S3 expõe
  versionamento, lifecycle e o contrato `orion.io/storage.object.swift`.

Terraform e Ansible continuam disponiveis como backends legados para templates
proprios dessas ferramentas. Para templates Heat/YAML com recursos Orion, o
backend nativo usa o protocolo de plugins e nao depende de Docker ou de um
executor externo. O certificado TLS do Octavia ainda e recebido como caminho
de bundle HAProxy; a integração opcional com Secret Manager pode ser adicionada
sem alterar o contrato L7.

Esses itens devem ser implementados no control plane Orion; Ceph, NFS, KVM,
OVN, Azure, Keycloak, OpenBao, NATS e Kafka permanecem plugins/provedores.

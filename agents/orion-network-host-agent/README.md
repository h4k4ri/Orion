# orion-network-host-agent

Agente de rede no host.

Responsavel por bindings, TAPs e saneamento de recursos locais de conectividade integrados ao backend `OVS/OVN`.

Fluxo atual:

- descobre `host_id` automaticamente a partir de `ORION_NETWORK_HOST_ID`, `HOSTNAME`, `/etc/hostname` ou `hostname`
- consome eventos versionados de porta no subject `orion.event.network.port.>` via NATS para reconciliar imediatamente
- consulta o `orion-network` por portas com `binding_host_id` igual ao host local como fallback periodico
- garante `br-int` via `ovs-vsctl --may-exist add-br br-int` quando houver portas bound para o host
- reporta `binding_status` de volta ao `orion-network`, para o `status` da porta refletir realizacao local + OVN
- materializa estado local por porta e por rede em disco
- remove estado local stale quando a porta deixa de apontar para o host

Variaveis principais:

- `ORION_NETWORK_HOST_ID`: override explicito do identificador do host
- `ORION_NETWORK_URL`: URL do servico `orion-network`
- `ORION_NATS_URL`: URL do barramento NATS
- `ORION_NETWORK_HOST_AGENT_STATE_DIR`: diretorio do estado local
- `ORION_NETWORK_HOST_AGENT_RECONCILE_INTERVAL`: intervalo do loop de reconciliacao em segundos

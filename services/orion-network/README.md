# orion-network

Servico de rede do Orion.

Escopo inicial:

- redes;
- sub-redes;
- portas;
- IPAM;
- security groups basicos;
- bindings para host com `OVS/OVN`.

Implementacao inicial:

- `network` -> `Logical_Switch` no OVN;
- `subnet` -> `DHCP_Options` no OVN;
- `port` -> `Logical_Switch_Port` com MAC/IP fixos;
- `node-agent` pluga a VIF no `br-int` com `interfaceid=<port-id>`.
- leitura de `port.status` reconciliada do campo `up` no OVN.
- security groups e regras persistidos em PostgreSQL e aplicados como ACLs
  OVN por porta, com deny de entrada por padrao.

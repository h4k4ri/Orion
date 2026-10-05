# OpenStack Service Model Reference

Este documento registra o que o Orion deve reaproveitar do modelo padrao do OpenStack e o que deve simplificar deliberadamente.

O criterio aqui nao e copiar implementacoes ou compatibilidade de API, e sim aproveitar conceitos operacionais provados.

## Identity: Keystone -> orion-identity

Aproveitar:

- autenticacao centralizada;
- tokens escopados;
- papeis padrao;
- catalogo de servicos;
- separacao entre operacoes de tenant e operacoes de sistema.

Modelo inicial no Orion:

- um unico dominio `Default`;
- autenticacao por `username + password`;
- tokens opacos emitidos pelo `orion-identity`;
- escopos `project` e `system`;
- papeis `admin`, `member`, `reader`;
- catalogo de servicos entregue junto do token.

Simplificacoes:

- sem federacao externa;
- sem trusts;
- sem MFA;
- sem token provider distribuido no primeiro corte.

## Compute and Cells: Nova -> orion-compute

Aproveitar:

- separacao entre API, scheduler e execucao;
- modelo celular para escalar e limitar blast radius;
- continuidade de execucao local mesmo com problemas fora do host;
- uso de tasks assincronas para operacoes longas.

Modelo inicial no Orion:

- API global unica;
- uma celula funcional primeiro;
- scheduler local antes do scheduler global;
- `orion-compute` como orquestrador local de build;
- `orion-node-agent` como executor no host.

Simplificacoes:

- sem live migration no inicio;
- sem resize complexo;
- sem multiplos hypervisors;
- sem herdar a topologia historica de servicos do Nova.

## Placement: Placement + Nova traits/inventory -> orion-placement

Aproveitar:

- inventario por host;
- traits;
- separacao entre capacidade agregada e escolha final de host;
- update otimista e geracao de inventario.

Modelo inicial no Orion:

- placement detalhado por celula;
- placement global apenas agregado por celula em fase posterior;
- foco inicial em CPU, RAM, disco e traits basicos.

Simplificacoes:

- sem microversions logo no inicio;
- sem superficie completa de resource providers.

## Network: Neutron + OVN -> orion-network

Aproveitar:

- recursos centrais `network`, `subnet`, `port`, `security group`;
- controle logico separado da aplicacao no host;
- uso de `OVN/OVS` como backend opinativo;
- consistencia entre banco de controle e estado do backend.

Modelo inicial no Orion:

- servico de rede mais enxuto;
- bindings de porta para host;
- roteamento e DHCP baseados no que o OVN ja entrega bem;
- reconciliacao explicita entre estado desejado e estado observado.

Simplificacoes:

- sem backend pluggable no inicio;
- sem variedade historica de agentes e extensoes do Neutron;
- sem SDN avancado alem do necessario para boot e conectividade basica.

## Image: Glance -> orion-image

Aproveitar:

- separacao entre metadados de imagem e dados da imagem;
- catalogo central de imagens;
- fluxo de upload/import;
- metadados suficientes para boot e validacao.

Modelo inicial no Orion:

- API unica de imagens;
- upload e distribuicao basicos;
- suporte a metadados e versionamento;
- object storage como destino preferencial posterior.

Simplificacoes:

- sem suporte precoce a todos os workflows de import do Glance;
- sem plugin ecosystem grande logo no inicio.

## Volume: Cinder -> orion-volume

Aproveitar:

- servico separado de block storage;
- modelo de driver;
- attach e detach como operacoes explicitas;
- registro claro de anexos;
- tolerancia a falhas e recuperacao por reconciliacao.

Modelo inicial no Orion:

- um unico driver `LVM`;
- volumes locais a celula;
- attach apenas dentro da mesma celula;
- volume materializado em um host especifico da celula;
- `orion-volume-host-agent` executando operacoes locais.

Simplificacoes:

- sem multi-attach;
- sem storage distribuido;
- sem drivers externos no primeiro corte.

## Regra de projeto

Todo novo servico do Orion deve explicitar em seu `README` ou design doc:

1. qual servico ou conceito do OpenStack inspirou o desenho;
2. o que foi reaproveitado;
3. o que foi simplificado;
4. o que foi removido por decisao de escopo.

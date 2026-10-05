# orion-node-agent

Agente principal de execucao no host.

Responsavel por aplicar estado desejado de servidores sobre `KVM/libvirt`, publicar estado observado e coordenar anexos de rede e volume.

Inspiracao OpenStack:

- papel operacional proximo do `nova-compute` no host
- integracao com `libvirt`

Simplificacoes deliberadas:

- API HTTP simples no primeiro corte;
- estado em memoria antes de reconciliacao completa.

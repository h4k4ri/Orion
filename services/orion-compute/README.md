# orion-compute

Servico de compute do Orion.

Inspiracao OpenStack:

- `Nova`
- separacao entre API de entrada e orquestracao de build
- integracao com `Placement`

Escopo inicial:

- ciclo de vida de servidores;
- coordenacao com placement, image, network e volume;
- execucao por task;
- reconciliacao do estado observado da VM.

Simplificacoes deliberadas:

- sem `conductor` separado no primeiro corte;
- sem scheduler proprio, delegando selecao de host ao `orion-placement`;
- sem `libvirt` binding direto no compute;
- execucao concreta delegada ao `orion-node-agent`.

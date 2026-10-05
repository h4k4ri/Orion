# orion-placement

Placement do Orion.

Inspiracao OpenStack:

- `Placement`
- inventario e traits consumidos pelo `Nova`

Escopo inicial:

- inventario detalhado por host na celula;
- selecao de host na celula;
- evolucao posterior para visao agregada global por celula.

Simplificacoes deliberadas:

- sem microversions;
- sem superficie completa de `resource providers`;
- sem hierarquia complexa de providers no primeiro corte;
- sem persistencia inicial alem de memoria;
- foco em `vCPU`, `RAM`, `disk` e `traits` basicos.

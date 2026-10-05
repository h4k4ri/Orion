# orion-api

API publica unificada do Orion.

Inspiracao OpenStack:

- papel equivalente ao endpoint publico agregado que, no OpenStack, normalmente exporia APIs separadas no catalogo;
- integracao com `Keystone` para autenticacao e catalogo.

Responsabilidades iniciais:

- autenticacao de requests;
- roteamento para servicos e celulas;
- exposicao de resources e tasks;
- envelope de erro e contrato HTTP canonico.

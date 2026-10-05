# orion-identity

Servico de identidade do Orion.

Inspiracao OpenStack:

- `Keystone`

Escopo inicial:

- usuarios;
- organizacoes e projetos;
- autenticacao;
- tokens;
- autorizacao simples para operadores e tenants.

Superficie implementada:

- gerenciamento protegido de usuarios, projetos e roles por token com escopo `system`;
- senhas novas armazenadas com bcrypt;
- persistencia em memoria para testes e PostgreSQL para producao.

Ainda pendente:

- um unico dominio `Default`;
- tokens opacos no primeiro corte;
- escopos `project` e `system`;
- federacao externa, trusts, MFA e revogacao administrativa de tokens.

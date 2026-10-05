# Como contribuir

Obrigado por se interessar em contribuir com o Orion. Este projeto e uma plataforma IaaS open source escrita em Go e Rust, e toda contribuicao de codigo, documentacao, correcao de bug ou discussao de design e bem-vinda.

## Onde comecar

- Leia o [`README.md`](README.md) para entender o proposito do projeto.
- Leia o [`docs/openstack-service-model.md`](docs/openstack-service-model.md) para entender o que o Orion reaproveita do OpenStack e o que simplifica.
- Leia o [`docs/execution-plan.md`](docs/execution-plan.md) para entender as fases e a ordem recomendada de implementacao.
- Olhe as issues abertas. Issues marcadas com `good first issue` sao boas portas de entrada.
- Se nao encontrar uma issue que combine com voce, abra uma nova descrevendo o que quer fazer antes de comecar.

## Dependencias locais

- `Go 1.27+`
- `Rust 1.80+` (toolchain estavel)
- `Docker` e `Docker Compose`
- `libvirt`, `ovs`, `ovn`, `lvm2` no host de execucao
- `nats-server`, `postgres` (subidos via `make dev-up`)

Comandos uteis:

```bash
make dev-up     # dependencias locais (postgres, nats)
make check      # build + lint + testes
make build      # compila control plane e host agents
```

## Fluxo de trabalho

1. Faca um fork do repositorio.
2. Crie uma branch a partir de `main`:
   ```bash
   git checkout -b tipo/descricao-curta
   ```
   Tipos comuns: `feat/`, `fix/`, `docs/`, `refactor/`, `test/`, `chore/`.
3. Faca commits pequenos e focados. Cada commit deve representar uma mudanca logica unica.
4. Escreva testes para qualquer comportamento novo ou correcao de bug.
5. Rode `make check` localmente antes de abrir o PR.
6. Abra um Pull Request contra `main` descrevendo:
   - o problema que voce resolve ou a capacidade que adiciona;
   - como voce testou;
   - qualquer impacto em outros servicos ou agentes.

## Mensagens de commit

Seguimos [Conventional Commits](https://www.conventionalcommits.org/pt-BR/v1.0.0/) em portugues, resumido:

```text
tipo(escopo opcional): resumo curto em minusculo

Corpo opcional explicando o que e por que. Quebre linhas em ~72 colunas.
```

Tipos mais usados:

- `feat`: nova capacidade
- `fix`: correcao de bug
- `docs`: somente documentacao
- `refactor`: mudanca interna sem alterar comportamento
- `test`: adicionando ou ajustando testes
- `chore`: manutencao (deps, CI, etc.)

Exemplos:

```text
feat(orion-compute): adiciona endpoint de criacao de VM com traits
fix(orion-identity): corrige renovacao de token opaco expirado
docs(readme): explica o criterio de simplificacao vs OpenStack
```

## Boas praticas de codigo

### Geral

- Nao commite segredo algum (token, chave, senha, certificado). Use `.env` local nunca versionado.
- Nao commite artefatos de build (`target/`, `bin/`, `dist/`, `tmp/`) ou arquivos temporarios. O `.gitignore` ja cobre a maioria.
- Mantenha o escopo de cada commit e PR pequeno. PRs grandes sao mais dificeis de revisar.

### Go

- `go fmt` antes de commitar.
- `go vet ./...` deve passar limpo.
- Prefira pacotes pequenos com responsabilidade clara. Reaproveite o nucleo em `core/` quando possivel.
- Mantenha adaptadores (DB, NATS, HTTP) explicitos; nao misture com dominio.

### Rust

- `cargo fmt` antes de commitar.
- `cargo clippy --all-targets -- -D warnings` deve passar limpo.
- Erros devem propagar com `?` ou serem tratados explicitamente; evite `unwrap` em codigo de producao.
- Use `tracing` (nao `println!`) para logs de operacao.

### Protobuf

- Toda mudanca em `.proto` exige rodar a geracao de codigo (`make gen` ou equivalente) e commitar tanto o proto quanto os arquivos gerados em `gen/`, `sdk/go/`, `sdk/rust/` ou `agents/*/src/generated/`.

## Documentacao

- Toda mudanca de comportamento visivel ao usuario (CLI, API, config) deve atualizar o `README.md` ou os arquivos em `docs/` correspondentes.
- Toda mudanca relevante deve ser adicionada na secao `[Unreleased]` do [`CHANGELOG.md`](CHANGELOG.md).

## Reportando bugs

Abra uma issue com:

- descricao curta e objetiva;
- passos para reproduzir;
- comportamento esperado vs observado;
- versao do Orion, SO e versao do Go/Rust;
- logs relevantes (use blocos de codigo, nunca cole segredos).

## Propostas maiores

Para mudancas que afetam arquitetura, contratos gRPC ou decisoes de longo prazo, abra uma issue com o prefixo `[RFC]` e espere discussao antes de escrever codigo.

## Codigo de conduta

Esperamos respeito nas conversas, reviews e discussions. Ataques pessoais, assedio ou comportamento desrespeitoso nao sao tolerados.

## Licenca

Ao contribuir, voce concorda que suas contribuicoes serao licenciadas sob Apache-2.0, a mesma licenca do projeto.
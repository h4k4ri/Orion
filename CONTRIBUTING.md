# Contributing

Thanks for your interest in contributing to Orion. This project is an open source IaaS platform written in Go and Rust. Every contribution of code, documentation, bug fixes, or design discussion is welcome.

## Where to start

- Read the [`README.md`](README.md) to understand the purpose of the project.
- Read [`docs/openstack-service-model.md`](docs/openstack-service-model.md) to see what Orion reuses from OpenStack and what it simplifies.
- Read [`docs/execution-plan.md`](docs/execution-plan.md) to understand the phases and the recommended implementation order.
- Browse open issues. Issues labeled `good first issue` are good entry points.
- If you do not find an issue that fits, open a new one describing what you want to do before starting.

## Local dependencies

- `Go 1.27+`
- `Rust 1.80+` (stable toolchain)
- `Docker` and `Docker Compose`
- `libvirt`, `ovs`, `ovn`, `lvm2` on the execution host
- `nats-server`, `postgres` (started via `make dev-up`)

Useful commands:

```bash
make dev-up     # local dependencies (postgres, nats)
make check      # build + lint + tests
make build      # compile control plane and host agents
```

## Workflow

1. Fork the repository.
2. Create a branch from `main`:
   ```bash
   git checkout -b type/short-description
   ```
   Common types: `feat/`, `fix/`, `docs/`, `refactor/`, `test/`, `chore/`.
3. Make small, focused commits. Each commit should represent a single logical change.
4. Write tests for any new behavior or bug fix.
5. Run `make check` locally before opening the PR.
6. Open a Pull Request against `main` describing:
   - the problem you solve or the capability you add;
   - how you tested it;
   - any impact on other services or agents.

## Commit messages

We follow [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/), summarized:

```text
type(optional scope): short lowercase summary

Optional body explaining what and why. Wrap lines at ~72 columns.
```

Most used types:

- `feat`: new capability
- `fix`: bug fix
- `docs`: documentation only
- `refactor`: internal change with no behavior impact
- `test`: adding or adjusting tests
- `chore`: maintenance (deps, CI, etc.)

Examples:

```text
feat(orion-compute): add VM creation endpoint with traits
fix(orion-identity): fix renewal of expired opaque token
docs(readme): explain the simplification criterion vs OpenStack
```

## Coding best practices

### General

- Never commit secrets (tokens, keys, passwords, certificates). Use a local, never-versioned `.env`.
- Never commit build artifacts (`target/`, `bin/`, `dist/`, `tmp/`) or temporary files. The `.gitignore` already covers most of them.
- Keep the scope of each commit and PR small. Large PRs are harder to review.

### Go

- Run `go fmt` before committing.
- `go vet ./...` must pass clean.
- Prefer small packages with a clear responsibility. Reuse the core in `core/` when possible.
- Keep adapters (DB, NATS, HTTP) explicit; do not mix them with domain code.

### Rust

- Run `cargo fmt` before committing.
- `cargo clippy --all-targets -- -D warnings` must pass clean.
- Errors must propagate with `?` or be handled explicitly; avoid `unwrap` in production code.
- Use `tracing` (not `println!`) for operational logs.

### Protobuf

- Any change to a `.proto` file requires running code generation (`make gen` or equivalent) and committing both the proto and the generated files under `gen/`, `sdk/go/`, `sdk/rust/`, or `agents/*/src/generated/`.

## Documentation

- Any change in user-visible behavior (CLI, API, config) must update the `README.md` or the corresponding files in `docs/`.
- Any notable change must be added to the `[Unreleased]` section of [`CHANGELOG.md`](CHANGELOG.md).

## Reporting bugs

Open an issue with:

- a short, objective description;
- steps to reproduce;
- expected vs observed behavior;
- Orion version, OS, and Go/Rust versions;
- relevant logs (use code blocks, never paste secrets).

## Larger proposals

For changes that affect architecture, gRPC contracts, or long-term decisions, open an issue prefixed with `[RFC]` and wait for discussion before writing code.

## Code of conduct

We expect respect in conversations, reviews, and discussions. Personal attacks, harassment, or disrespectful behavior are not tolerated.

## License

By contributing, you agree that your contributions will be licensed under Apache-2.0, the same license as the project.
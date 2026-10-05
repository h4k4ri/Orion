GO ?= go
CARGO ?= cargo

.PHONY: fmt
fmt:
	$(GO) fmt ./...
	$(CARGO) fmt --all

.PHONY: test
test:
	$(GO) test ./libs/... ./services/... ./gen/... ./cli/orion-cli/internal/... ./cli/orion-cli/cmd/orion
	$(CARGO) test --workspace

.PHONY: test-e2e
test-e2e:
	$(GO) test ./cli/orion-cli/cmd/orion-e2e

.PHONY: build
build:
	mkdir -p bin
	$(GO) build -o bin/orion-identity ./services/orion-identity/cmd/orion-identity
	$(GO) build -o bin/orion-api ./services/orion-api/cmd/orion-api
	$(GO) build -o bin/orion-placement ./services/orion-placement/cmd/orion-placement
	$(GO) build -o bin/orion-compute ./services/orion-compute/cmd/orion-compute
	$(GO) build -o bin/orion-image ./services/orion-image/cmd/orion-image
	$(GO) build -o bin/orion-network ./services/orion-network/cmd/orion-network
	$(GO) build -o bin/orion-volume ./services/orion-volume/cmd/orion-volume
	$(GO) build -o bin/orion ./cli/orion-cli/cmd/orion
	$(GO) build -o bin/orion-e2e ./cli/orion-cli/cmd/orion-e2e
	$(CARGO) build --workspace

.PHONY: check
check:
	$(GO) test ./...
	$(CARGO) check --workspace

.PHONY: docs-install
docs-install:
	cd docs-site && npm install

.PHONY: docs-dev
docs-dev:
	cd docs-site && npm run dev

.PHONY: docs-build
docs-build:
	cd docs-site && npm run build

.PHONY: dev-up
dev-up:
	docker compose -f docker-compose.dev.yml up -d

.PHONY: dev-down
dev-down:
	docker compose -f docker-compose.dev.yml down

.PHONY: e2e
e2e:
	$(GO) run ./cli/orion-cli/cmd/orion-e2e

.PHONY: deb-e2e
deb-e2e:
	./packaging/deb/e2e/run.sh

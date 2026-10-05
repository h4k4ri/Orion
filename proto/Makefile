.PHONY: proto-gen proto-lint proto-break check-install install-buf

PROTO_DIR := .
GEN_GO_DIR := ../gen/go
GEN_RUST_DIR := ../gen/rust/src

OS := $(shell uname -s)

proto-gen: check-install
	rm -rf $(GEN_GO_DIR) $(GEN_RUST_DIR)
	buf generate --template buf.gen.yaml $(PROTO_DIR)
	@echo "Generated:"
	@echo "  Go:  $(GEN_GO_DIR)/"
	@echo "  Rust: $(GEN_RUST_DIR)/ (requires protoc + prost + tonic)"

proto-lint:
	buf lint $(PROTO_DIR)

proto-break:
	buf breaking $(PROTO_DIR) --against '../../.git?ref=HEAD~1'

check-install:
	@which buf > /dev/null || (echo "buf not found. Install: https://docs.buf.build/installation" && exit 1)
	@buf --version

install-buf:
ifeq ($(OS),Darwin)
	brew install buf
else ifeq ($(OS),Linux)
	curl -sSL https://buf.run/install.sh | bash
else
	echo "Unsupported OS: $(OS). Install buf manually: https://docs.buf.build/installation"
endif

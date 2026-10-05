FROM rust:1.82-slim-bookworm AS builder

WORKDIR /app
RUN apt-get update && apt-get install -y musl-tools && rm -rf /var/lib/apt/lists/*

COPY . .
RUN cargo build --release -p orion-node-agent

FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*

COPY --from=builder /app/target/release/orion-node-agent /usr/local/bin/

ENV ORION_NODE_AGENT_GRPC_PORT=50051
ENV ORION_NODE_AGENT_STORAGE_DIR=/var/tmp/orion-node-agent

EXPOSE 50051

ENTRYPOINT ["orion-node-agent"]

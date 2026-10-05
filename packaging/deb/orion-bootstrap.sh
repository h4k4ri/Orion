#!/usr/bin/env bash
set -euo pipefail

ORION_ETC_DIR="${ORION_ETC_DIR:-/etc/orion}"
ORION_ENV_FILE="${ORION_ENV_FILE:-$ORION_ETC_DIR/orion.env}"
ORION_DATA_DIR="${ORION_DATA_DIR:-/var/lib/orion}"
ORION_LOG_DIR="${ORION_LOG_DIR:-/var/log/orion}"

ensure_dirs() {
  mkdir -p \
    "$ORION_ETC_DIR" \
    "$ORION_DATA_DIR" \
    "$ORION_LOG_DIR" \
    "$ORION_DATA_DIR/image" \
    "$ORION_DATA_DIR/node-agent" \
    "$ORION_DATA_DIR/network-host-agent" \
    "$ORION_DATA_DIR/volume-host-agent"
}

ensure_env() {
  ensure_dirs
  if [[ -f "$ORION_ENV_FILE" ]]; then
    return 0
  fi

  cat >"$ORION_ENV_FILE" <<EOF
ORION_DB_DSN=postgres://orion:orion@127.0.0.1:5432/orion?sslmode=disable
ORION_NATS_URL=nats://127.0.0.1:4222
ORION_IDENTITY_URL=http://127.0.0.1:8081
ORION_PLACEMENT_URL=http://127.0.0.1:8082
ORION_COMPUTE_URL=http://127.0.0.1:8083
ORION_NODE_AGENT_URL=http://127.0.0.1:8084
ORION_NODE_AGENT_GRPC_ADDR=127.0.0.1:50051
ORION_IMAGE_URL=http://127.0.0.1:8085
ORION_NETWORK_URL=http://127.0.0.1:8086
ORION_NETWORK_HOST_AGENT_URL=http://127.0.0.1:8089
ORION_VOLUME_URL=http://127.0.0.1:8087
ORION_VOLUME_HOST_AGENT_URL=http://127.0.0.1:8088
ORION_NETWORK_HOST_AGENT_LISTEN_ADDR=0.0.0.0:8089
ORION_IMAGE_STORE_DIR=$ORION_DATA_DIR/image
ORION_NODE_AGENT_STORAGE_DIR=$ORION_DATA_DIR/node-agent
ORION_NETWORK_HOST_AGENT_STATE_DIR=$ORION_DATA_DIR/network-host-agent
ORION_VOLUME_HOST_AGENT_STATE_DIR=$ORION_DATA_DIR/volume-host-agent
OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:4318
EOF
}

restart_services() {
  ensure_env
  if ! command -v systemctl >/dev/null 2>&1; then
    echo "systemctl not found; skipping restart" >&2
    return 0
  fi

  systemctl daemon-reload
  for service in \
    orion-identity \
    orion-api \
    orion-placement \
    orion-compute \
    orion-image \
    orion-network \
    orion-volume \
    orion-node-agent \
    orion-network-host-agent \
    orion-volume-host-agent
  do
    systemctl restart "${service}.service" || true
  done
}

print_usage() {
  cat <<'EOF'
Usage: orion-bootstrap [--ensure-dirs] [--ensure-env] [--restart] [--print-env]
EOF
}

main() {
  case "${1:---ensure-env}" in
    --ensure-dirs)
      ensure_dirs
      ;;
    --ensure-env)
      ensure_env
      ;;
    --restart)
      restart_services
      ;;
    --print-env)
      ensure_env
      cat "$ORION_ENV_FILE"
      ;;
    -h|--help)
      print_usage
      ;;
    *)
      print_usage >&2
      exit 1
      ;;
  esac
}

main "$@"

#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
PKG_DIR="$ROOT_DIR/packaging/deb"
BUILD_DIR="$PKG_DIR/.build"
OUT_DIR="$PKG_DIR/out"
BOOTSTRAP_SRC="$PKG_DIR/orion-bootstrap.sh"
VERSION="${VERSION:-0.1.0}"
ARCH="${ARCH:-amd64}"

require() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing required tool: $1" >&2
    exit 1
  }
}

write_control() {
  local root="$1"
  local pkg="$2"
  local desc="$3"
  local deps="$4"
  local extra="${5:-}"

  mkdir -p "$root/DEBIAN"
  {
    echo "Package: $pkg"
    echo "Version: $VERSION"
    echo "Section: admin"
    echo "Priority: optional"
    echo "Architecture: $ARCH"
    echo "Maintainer: Horizon <opensource@horizon.invalid>"
    echo "Depends: $deps"
    if [ -n "$extra" ]; then
      echo "$extra"
    fi
    echo "Description: $desc"
  } >"$root/DEBIAN/control"
}

write_postinst() {
  local root="$1"
  cat >"$root/DEBIAN/postinst" <<'EOF'
#!/bin/sh
set -e
if [ -x /usr/bin/orion-bootstrap ]; then
  /usr/bin/orion-bootstrap --ensure-env >/dev/null 2>&1 || true
fi
if command -v systemctl >/dev/null 2>&1; then
  systemctl daemon-reload >/dev/null 2>&1 || true
fi
exit 0
EOF
  chmod 0755 "$root/DEBIAN/postinst"
}

prepare_root() {
  local root="$1"
  mkdir -p \
    "$root/etc/orion" \
    "$root/usr/bin" \
    "$root/usr/lib/orion" \
    "$root/usr/lib/systemd/system" \
    "$root/var/lib/orion" \
    "$root/var/log/orion"

}

stage_common() {
  local root="$BUILD_DIR/orion-common"
  rm -rf "$root"
  prepare_root "$root"
  write_control "$root" "orion-common" "Orion shared bootstrap assets." "bash"

  install -m 0755 "$BOOTSTRAP_SRC" "$root/usr/bin/orion-bootstrap"
  install -m 0755 "$BOOTSTRAP_SRC" "$root/usr/lib/orion/orion-bootstrap"
  cat >"$root/etc/orion/orion.env" <<'EOF'
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
ORION_IMAGE_STORE_DIR=/var/lib/orion/image
ORION_NODE_AGENT_STORAGE_DIR=/var/lib/orion/node-agent
ORION_NETWORK_HOST_AGENT_STATE_DIR=/var/lib/orion/network-host-agent
ORION_VOLUME_HOST_AGENT_STATE_DIR=/var/lib/orion/volume-host-agent
OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:4318
EOF

  dpkg-deb --build "$root" "$OUT_DIR/orion-common_${VERSION}_${ARCH}.deb"
}

write_service_unit() {
  local root="$1"
  local unit="$2"
  local binary="$3"

  cat >"$root/usr/lib/systemd/system/$unit.service" <<EOF
[Unit]
Description=Orion $unit
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
EnvironmentFile=-/etc/orion/orion.env
ExecStartPre=/usr/lib/orion/orion-bootstrap --ensure-dirs
WorkingDirectory=/var/lib/orion
ExecStart=/usr/bin/$binary
Restart=on-failure
RestartSec=2s

[Install]
WantedBy=multi-user.target
EOF
}

stage_control_plane() {
  local root="$BUILD_DIR/orion-control-plane"
  rm -rf "$root"
  prepare_root "$root"
  write_control "$root" "orion-control-plane" "Orion control plane starter package." "bash, ca-certificates, orion-common (= $VERSION)"
  write_postinst "$root"

  install -m 0755 "$ROOT_DIR/bin/orion-identity" "$root/usr/bin/orion-identity"
  install -m 0755 "$ROOT_DIR/bin/orion-api" "$root/usr/bin/orion-api"
  install -m 0755 "$ROOT_DIR/bin/orion-placement" "$root/usr/bin/orion-placement"
  install -m 0755 "$ROOT_DIR/bin/orion-compute" "$root/usr/bin/orion-compute"
  install -m 0755 "$ROOT_DIR/bin/orion-image" "$root/usr/bin/orion-image"
  install -m 0755 "$ROOT_DIR/bin/orion-network" "$root/usr/bin/orion-network"
  install -m 0755 "$ROOT_DIR/bin/orion-volume" "$root/usr/bin/orion-volume"

  write_service_unit "$root" "orion-identity" "orion-identity"
  write_service_unit "$root" "orion-api" "orion-api"
  write_service_unit "$root" "orion-placement" "orion-placement"
  write_service_unit "$root" "orion-compute" "orion-compute"
  write_service_unit "$root" "orion-image" "orion-image"
  write_service_unit "$root" "orion-network" "orion-network"
  write_service_unit "$root" "orion-volume" "orion-volume"

  dpkg-deb --build "$root" "$OUT_DIR/orion-control-plane_${VERSION}_${ARCH}.deb"
}

stage_host_agents() {
  local root="$BUILD_DIR/orion-host-agents"
  rm -rf "$root"
  prepare_root "$root"
  write_control "$root" "orion-host-agents" "Orion host agents starter package." "bash, libvirt-daemon-system, libvirt-daemon-driver-qemu, libvirt-clients, qemu-system-x86, qemu-utils, openvswitch-switch, lvm2, orion-common (= $VERSION)"
  write_postinst "$root"

  install -m 0755 "$ROOT_DIR/target/debug/orion-node-agent" "$root/usr/bin/orion-node-agent"
  install -m 0755 "$ROOT_DIR/target/debug/orion-network-host-agent" "$root/usr/bin/orion-network-host-agent"
  install -m 0755 "$ROOT_DIR/target/debug/orion-volume-host-agent" "$root/usr/bin/orion-volume-host-agent"

  write_service_unit "$root" "orion-node-agent" "orion-node-agent"
  write_service_unit "$root" "orion-network-host-agent" "orion-network-host-agent"
  write_service_unit "$root" "orion-volume-host-agent" "orion-volume-host-agent"

  dpkg-deb --build "$root" "$OUT_DIR/orion-host-agents_${VERSION}_${ARCH}.deb"
}

stage_cli() {
  local root="$BUILD_DIR/orion-cli"
  rm -rf "$root"
  mkdir -p "$root/usr/bin"
  write_control "$root" "orion-cli" "Orion CLI starter package." "bash"

  install -m 0755 "$ROOT_DIR/bin/orion" "$root/usr/bin/orion"
  install -m 0755 "$ROOT_DIR/bin/orion-e2e" "$root/usr/bin/orion-e2e"

  dpkg-deb --build "$root" "$OUT_DIR/orion-cli_${VERSION}_${ARCH}.deb"
}

main() {
  require dpkg-deb
  require install

  rm -rf "$BUILD_DIR" "$OUT_DIR"
  mkdir -p "$BUILD_DIR" "$OUT_DIR"

  stage_common
  stage_control_plane
  stage_host_agents
  stage_cli

  echo "deb packages written to $OUT_DIR"
  ls -1 "$OUT_DIR"
}

main "$@"

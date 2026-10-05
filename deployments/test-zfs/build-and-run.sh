#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ORION_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

echo "=== Building ZFS plugin ==="
cd "$ORION_ROOT/plugins/zfs"
go build -o /tmp/orion-zfs-plugin ./cmd
echo "Built: /tmp/orion-zfs-plugin"

echo "=== Building Mock Compute plugin ==="
cd "$ORION_ROOT/plugins/mock-compute"
go build -o /tmp/orion-mock-compute ./cmd
echo "Built: /tmp/orion-mock-compute"

echo ""
echo "=== Creating test containers ==="

echo "Building ZFS container..."
docker build -t orion-zfs-test -f "$SCRIPT_DIR/Dockerfile.zfs" "$SCRIPT_DIR" 2>&1 | tail -5

echo ""
echo "=== Starting ZFS plugin (dev mode - bypass libvirt) ==="
docker run --rm -it \
    --privileged \
    -v /tmp/orion-zfs-plugin:/plugin/orion-zfs-plugin \
    -v /usr/bin/zfs:/usr/bin/zfs:ro \
    -v /usr/sbin/zfs:/usr/sbin/zfs:ro \
    -p 50051:50051 \
    orion-zfs-test \
    /plugin/orion-zfs-plugin

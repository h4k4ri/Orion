#!/bin/bash
set -e

echo "=== Building ZFS plugin ==="
cd /root/orion/plugins/zfs
go build -o /tmp/orion-zfs-plugin ./cmd

echo "=== Building Mock Compute plugin ==="
cd /root/orion/plugins/mock-compute
go build -o /tmp/orion-mock-compute ./cmd

echo "=== Starting ZFS plugin (PID: $$) ==="
/tmp/orion-zfs-plugin &
ZFS_PID=$!

echo "=== Starting Mock Compute plugin ==="
/tmp/orion-mock-compute &
COMPUTE_PID=$!

echo ""
echo "Plugins running:"
echo "  ZFS plugin: PID $ZFS_PID (port 50051)"
echo "  Mock Compute: PID $COMPUTE_PID (port 50052)"
echo ""
echo "To stop: kill $ZFS_PID $COMPUTE_PID"

wait

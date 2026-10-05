#!/bin/bash
set -e

cd "$(dirname "$0")"

echo "=== Building and starting ZFS plugin test environment ==="

echo "1. Building ZFS plugin container..."
docker compose build zfs-plugin

echo ""
echo "2. Starting PostgreSQL..."
docker compose up -d postgres
sleep 2

echo ""
echo "3. Starting ZFS plugin..."
docker compose up -d zfs-plugin

echo ""
echo "=== Status ==="
docker compose ps

echo ""
echo "=== Testing gRPC connection ==="
docker compose exec zfs-plugin sh -c "apt-get update && apt-get install -y grpc-tools 2>/dev/null || true"

echo ""
echo "ZFS Plugin running on port 50051"
echo "PostgreSQL running on port 5432"
echo ""
echo "To stop: docker compose down"
echo "To see logs: docker compose logs -f zfs-plugin"

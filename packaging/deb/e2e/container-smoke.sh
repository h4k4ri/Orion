#!/usr/bin/env bash
set -euo pipefail

PIDS=()
PG_VERSION=""
PG_CLUSTER=""

print_logs() {
  for log_file in /var/log/orion/*.log; do
    [ -f "$log_file" ] || continue
    echo "==> $log_file"
    tail -n 80 "$log_file" || true
  done
}

cleanup() {
  local status="$1"
  for pid in "${PIDS[@]:-}"; do
    kill "$pid" >/dev/null 2>&1 || true
  done
  if [ -n "$PG_VERSION" ] && [ -n "$PG_CLUSTER" ]; then
    pg_ctlcluster "$PG_VERSION" "$PG_CLUSTER" stop --force >/dev/null 2>&1 || true
  fi
  if [ "$status" -ne 0 ]; then
    print_logs
  fi
}

trap 'status=$?; cleanup "$status"' EXIT

wait_http() {
  local url="$1"
  local attempts="${2:-120}"
  local delay="${3:-1}"

  for _ in $(seq 1 "$attempts"); do
    if curl -fsS "$url" >/dev/null 2>&1; then
      return 0
    fi
    sleep "$delay"
  done

  echo "timeout waiting for $url" >&2
  return 1
}

wait_json() {
  local url="$1"
  local jq_expr="$2"
  local attempts="${3:-120}"

  for _ in $(seq 1 "$attempts"); do
    if curl -fsS "$url" | jq -e "$jq_expr" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done

  echo "timeout waiting for JSON condition on $url" >&2
  return 1
}

psql_orion() {
  PGPASSWORD=orion psql "postgres://orion:orion@127.0.0.1:5432/orion?sslmode=disable" "$@"
}

assert_exists() {
  local path="$1"
  [ -e "$path" ] || {
    echo "missing expected path: $path" >&2
    return 1
  }
}

postgres_setup() {
  read -r PG_VERSION PG_CLUSTER <<EOF
$(pg_lsclusters --no-header | awk 'NR == 1 {print $1, $2}')
EOF
  pg_ctlcluster "$PG_VERSION" "$PG_CLUSTER" start

  su - postgres -c "psql -v ON_ERROR_STOP=1 <<'SQL'
DO \$\$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'orion') THEN
    CREATE ROLE orion LOGIN PASSWORD 'orion';
  END IF;
END
\$\$;
SQL"

  su - postgres -c "psql -tAc \"SELECT 1 FROM pg_database WHERE datname = 'orion'\"" | grep -q 1 || \
    su - postgres -c "createdb -O orion orion"
}

prepare_legacy_schema() {
  psql_orion -v ON_ERROR_STOP=1 <<'SQL'
CREATE TABLE IF NOT EXISTS identity_users (
  id TEXT PRIMARY KEY,
  username TEXT UNIQUE NOT NULL,
  password TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS identity_projects (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS identity_user_project_roles (
  user_id TEXT NOT NULL REFERENCES identity_users(id) ON DELETE CASCADE,
  project_id TEXT NOT NULL REFERENCES identity_projects(id) ON DELETE CASCADE,
  role TEXT NOT NULL,
  PRIMARY KEY (user_id, project_id, role)
);

CREATE TABLE IF NOT EXISTS identity_user_system_roles (
  user_id TEXT NOT NULL REFERENCES identity_users(id) ON DELETE CASCADE,
  role TEXT NOT NULL,
  PRIMARY KEY (user_id, role)
);

CREATE TABLE IF NOT EXISTS identity_tokens (
  value TEXT PRIMARY KEY,
  user_id TEXT NOT NULL,
  payload JSONB NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL,
  issued_at TIMESTAMPTZ NOT NULL
);

INSERT INTO identity_users (id, username, password)
VALUES ('usr_legacy', 'admin', 'orion-admin')
ON CONFLICT (username) DO NOTHING;

INSERT INTO identity_projects (id, name)
VALUES ('proj_admin', 'admin')
ON CONFLICT (id) DO NOTHING;

CREATE TABLE IF NOT EXISTS placement_hosts (
  host_id TEXT PRIMARY KEY,
  cell_id TEXT NOT NULL DEFAULT '',
  group_name TEXT NOT NULL DEFAULT '',
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  drained BOOLEAN NOT NULL DEFAULT FALSE,
  traits TEXT[] NOT NULL DEFAULT '{}',
  vcpus_total INT NOT NULL DEFAULT 0,
  vcpus_allocated INT NOT NULL DEFAULT 0,
  memory_mb_total INT NOT NULL DEFAULT 0,
  memory_mb_allocated INT NOT NULL DEFAULT 0,
  disk_gb_total INT NOT NULL DEFAULT 0,
  disk_gb_allocated INT NOT NULL DEFAULT 0,
  generation BIGINT NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS networks (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL DEFAULT '',
  name TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'active',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS subnets (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL DEFAULT '',
  network_id TEXT NOT NULL,
  name TEXT NOT NULL,
  cidr TEXT NOT NULL,
  gateway_ip TEXT NOT NULL DEFAULT '',
  enable_dhcp BOOLEAN NOT NULL DEFAULT TRUE,
  dhcp_options_uuid TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS ports (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL DEFAULT '',
  network_id TEXT NOT NULL,
  device_id TEXT NOT NULL DEFAULT '',
  device_owner TEXT NOT NULL DEFAULT '',
  binding_host_id TEXT NOT NULL DEFAULT '',
  mac_address TEXT NOT NULL DEFAULT '',
  fixed_ips JSONB NOT NULL DEFAULT '[]',
  status TEXT NOT NULL DEFAULT 'down',
  vif_type TEXT NOT NULL DEFAULT 'ovs',
  vnic_type TEXT NOT NULL DEFAULT 'normal',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);
SQL
}

start_nats() {
  local nats_bin
  nats_bin=$(command -v nats-server)
  "$nats_bin" -js -m 8222 -a 127.0.0.1 -p 4222 >/var/log/orion/nats.log 2>&1 &
  PIDS+=("$!")
}

start_service() {
  local binary="$1"
  "/usr/bin/$binary" >/var/log/orion/"$binary".log 2>&1 &
  PIDS+=("$!")
}

main() {
  dpkg -i /opt/orion/*.deb
  /usr/bin/orion-bootstrap --ensure-env
  /usr/bin/orion-bootstrap --ensure-dirs

  assert_exists /etc/orion/orion.env
  assert_exists /var/lib/orion/image
  assert_exists /var/lib/orion/node-agent
  assert_exists /var/lib/orion/network-host-agent
  assert_exists /var/lib/orion/volume-host-agent
  assert_exists /var/log/orion

  postgres_setup
  prepare_legacy_schema
  start_nats

  set -a
  . /etc/orion/orion.env
  set +a

  start_service orion-identity
  start_service orion-placement
  start_service orion-image
  start_service orion-network
  start_service orion-volume
  start_service orion-node-agent
  start_service orion-network-host-agent
  start_service orion-volume-host-agent
  start_service orion-compute
  start_service orion-api

  wait_http http://127.0.0.1:8080/healthz
  wait_http http://127.0.0.1:8081/healthz
  wait_http http://127.0.0.1:8082/healthz
  wait_http http://127.0.0.1:8083/healthz
  wait_http http://127.0.0.1:8084/healthz
  wait_http http://127.0.0.1:8085/healthz 180
  wait_http http://127.0.0.1:8086/healthz
  wait_http http://127.0.0.1:8087/healthz
  wait_http http://127.0.0.1:8088/healthz
  wait_http http://127.0.0.1:8089/healthz
  wait_http http://127.0.0.1:8080/metrics

  wait_json http://127.0.0.1:8085/v1/images '.images | length >= 1' 180
  wait_json http://127.0.0.1:8082/v1/hosts/host_local '.host.inventory.numa | length >= 1'
  wait_json http://127.0.0.1:8082/v1/hosts/host_local '.host.traits == ["general","kvm"]'
  wait_json http://127.0.0.1:8082/v1/hosts/host_local '.host.node_agent_url == "http://127.0.0.1:8084"'
  wait_json http://127.0.0.1:8082/v1/hosts/host_local '.host.volume_host_agent_url == "http://127.0.0.1:8088"'
  wait_json http://127.0.0.1:8082/v1/hosts/host_local '.host.network_host_agent_url == "http://127.0.0.1:8089"'

  psql_orion -tAc "SELECT COUNT(*) FROM information_schema.columns WHERE table_name = 'ports' AND column_name IN ('binding_status','binding_detail')" | grep -q '^2$'
  psql_orion -tAc "SELECT COUNT(*) FROM information_schema.columns WHERE table_name = 'placement_hosts' AND column_name IN ('numa','gpus','node_agent_url','volume_host_agent_url','network_host_agent_url')" | grep -q '^5$'
  psql_orion -tAc "SELECT COUNT(*) FROM identity_user_system_roles usr JOIN identity_users u ON u.id = usr.user_id WHERE u.username = 'admin'" | grep -q '^3$'
  psql_orion -tAc "SELECT COUNT(*) FROM schema_migrations WHERE service IN ('orion-compute','orion-identity','orion-network','orion-placement','orion-volume')" | grep -Eq '^[1-9][0-9]*$'

  TOKEN=$(
    curl -fsS \
      -H 'Content-Type: application/json' \
      -d '{"username":"admin","password":"orion-admin","scope":{"type":"project","project_id":"proj_admin"}}' \
      http://127.0.0.1:8081/v1/auth/tokens | jq -r '.token.value'
  )
  [ -n "$TOKEN" ] || {
    echo "failed to obtain token" >&2
    exit 1
  }

  curl -fsS -H "Authorization: Bearer $TOKEN" http://127.0.0.1:8080/v1/servers | jq -e '.servers | type == "array"' >/dev/null
  [ "$(curl -fsS http://127.0.0.1:8082/metrics | wc -l)" -gt 0 ]

  echo "deb e2e bootstrap smoke and migration upgrade test passed"
}

main "$@"

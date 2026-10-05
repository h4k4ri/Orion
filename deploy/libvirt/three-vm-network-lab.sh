#!/usr/bin/env bash
set -euo pipefail

ACTION="${1:-help}"

LAB_NAME="${LAB_NAME:-orion-net-lab}"
LIBVIRT_URI="${LIBVIRT_URI:-qemu:///system}"
NETWORK_NAME="${NETWORK_NAME:-${LAB_NAME}-net}"
LAB_DIR="${LAB_DIR:-/var/lib/libvirt/images/${LAB_NAME}}"
BASE_IMAGE_URL="${BASE_IMAGE_URL:-https://cloud.debian.org/images/cloud/trixie/latest/debian-13-genericcloud-amd64.qcow2}"
BASE_IMAGE_NAME="${BASE_IMAGE_NAME:-$(basename "$BASE_IMAGE_URL")}"
BASE_IMAGE_PATH="${BASE_IMAGE_PATH:-${LAB_DIR}/${BASE_IMAGE_NAME}}"
VM_SSH_USER="${VM_SSH_USER:-orion}"
VM_SSH_PASSWORD="${VM_SSH_PASSWORD:-orion}"
SSH_PUBLIC_KEY_FILE="${SSH_PUBLIC_KEY_FILE:-}"
SSH_WAIT_TIMEOUT="${SSH_WAIT_TIMEOUT:-2400}"
PKG_DIR="${PKG_DIR:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)/packaging/deb/out}"
LAB_PROJECT_ID="${LAB_PROJECT_ID:-proj_admin}"

CONTROL_NAME="${CONTROL_NAME:-control-plane-01}"
CONTROL_IP="${CONTROL_IP:-10.20.0.10}"
CONTROL_MAC="${CONTROL_MAC:-52:54:00:20:00:10}"
CONTROL_VCPUS="${CONTROL_VCPUS:-2}"
CONTROL_MEMORY_MB="${CONTROL_MEMORY_MB:-2048}"
CONTROL_DISK_GB="${CONTROL_DISK_GB:-40}"

EDGE1_NAME="${EDGE1_NAME:-host-edge-01}"
EDGE1_IP="${EDGE1_IP:-10.20.0.21}"
EDGE1_MAC="${EDGE1_MAC:-52:54:00:20:00:21}"
EDGE1_VCPUS="${EDGE1_VCPUS:-2}"
EDGE1_MEMORY_MB="${EDGE1_MEMORY_MB:-2048}"
EDGE1_DISK_GB="${EDGE1_DISK_GB:-60}"
EDGE1_DATA_DISK_GB="${EDGE1_DATA_DISK_GB:-20}"

EDGE2_NAME="${EDGE2_NAME:-host-edge-02}"
EDGE2_IP="${EDGE2_IP:-10.20.0.22}"
EDGE2_MAC="${EDGE2_MAC:-52:54:00:20:00:22}"
EDGE2_VCPUS="${EDGE2_VCPUS:-2}"
EDGE2_MEMORY_MB="${EDGE2_MEMORY_MB:-2048}"
EDGE2_DISK_GB="${EDGE2_DISK_GB:-60}"
EDGE2_DATA_DISK_GB="${EDGE2_DATA_DISK_GB:-20}"

VIRT_TYPE="${VIRT_TYPE:-}"

SSH_COMMON_OPTS=(
  -o StrictHostKeyChecking=no
  -o UserKnownHostsFile=/dev/null
  -o ConnectTimeout=5
)

usage() {
  cat <<'EOF'
Usage:
  three-vm-network-lab.sh up
  three-vm-network-lab.sh install
  three-vm-network-lab.sh verify-network
  three-vm-network-lab.sh all
  three-vm-network-lab.sh down
  three-vm-network-lab.sh destroy
  three-vm-network-lab.sh status

What it validates:
  - control plane + 2 edge VMs on Debian 13
  - orion-network creating one logical network and ports bound to 2 different hosts
  - each orion-network-host-agent materializing only its own bound ports
  - the same logical network appearing on both edge hosts

Important scope:
  - this script validates Orion's current network replication semantics
  - it does not validate full multi-host guest boot through orion-compute yet,
    because compute still dispatches to a single node-agent URL today

Requirements:
  - host with libvirt/virt-install/qemu-img/cloud-localds
  - built Orion .deb packages in packaging/deb/out
  - outbound internet for Debian package install and CirrOS download
EOF
}

log() {
  printf '[orion-net-lab] %s\n' "$*"
}

die() {
  printf '[orion-net-lab] error: %s\n' "$*" >&2
  exit 1
}

run_root() {
  if [[ "${EUID}" -eq 0 ]]; then
    "$@"
  else
    sudo "$@"
  fi
}

run_virsh() {
  run_root virsh -c "$LIBVIRT_URI" "$@"
}

run_virt_install() {
  run_root virt-install --connect "$LIBVIRT_URI" "$@"
}

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "required command not found: $1"
}

default_pubkey_file() {
  local candidate
  for candidate in \
    "${HOME}/.ssh/id_ed25519.pub" \
    "${HOME}/.ssh/id_ecdsa.pub" \
    "${HOME}/.ssh/id_rsa.pub"; do
    if [[ -f "$candidate" ]]; then
      printf '%s\n' "$candidate"
      return
    fi
  done
}

load_ssh_public_key() {
  local key_file="${SSH_PUBLIC_KEY_FILE}"
  if [[ -z "$key_file" ]]; then
    key_file="$(default_pubkey_file || true)"
  fi
  if [[ -n "$key_file" && -f "$key_file" ]]; then
    tr -d '\n' <"$key_file"
  fi
}

SSH_PUBLIC_KEY="$(load_ssh_public_key || true)"

detect_virt_type() {
  if [[ -n "$VIRT_TYPE" ]]; then
    printf '%s\n' "$VIRT_TYPE"
    return
  fi
  if [[ -e /dev/kvm ]]; then
    printf 'kvm\n'
  else
    printf 'qemu\n'
  fi
}

yaml_list() {
  local indent="$1"
  shift
  local prefix
  prefix="$(printf '%*s' "$indent" '')"
  local item
  for item in "$@"; do
    printf '%s- %s\n' "$prefix" "$item"
  done
}

indent() {
  local spaces="$1"
  local prefix
  prefix="$(printf '%*s' "$spaces" '')"
  sed "s/^/${prefix}/"
}

ssh_key_yaml() {
  if [[ -n "$SSH_PUBLIC_KEY" ]]; then
    printf '    ssh_authorized_keys:\n'
    printf '      - %s\n' "$SSH_PUBLIC_KEY"
  fi
}

ensure_prereqs() {
  need_cmd curl
  need_cmd jq
  need_cmd qemu-img
  need_cmd virsh
  need_cmd virt-install
  need_cmd cloud-localds
  need_cmd mktemp
  need_cmd awk
  need_cmd sed
  need_cmd ssh
  need_cmd scp
  if [[ -z "$SSH_PUBLIC_KEY" ]]; then
    need_cmd sshpass
  fi
}

ensure_lab_dir() {
  run_root install -d -m 0755 "$LAB_DIR"
}

download_base_image() {
  if [[ -f "$BASE_IMAGE_PATH" ]]; then
    log "base image already present: $BASE_IMAGE_PATH"
    return
  fi

  log "downloading Debian 13 cloud image"
  local tmp
  tmp="$(mktemp)"
  curl -fsSL "$BASE_IMAGE_URL" -o "$tmp"
  run_root mv "$tmp" "$BASE_IMAGE_PATH"
  run_root chmod 0644 "$BASE_IMAGE_PATH"
}

network_xml() {
  cat <<EOF
<network>
  <name>${NETWORK_NAME}</name>
  <forward mode='nat'/>
  <domain name='${NETWORK_NAME}' localOnly='yes'/>
  <ip address='10.20.0.1' netmask='255.255.255.0'>
    <dhcp>
      <range start='10.20.0.100' end='10.20.0.220'/>
      <host mac='${CONTROL_MAC}' name='${CONTROL_NAME}' ip='${CONTROL_IP}'/>
      <host mac='${EDGE1_MAC}' name='${EDGE1_NAME}' ip='${EDGE1_IP}'/>
      <host mac='${EDGE2_MAC}' name='${EDGE2_NAME}' ip='${EDGE2_IP}'/>
    </dhcp>
  </ip>
</network>
EOF
}

ensure_network() {
  if run_virsh net-info "$NETWORK_NAME" >/dev/null 2>&1; then
    if ! run_virsh net-info "$NETWORK_NAME" | grep -q 'Active:.*yes'; then
      run_virsh net-start "$NETWORK_NAME" >/dev/null 2>&1 || true
    fi
    run_virsh net-autostart "$NETWORK_NAME" >/dev/null 2>&1 || true
    return
  fi

  log "creating libvirt network $NETWORK_NAME"
  local xml
  xml="$(mktemp)"
  network_xml >"$xml"
  run_virsh net-define "$xml" >/dev/null
  run_virsh net-start "$NETWORK_NAME" >/dev/null
  run_virsh net-autostart "$NETWORK_NAME" >/dev/null
  rm -f "$xml"
}

vm_exists() {
  run_virsh dominfo "$1" >/dev/null 2>&1
}

vm_state() {
  run_virsh domstate "$1" 2>/dev/null | awk 'NR == 1 {print $0}'
}

wait_for_vm_state() {
  local name="$1"
  local wanted="$2"
  local attempts="${3:-60}"
  local i state
  for ((i = 0; i < attempts; i++)); do
    state="$(vm_state "$name" || true)"
    if [[ "$state" == "$wanted" ]]; then
      return 0
    fi
    sleep 2
  done
  die "timeout waiting for $name to reach state: $wanted"
}

ssh_cmd() {
  if [[ -z "$SSH_PUBLIC_KEY" ]]; then
    sshpass -p "$VM_SSH_PASSWORD" ssh "${SSH_COMMON_OPTS[@]}" "$@"
  else
    ssh "${SSH_COMMON_OPTS[@]}" "$@"
  fi
}

scp_cmd() {
  if [[ -z "$SSH_PUBLIC_KEY" ]]; then
    sshpass -p "$VM_SSH_PASSWORD" scp "${SSH_COMMON_OPTS[@]}" "$@"
  else
    scp "${SSH_COMMON_OPTS[@]}" "$@"
  fi
}

wait_for_ssh() {
  local ip="$1"
  local deadline
  deadline=$((SECONDS + SSH_WAIT_TIMEOUT))
  log "waiting for SSH on ${ip} (timeout: ${SSH_WAIT_TIMEOUT}s)"
  while ((SECONDS < deadline)); do
    if [[ -n "$SSH_PUBLIC_KEY" ]]; then
      if ssh "${SSH_COMMON_OPTS[@]}" -o BatchMode=yes "${VM_SSH_USER}@${ip}" true >/dev/null 2>&1; then
        return 0
      fi
    else
      if sshpass -p "$VM_SSH_PASSWORD" ssh "${SSH_COMMON_OPTS[@]}" "${VM_SSH_USER}@${ip}" true >/dev/null 2>&1; then
        return 0
      fi
    fi
    sleep 5
  done
  return 1
}

wait_for_dhcp_lease() {
  local ip="$1"
  local deadline
  deadline=$((SECONDS + SSH_WAIT_TIMEOUT))
  log "waiting for DHCP lease for ${ip} (timeout: ${SSH_WAIT_TIMEOUT}s)"
  while ((SECONDS < deadline)); do
    if run_virsh net-dhcp-leases "$NETWORK_NAME" | grep -qF "$ip"; then
      return 0
    fi
    sleep 5
  done
  return 1
}

wait_http() {
  local url="$1"
  local attempts="${2:-60}"
  local delay="${3:-2}"
  local i
  for ((i = 0; i < attempts; i++)); do
    if curl -fsS "$url" >/dev/null 2>&1; then
      return 0
    fi
    sleep "$delay"
  done
  return 1
}

remote_exec() {
  local ip="$1"
  shift
  ssh_cmd "${VM_SSH_USER}@${ip}" "$@"
}

copy_to_vm() {
  local src="$1"
  local ip="$2"
  local dst="$3"
  scp_cmd "$src" "${VM_SSH_USER}@${ip}:${dst}"
}

write_remote_file() {
  local ip="$1"
  local path="$2"
  local mode="$3"
  local tmp_local tmp_remote
  tmp_local="$(mktemp)"
  tmp_remote="/tmp/$(basename "$path").$$"
  cat >"$tmp_local"
  scp_cmd "$tmp_local" "${VM_SSH_USER}@${ip}:${tmp_remote}"
  remote_exec "$ip" "sudo install -D -m ${mode} ${tmp_remote} ${path} && rm -f ${tmp_remote}"
  rm -f "$tmp_local"
}

control_plane_init_script() {
  cat <<'EOF'
#!/usr/bin/env bash
set -euxo pipefail
systemctl enable --now qemu-guest-agent || true
systemctl enable --now postgresql || true
systemctl enable --now nats-server || true
systemctl enable --now openvswitch-switch || true
systemctl enable --now ovn-central || true
systemctl enable --now ovn-northd || true

if command -v psql >/dev/null 2>&1; then
  sudo -u postgres psql -v ON_ERROR_STOP=1 <<'SQL'
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'orion') THEN
    CREATE ROLE orion LOGIN PASSWORD 'orion';
  END IF;
END
$$;
SQL

  if ! sudo -u postgres psql -tAc "SELECT 1 FROM pg_database WHERE datname = 'orion'" | grep -q 1; then
    sudo -u postgres createdb -O orion orion
  fi
fi

install -d -m 0755 /srv/orion/image
EOF
}

edge_init_script() {
  cat <<'EOF'
#!/usr/bin/env bash
set -euxo pipefail
systemctl enable --now qemu-guest-agent || true
systemctl enable --now libvirtd || true
systemctl enable --now openvswitch-switch || true
EOF
}

cloud_user_data() {
  local name="$1"
  local role="$2"
  local package_lines init_script

  case "$role" in
    control-plane)
      package_lines="$(yaml_list 2 \
        qemu-guest-agent \
        ca-certificates \
        curl \
        jq \
        procps \
        sudo \
        nats-server \
        postgresql \
        openvswitch-switch \
        ovn-central)"
      init_script="$(control_plane_init_script)"
      ;;
    edge)
      package_lines="$(yaml_list 2 \
        qemu-guest-agent \
        ca-certificates \
        curl \
        jq \
        procps \
        sudo \
        lvm2 \
        libvirt-daemon-system \
        libvirt-daemon-driver-qemu \
        libvirt-clients \
        qemu-system-x86 \
        qemu-utils \
        openvswitch-switch)"
      init_script="$(edge_init_script)"
      ;;
    *)
      die "unknown role: $role"
      ;;
  esac

  cat <<EOF
#cloud-config
hostname: ${name}
fqdn: ${name}
manage_etc_hosts: true
package_update: true
package_upgrade: false
ssh_pwauth: true
users:
  - default
  - name: ${VM_SSH_USER}
    gecos: Orion Lab User
    groups: [sudo, adm, systemd-journal]
    shell: /bin/bash
    sudo: ALL=(ALL) NOPASSWD:ALL
$(ssh_key_yaml)
chpasswd:
  expire: false
  users:
    - name: ${VM_SSH_USER}
      password: ${VM_SSH_PASSWORD}
      type: text
packages:
${package_lines}
write_files:
  - path: /usr/local/sbin/orion-lab-init.sh
    owner: root:root
    permissions: '0755'
    content: |
$(printf '%s\n' "$init_script" | indent 6)
runcmd:
  - [ bash, -lc, "/usr/local/sbin/orion-lab-init.sh" ]
final_message: "${name} role=${role} is ready after cloud-init."
EOF
}

cloud_meta_data() {
  local name="$1"
  cat <<EOF
instance-id: ${LAB_NAME}-${name}
local-hostname: ${name}
EOF
}

create_seed_image() {
  local name="$1"
  local role="$2"
  local workdir seed_path
  workdir="$(mktemp -d)"
  seed_path="${LAB_DIR}/${name}-seed.iso"
  cloud_user_data "$name" "$role" >"${workdir}/user-data"
  cloud_meta_data "$name" >"${workdir}/meta-data"
  run_root rm -f "$seed_path"
  run_root cloud-localds "$seed_path" "${workdir}/user-data" "${workdir}/meta-data"
  rm -rf "$workdir"
  printf '%s\n' "$seed_path"
}

create_overlay_disk() {
  local path="$1"
  local size_gb="$2"
  if [[ -f "$path" ]]; then
    return
  fi
  run_root qemu-img create -f qcow2 -F qcow2 -b "$BASE_IMAGE_PATH" "$path" "${size_gb}G" >/dev/null
}

create_data_disk() {
  local path="$1"
  local size_gb="$2"
  if [[ -f "$path" ]]; then
    return
  fi
  run_root qemu-img create -f qcow2 "$path" "${size_gb}G" >/dev/null
}

create_vm() {
  local name="$1"
  local role="$2"
  local ip="$3"
  local mac="$4"
  local vcpus="$5"
  local memory_mb="$6"
  local disk_gb="$7"
  local extra_disk_gb="${8:-}"
  local seed_path disk_path data_path

  if vm_exists "$name"; then
    if [[ "$(vm_state "$name" || true)" == "shut off" ]]; then
      log "domain $name is shut off (likely from a failed run) — destroying for fresh creation"
      destroy_vm "$name"
      run_root rm -f \
        "${LAB_DIR}/${name}.qcow2" \
        "${LAB_DIR}/${name}-seed.iso" \
        "${LAB_DIR}/${name}-data.qcow2" || true
    else
      log "domain already exists and is running: $name"
      return
    fi
  fi

  seed_path="$(create_seed_image "$name" "$role")"
  disk_path="${LAB_DIR}/${name}.qcow2"
  create_overlay_disk "$disk_path" "$disk_gb"

  if [[ -n "$extra_disk_gb" ]]; then
    data_path="${LAB_DIR}/${name}-data.qcow2"
    create_data_disk "$data_path" "$extra_disk_gb"
  fi

  local _virt_type
  _virt_type="$(detect_virt_type)"
  local _cpu_flag
  if [[ "$_virt_type" == "kvm" ]]; then
    _cpu_flag="host-passthrough,cache.mode=passthrough"
  else
    _cpu_flag="host-model,check=none"
  fi

  log "creating domain $name (virt-type=${_virt_type} cpu=${_cpu_flag})"
  local args=(
    --name "$name"
    --virt-type "$_virt_type"
    --memory "$memory_mb"
    --vcpus "$vcpus"
    --cpu "$_cpu_flag"
    --import
    --osinfo detect=on,require=off
    --graphics none
    --noautoconsole
    --rng /dev/urandom
    --channel unix,target_type=virtio,name=org.qemu.guest_agent.0
    --network "network=${NETWORK_NAME},model=virtio,mac=${mac}"
    --disk "path=${disk_path},format=qcow2,bus=virtio"
    --disk "path=${seed_path},device=cdrom"
  )
  if [[ -n "${data_path:-}" ]]; then
    args+=(--disk "path=${data_path},format=qcow2,bus=virtio")
  fi
  run_virt_install "${args[@]}"
}

start_vm_if_needed() {
  local name="$1"
  local state
  state="$(vm_state "$name" || true)"
  if [[ "$state" != "running" ]]; then
    run_virsh start "$name" >/dev/null
  fi
}

shutdown_vm_if_running() {
  local name="$1"
  if [[ "$(vm_state "$name" || true)" == "running" ]]; then
    run_virsh shutdown "$name" >/dev/null || true
  fi
}

destroy_vm() {
  local name="$1"
  if ! vm_exists "$name"; then
    return
  fi
  if [[ "$(vm_state "$name" || true)" != "shut off" ]]; then
    run_virsh destroy "$name" >/dev/null || true
  fi
  run_virsh undefine "$name" --nvram >/dev/null 2>&1 || run_virsh undefine "$name" >/dev/null 2>&1 || true
}

up() {
  ensure_prereqs
  ensure_lab_dir
  download_base_image
  ensure_network

  create_vm "$CONTROL_NAME" control-plane "$CONTROL_IP" "$CONTROL_MAC" "$CONTROL_VCPUS" "$CONTROL_MEMORY_MB" "$CONTROL_DISK_GB"
  create_vm "$EDGE1_NAME" edge "$EDGE1_IP" "$EDGE1_MAC" "$EDGE1_VCPUS" "$EDGE1_MEMORY_MB" "$EDGE1_DISK_GB" "$EDGE1_DATA_DISK_GB"
  create_vm "$EDGE2_NAME" edge "$EDGE2_IP" "$EDGE2_MAC" "$EDGE2_VCPUS" "$EDGE2_MEMORY_MB" "$EDGE2_DISK_GB" "$EDGE2_DATA_DISK_GB"

  start_vm_if_needed "$CONTROL_NAME"
  start_vm_if_needed "$EDGE1_NAME"
  start_vm_if_needed "$EDGE2_NAME"

  wait_for_vm_state "$CONTROL_NAME" running
  wait_for_vm_state "$EDGE1_NAME" running
  wait_for_vm_state "$EDGE2_NAME" running

  log "VMs started; SSH will be polled by install step (cloud-init may take 5-20 min on QEMU)"

  cat <<EOF

Lab ready:
  control plane: ${CONTROL_NAME} ${CONTROL_IP}
  edge host 01 : ${EDGE1_NAME} ${EDGE1_IP}
  edge host 02 : ${EDGE2_NAME} ${EDGE2_IP}

Next:
  ./deploy/libvirt/three-vm-network-lab.sh install
  ./deploy/libvirt/three-vm-network-lab.sh verify-network
EOF
}

require_package() {
  local pattern="$1"
  local match
  match="$(compgen -G "${PKG_DIR}/${pattern}" | head -n 1 || true)"
  [[ -n "$match" ]] || die "package not found: ${PKG_DIR}/${pattern}"
  printf '%s\n' "$match"
}

package_common() { require_package 'orion-common_*_amd64.deb'; }
package_control_plane() { require_package 'orion-control-plane_*_amd64.deb'; }
package_cli() { require_package 'orion-cli_*_amd64.deb'; }
package_host_agents() { require_package 'orion-host-agents_*_amd64.deb'; }

copy_packages_to_vm() {
  local ip="$1"
  shift
  remote_exec "$ip" "mkdir -p ~/orion-packages"
  local pkg
  for pkg in "$@"; do
    copy_to_vm "$pkg" "$ip" "~/orion-packages/"
  done
}

configure_control_plane() {
  local common_pkg control_pkg cli_pkg
  common_pkg="$(package_common)"
  control_pkg="$(package_control_plane)"
  cli_pkg="$(package_cli)"

  copy_packages_to_vm "$CONTROL_IP" "$common_pkg" "$control_pkg" "$cli_pkg"

  remote_exec "$CONTROL_IP" "sudo dpkg -i ~/orion-packages/$(basename "$common_pkg") ~/orion-packages/$(basename "$control_pkg") ~/orion-packages/$(basename "$cli_pkg")"
  remote_exec "$CONTROL_IP" "sudo orion-bootstrap --ensure-env && sudo orion-bootstrap --ensure-dirs"

  write_remote_file "$CONTROL_IP" /etc/orion/orion.env 0644 <<EOF
ORION_DB_DSN=postgres://orion:orion@127.0.0.1:5432/orion?sslmode=disable
ORION_NATS_URL=nats://127.0.0.1:4222
ORION_IDENTITY_URL=http://127.0.0.1:8081
ORION_PLACEMENT_URL=http://127.0.0.1:8082
ORION_COMPUTE_URL=http://127.0.0.1:8083
ORION_IMAGE_URL=http://127.0.0.1:8085
ORION_NETWORK_URL=http://127.0.0.1:8086
ORION_VOLUME_URL=http://127.0.0.1:8087
ORION_IMAGE_STORE_DIR=/srv/orion/image
ORION_NETWORK_OVN_NB_DB=unix:/var/run/ovn/ovnnb_db.sock
EOF

  remote_exec "$CONTROL_IP" "sudo systemctl daemon-reload"
  remote_exec "$CONTROL_IP" "sudo systemctl restart postgresql nats-server openvswitch-switch"
  remote_exec "$CONTROL_IP" "sudo systemctl restart ovn-central || true"
  remote_exec "$CONTROL_IP" "sudo systemctl restart ovn-northd || true"
  remote_exec "$CONTROL_IP" "sudo systemctl restart orion-identity orion-placement orion-image orion-network orion-volume orion-compute orion-api"
}

configure_edge() {
  local ip="$1"
  local host_id="$2"
  local common_pkg host_pkg
  common_pkg="$(package_common)"
  host_pkg="$(package_host_agents)"

  copy_packages_to_vm "$ip" "$common_pkg" "$host_pkg"
  remote_exec "$ip" "sudo dpkg -i ~/orion-packages/$(basename "$common_pkg") ~/orion-packages/$(basename "$host_pkg")"
  remote_exec "$ip" "sudo orion-bootstrap --ensure-env && sudo orion-bootstrap --ensure-dirs"

  write_remote_file "$ip" /etc/orion/orion.env 0644 <<EOF
ORION_NODE_AGENT_LISTEN_ADDR=0.0.0.0:8084
ORION_NETWORK_HOST_AGENT_LISTEN_ADDR=0.0.0.0:8087
ORION_VOLUME_HOST_AGENT_LISTEN_ADDR=0.0.0.0:8088
ORION_NETWORK_URL=http://${CONTROL_IP}:8086
ORION_NATS_URL=nats://${CONTROL_IP}:4222
ORION_NETWORK_HOST_ID=${host_id}
ORION_NODE_AGENT_STORAGE_DIR=/var/lib/orion/node-agent
ORION_NETWORK_HOST_AGENT_STATE_DIR=/var/lib/orion/network-host-agent
ORION_VOLUME_HOST_AGENT_STATE_DIR=/var/lib/orion/volume-host-agent
EOF

  remote_exec "$ip" "sudo systemctl daemon-reload"
  remote_exec "$ip" "sudo systemctl restart libvirtd openvswitch-switch"
  remote_exec "$ip" "sudo systemctl restart orion-node-agent orion-network-host-agent orion-volume-host-agent"
}

register_hosts() {
  local payload
  for payload in \
    "{\"host_id\":\"${EDGE1_NAME}\",\"cell_id\":\"cell_local\",\"group\":\"general\",\"enabled\":true,\"drained\":false,\"node_agent_url\":\"http://${EDGE1_IP}:8084\",\"network_host_agent_url\":\"http://${EDGE1_IP}:8087\",\"volume_host_agent_url\":\"http://${EDGE1_IP}:8088\",\"traits\":[\"general\",\"edge-01\"],\"vcpus_total\":8,\"memory_mb_total\":16384,\"disk_gb_total\":100}" \
    "{\"host_id\":\"${EDGE2_NAME}\",\"cell_id\":\"cell_local\",\"group\":\"general\",\"enabled\":true,\"drained\":false,\"node_agent_url\":\"http://${EDGE2_IP}:8084\",\"network_host_agent_url\":\"http://${EDGE2_IP}:8087\",\"volume_host_agent_url\":\"http://${EDGE2_IP}:8088\",\"traits\":[\"general\",\"edge-02\"],\"vcpus_total\":8,\"memory_mb_total\":16384,\"disk_gb_total\":100}"; do
    curl -fsS \
      -H 'Content-Type: application/json' \
      -d "$payload" \
      "http://${CONTROL_IP}:8082/v1/hosts" >/dev/null
  done

  curl -fsS -X POST "http://${CONTROL_IP}:8082/v1/hosts/host_local/disable" >/dev/null || true
}

install_orion() {
  ensure_prereqs

  log "waiting for SSH on all VMs (shared ${SSH_WAIT_TIMEOUT}s deadline — may take 5-20 min on QEMU)"
  local _ssh_deadline=$(( SECONDS + SSH_WAIT_TIMEOUT ))
  local _remaining
  for _ip in "$CONTROL_IP" "$EDGE1_IP" "$EDGE2_IP"; do
    _remaining=$(( _ssh_deadline - SECONDS ))
    if (( _remaining <= 0 )); then
      die "SSH deadline exceeded before reaching ${_ip}"
    fi
    local _saved_timeout=$SSH_WAIT_TIMEOUT
    SSH_WAIT_TIMEOUT=$_remaining
    wait_for_ssh "$_ip" || die "SSH not available on ${_ip} after ${_saved_timeout}s — aborting install"
    SSH_WAIT_TIMEOUT=$_saved_timeout
  done

  log "waiting for cloud-init to complete on all VMs"
  remote_exec "$CONTROL_IP" "sudo cloud-init status --wait 2>/dev/null || true"
  remote_exec "$EDGE1_IP"   "sudo cloud-init status --wait 2>/dev/null || true"
  remote_exec "$EDGE2_IP"   "sudo cloud-init status --wait 2>/dev/null || true"

  configure_control_plane
  configure_edge "$EDGE1_IP" "$EDGE1_NAME"
  configure_edge "$EDGE2_IP" "$EDGE2_NAME"

  wait_http "http://${CONTROL_IP}:8081/healthz" 90 2 || die "identity not healthy"
  wait_http "http://${CONTROL_IP}:8082/healthz" 90 2 || die "placement not healthy"
  wait_http "http://${CONTROL_IP}:8086/healthz" 90 2 || die "network not healthy"
  wait_http "http://${EDGE1_IP}:8087/healthz" 90 2 || die "edge1 network-host-agent not healthy"
  wait_http "http://${EDGE2_IP}:8087/healthz" 90 2 || die "edge2 network-host-agent not healthy"

  register_hosts

  cat <<EOF

Orion installed for network replication validation.

Endpoints:
  placement:            http://${CONTROL_IP}:8082
  network:              http://${CONTROL_IP}:8086
  network-host-agent-1: http://${EDGE1_IP}:8087
  network-host-agent-2: http://${EDGE2_IP}:8087
EOF
}

wait_jq_condition() {
  local url="$1"
  local expr="$2"
  local attempts="${3:-60}"
  local delay="${4:-2}"
  local i
  for ((i = 0; i < attempts; i++)); do
    if curl -fsS "$url" | jq -e "$expr" >/dev/null 2>&1; then
      return 0
    fi
    sleep "$delay"
  done
  return 1
}

verify_network() {
  ensure_prereqs
  wait_http "http://${CONTROL_IP}:8086/healthz" 30 2 || die "network service not healthy"
  wait_http "http://${EDGE1_IP}:8087/healthz" 30 2 || die "edge1 network-host-agent not healthy"
  wait_http "http://${EDGE2_IP}:8087/healthz" 30 2 || die "edge2 network-host-agent not healthy"

  local suffix network_json subnet_json port1_json port2_json network_id subnet_id port1_id port2_id cidr gateway
  suffix="$(date +%s)"
  cidr="10.210.$((suffix % 200)).0/24"
  gateway="${cidr%0/24}1"

  network_json="$(curl -fsS \
    -H 'Content-Type: application/json' \
    -d "{\"project_id\":\"${LAB_PROJECT_ID}\",\"name\":\"lab-net-${suffix}\"}" \
    "http://${CONTROL_IP}:8086/v1/networks")"
  network_id="$(jq -r '.network.id' <<<"$network_json")"
  [[ -n "$network_id" && "$network_id" != "null" ]] || die "failed to create network"

  subnet_json="$(curl -fsS \
    -H 'Content-Type: application/json' \
    -d "{\"project_id\":\"${LAB_PROJECT_ID}\",\"network_id\":\"${network_id}\",\"name\":\"lab-subnet-${suffix}\",\"cidr\":\"${cidr}\",\"gateway_ip\":\"${gateway}\",\"enable_dhcp\":true}" \
    "http://${CONTROL_IP}:8086/v1/subnets")"
  subnet_id="$(jq -r '.subnet.id' <<<"$subnet_json")"
  [[ -n "$subnet_id" && "$subnet_id" != "null" ]] || die "failed to create subnet"

  port1_json="$(curl -fsS \
    -H 'Content-Type: application/json' \
    -d "{\"project_id\":\"${LAB_PROJECT_ID}\",\"network_id\":\"${network_id}\",\"device_id\":\"probe-${suffix}-a\",\"device_owner\":\"lab:network-test\",\"binding_host_id\":\"${EDGE1_NAME}\"}" \
    "http://${CONTROL_IP}:8086/v1/ports")"
  port1_id="$(jq -r '.port.id' <<<"$port1_json")"
  [[ -n "$port1_id" && "$port1_id" != "null" ]] || die "failed to create port 1"

  port2_json="$(curl -fsS \
    -H 'Content-Type: application/json' \
    -d "{\"project_id\":\"${LAB_PROJECT_ID}\",\"network_id\":\"${network_id}\",\"device_id\":\"probe-${suffix}-b\",\"device_owner\":\"lab:network-test\",\"binding_host_id\":\"${EDGE2_NAME}\"}" \
    "http://${CONTROL_IP}:8086/v1/ports")"
  port2_id="$(jq -r '.port.id' <<<"$port2_json")"
  [[ -n "$port2_id" && "$port2_id" != "null" ]] || die "failed to create port 2"

  wait_jq_condition "http://${CONTROL_IP}:8086/v1/ports/${port1_id}" '.port.binding_host_id == "'"${EDGE1_NAME}"'" and (.port.binding_status == "ready" or .port.binding_status == "pending")' 60 2 || die "port 1 did not reconcile"
  wait_jq_condition "http://${CONTROL_IP}:8086/v1/ports/${port2_id}" '.port.binding_host_id == "'"${EDGE2_NAME}"'" and (.port.binding_status == "ready" or .port.binding_status == "pending")' 60 2 || die "port 2 did not reconcile"

  wait_jq_condition "http://${EDGE1_IP}:8087/v1/networks" '[.[] | select(.id == "'"${network_id}"'" and (.port_ids | index("'"${port1_id}"'")))] | length >= 1' 60 2 || die "edge1 did not materialize the shared network"
  wait_jq_condition "http://${EDGE2_IP}:8087/v1/networks" '[.[] | select(.id == "'"${network_id}"'" and (.port_ids | index("'"${port2_id}"'")))] | length >= 1' 60 2 || die "edge2 did not materialize the shared network"

  wait_jq_condition "http://${EDGE1_IP}:8087/v1/ports" '[.[] | select(.id == "'"${port1_id}"'" and .network_id == "'"${network_id}"'" and .binding_host_id == "'"${EDGE1_NAME}"'")] | length == 1' 60 2 || die "edge1 did not materialize its bound port"
  wait_jq_condition "http://${EDGE2_IP}:8087/v1/ports" '[.[] | select(.id == "'"${port2_id}"'" and .network_id == "'"${network_id}"'" and .binding_host_id == "'"${EDGE2_NAME}"'")] | length == 1' 60 2 || die "edge2 did not materialize its bound port"

  if curl -fsS "http://${EDGE1_IP}:8087/v1/ports" | jq -e '[.[] | select(.id == "'"${port2_id}"'")] | length == 0' >/dev/null 2>&1; then
    log "edge1 correctly ignored the port bound to edge2"
  else
    die "edge1 should not materialize port bound to edge2"
  fi

  if curl -fsS "http://${EDGE2_IP}:8087/v1/ports" | jq -e '[.[] | select(.id == "'"${port1_id}"'")] | length == 0' >/dev/null 2>&1; then
    log "edge2 correctly ignored the port bound to edge1"
  else
    die "edge2 should not materialize port bound to edge1"
  fi

  cat <<EOF

Network replication validation passed.

Shared logical network:
  network_id: ${network_id}
  subnet_id:  ${subnet_id}

Host-bound ports:
  ${EDGE1_NAME}: ${port1_id}
  ${EDGE2_NAME}: ${port2_id}

What this proved:
  - the same project network was materialized on both edge hosts
  - each host agent reconciled only the ports bound to itself
  - Orion network state is distributed by binding_host_id, not by a single "main machine"

What this script still does not cover:
  - full image/compute boot end-to-end across both edges
EOF
}

down() {
  shutdown_vm_if_running "$CONTROL_NAME"
  shutdown_vm_if_running "$EDGE1_NAME"
  shutdown_vm_if_running "$EDGE2_NAME"
}

destroy() {
  destroy_vm "$CONTROL_NAME"
  destroy_vm "$EDGE1_NAME"
  destroy_vm "$EDGE2_NAME"
  if run_virsh net-info "$NETWORK_NAME" >/dev/null 2>&1; then
    run_virsh net-destroy "$NETWORK_NAME" >/dev/null 2>&1 || true
    run_virsh net-undefine "$NETWORK_NAME" >/dev/null 2>&1 || true
  fi
  run_root rm -f \
    "${LAB_DIR}/${CONTROL_NAME}.qcow2" \
    "${LAB_DIR}/${CONTROL_NAME}-seed.iso" \
    "${LAB_DIR}/${EDGE1_NAME}.qcow2" \
    "${LAB_DIR}/${EDGE1_NAME}-seed.iso" \
    "${LAB_DIR}/${EDGE1_NAME}-data.qcow2" \
    "${LAB_DIR}/${EDGE2_NAME}.qcow2" \
    "${LAB_DIR}/${EDGE2_NAME}-seed.iso" \
    "${LAB_DIR}/${EDGE2_NAME}-data.qcow2"
}

status() {
  printf '%-20s %-15s %-15s\n' "domain" "ip" "state"
  printf '%-20s %-15s %-15s\n' "$CONTROL_NAME" "$CONTROL_IP" "$(vm_state "$CONTROL_NAME" || echo missing)"
  printf '%-20s %-15s %-15s\n' "$EDGE1_NAME" "$EDGE1_IP" "$(vm_state "$EDGE1_NAME" || echo missing)"
  printf '%-20s %-15s %-15s\n' "$EDGE2_NAME" "$EDGE2_IP" "$(vm_state "$EDGE2_NAME" || echo missing)"
}

all() {
  up
  install_orion
  verify_network
}

case "$ACTION" in
  up)
    up
    ;;
  install)
    install_orion
    ;;
  verify-network)
    verify_network
    ;;
  all)
    all
    ;;
  down)
    down
    ;;
  destroy)
    destroy
    ;;
  status)
    status
    ;;
  help|-h|--help)
    usage
    ;;
  *)
    usage >&2
    exit 1
    ;;
esac

#!/usr/bin/env bash
set -euo pipefail

ACTION="${1:-up}"

LAB_NAME="${LAB_NAME:-orion-lab}"
LIBVIRT_URI="${LIBVIRT_URI:-qemu:///system}"
NETWORK_NAME="${NETWORK_NAME:-${LAB_NAME}-net}"
LAB_DIR="${LAB_DIR:-/var/lib/libvirt/images/${LAB_NAME}}"
BASE_IMAGE_URL="${BASE_IMAGE_URL:-https://cloud.debian.org/images/cloud/trixie/latest/debian-13-genericcloud-amd64.qcow2}"
BASE_IMAGE_NAME="${BASE_IMAGE_NAME:-$(basename "$BASE_IMAGE_URL")}"
BASE_IMAGE_PATH="${BASE_IMAGE_PATH:-${LAB_DIR}/${BASE_IMAGE_NAME}}"
VM_SSH_USER="${VM_SSH_USER:-orion}"
VM_SSH_PASSWORD="${VM_SSH_PASSWORD:-orion}"
SSH_PUBLIC_KEY_FILE="${SSH_PUBLIC_KEY_FILE:-}"

CONTROL_NAME="${CONTROL_NAME:-vm-control-plane}"
CONTROL_IP="${CONTROL_IP:-10.20.0.10}"
CONTROL_MAC="${CONTROL_MAC:-52:54:00:20:00:10}"
CONTROL_VCPUS="${CONTROL_VCPUS:-4}"
CONTROL_MEMORY_MB="${CONTROL_MEMORY_MB:-8192}"
CONTROL_DISK_GB="${CONTROL_DISK_GB:-40}"

EDGE_NAME="${EDGE_NAME:-vm-edge-01}"
EDGE_IP="${EDGE_IP:-10.20.0.21}"
EDGE_MAC="${EDGE_MAC:-52:54:00:20:00:21}"
EDGE_VCPUS="${EDGE_VCPUS:-4}"
EDGE_MEMORY_MB="${EDGE_MEMORY_MB:-8192}"
EDGE_DISK_GB="${EDGE_DISK_GB:-60}"
EDGE_LVM_DISK_GB="${EDGE_LVM_DISK_GB:-40}"

SSH_WAIT_TIMEOUT="${SSH_WAIT_TIMEOUT:-240}"
VIRT_TYPE="${VIRT_TYPE:-}"

usage() {
  cat <<'EOF'
Usage:
  two-vm-lab.sh up
  two-vm-lab.sh down
  two-vm-lab.sh destroy
  two-vm-lab.sh status

Environment overrides:
  LAB_NAME, LIBVIRT_URI, LAB_DIR, BASE_IMAGE_URL
  CONTROL_NAME, CONTROL_IP, CONTROL_VCPUS, CONTROL_MEMORY_MB, CONTROL_DISK_GB
  EDGE_NAME, EDGE_IP, EDGE_VCPUS, EDGE_MEMORY_MB, EDGE_DISK_GB, EDGE_LVM_DISK_GB
  VM_SSH_USER, VM_SSH_PASSWORD, SSH_PUBLIC_KEY_FILE

Notes:
  - Default image: Debian 13 genericcloud qcow2.
  - The edge VM is prepared for nested libvirt/qemu, but real nested KVM still
    depends on the host exposing virtualization extensions.
EOF
}

log() {
  printf '[orion-lab] %s\n' "$*"
}

die() {
  printf '[orion-lab] error: %s\n' "$*" >&2
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
  need_cmd qemu-img
  need_cmd virsh
  need_cmd virt-install
  need_cmd cloud-localds
  need_cmd mktemp
  need_cmd awk
  need_cmd sed
  if [[ -n "$SSH_PUBLIC_KEY" ]]; then
    need_cmd ssh
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
  trap 'rm -f "$tmp"' RETURN
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
      <range start='10.20.0.100' end='10.20.0.200'/>
      <host mac='${CONTROL_MAC}' name='${CONTROL_NAME}' ip='${CONTROL_IP}'/>
      <host mac='${EDGE_MAC}' name='${EDGE_NAME}' ip='${EDGE_IP}'/>
    </dhcp>
  </ip>
</network>
EOF
}

ensure_network() {
  if run_virsh net-info "$NETWORK_NAME" >/dev/null 2>&1; then
    if ! run_virsh net-info "$NETWORK_NAME" | grep -q 'Active:.*yes'; then
      log "starting existing libvirt network $NETWORK_NAME"
      run_virsh net-start "$NETWORK_NAME" >/dev/null
    fi
    run_virsh net-autostart "$NETWORK_NAME" >/dev/null
    return
  fi

  log "creating libvirt network $NETWORK_NAME"
  local xml
  xml="$(mktemp)"
  trap 'rm -f "$xml"' RETURN
  network_xml >"$xml"
  run_virsh net-define "$xml" >/dev/null
  run_virsh net-start "$NETWORK_NAME" >/dev/null
  run_virsh net-autostart "$NETWORK_NAME" >/dev/null
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

wait_for_ssh() {
  local ip="$1"
  local timeout="$2"
  local deadline shell_rc
  deadline=$((SECONDS + timeout))
  while ((SECONDS < deadline)); do
    if ssh \
      -o BatchMode=yes \
      -o StrictHostKeyChecking=no \
      -o UserKnownHostsFile=/dev/null \
      -o ConnectTimeout=3 \
      "${VM_SSH_USER}@${ip}" true >/dev/null 2>&1; then
      return 0
    fi
    shell_rc=$?
    if [[ "$shell_rc" -ne 255 ]]; then
      return 0
    fi
    sleep 5
  done
  return 1
}

control_plane_init_script() {
  cat <<EOF
#!/usr/bin/env bash
set -euxo pipefail
systemctl enable --now qemu-guest-agent || true
systemctl enable --now postgresql || true
systemctl enable --now nats-server || true

if command -v psql >/dev/null 2>&1; then
  sudo -u postgres psql -v ON_ERROR_STOP=1 <<'SQL'
DO \$\$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'orion') THEN
    CREATE ROLE orion LOGIN PASSWORD 'orion';
  END IF;
END
\$\$;
SQL

  if ! sudo -u postgres psql -tAc "SELECT 1 FROM pg_database WHERE datname = 'orion'" | grep -q 1; then
    sudo -u postgres createdb -O orion orion
  fi
fi

install -d -m 0755 /etc/orion /srv/orion/image
cat >/etc/orion/lab.env <<'ENV'
ORION_DB_DSN=postgres://orion:orion@127.0.0.1:5432/orion?sslmode=disable
ORION_NATS_URL=nats://127.0.0.1:4222
ORION_IDENTITY_URL=http://127.0.0.1:8081
ORION_PLACEMENT_URL=http://127.0.0.1:8082
ORION_COMPUTE_URL=http://127.0.0.1:8083
ORION_NODE_AGENT_URL=http://${EDGE_IP}:8084
ORION_NODE_AGENT_GRPC_ADDR=${EDGE_IP}:50051
ORION_IMAGE_URL=http://127.0.0.1:8085
ORION_NETWORK_URL=http://127.0.0.1:8086
ORION_VOLUME_URL=http://127.0.0.1:8087
ORION_VOLUME_HOST_AGENT_URL=http://${EDGE_IP}:8088
ORION_IMAGE_STORE_DIR=/srv/orion/image
ENV
EOF
}

edge_init_script() {
  cat <<EOF
#!/usr/bin/env bash
set -euxo pipefail
systemctl enable --now qemu-guest-agent || true
systemctl enable --now libvirtd || true
systemctl enable --now openvswitch-switch || true

if [[ -b /dev/vdb ]]; then
  if ! pvs /dev/vdb >/dev/null 2>&1; then
    pvcreate -ff -y /dev/vdb
  fi
  if ! vgs orion-vg >/dev/null 2>&1; then
    vgcreate orion-vg /dev/vdb
  fi
fi

install -d -m 0755 /etc/orion /var/lib/orion/node-agent /var/lib/orion/network-host-agent /var/lib/orion/volume-host-agent
cat >/etc/orion/lab.env <<'ENV'
ORION_NODE_AGENT_LISTEN_ADDR=0.0.0.0:8084
ORION_NETWORK_HOST_AGENT_LISTEN_ADDR=0.0.0.0:8087
ORION_VOLUME_HOST_AGENT_LISTEN_ADDR=0.0.0.0:8088
ORION_NETWORK_URL=http://${CONTROL_IP}:8086
ORION_NATS_URL=nats://${CONTROL_IP}:4222
ORION_NODE_AGENT_STORAGE_DIR=/var/lib/orion/node-agent
ORION_NETWORK_HOST_AGENT_STATE_DIR=/var/lib/orion/network-host-agent
ORION_VOLUME_HOST_AGENT_STATE_DIR=/var/lib/orion/volume-host-agent
ORION_VOLUME_LVM_VG=orion-vg
ENV
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
        postgresql-client)"
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
  local workdir
  workdir="$(mktemp -d)"
  trap 'rm -rf "$workdir"' RETURN

  cloud_user_data "$name" "$role" >"${workdir}/user-data"
  cloud_meta_data "$name" >"${workdir}/meta-data"

  local seed_path="${LAB_DIR}/${name}-seed.iso"
  run_root rm -f "$seed_path"
  run_root cloud-localds "$seed_path" "${workdir}/user-data" "${workdir}/meta-data"
  printf '%s\n' "$seed_path"
}

create_overlay_disk() {
  local path="$1"
  local size_gb="$2"
  if [[ -f "$path" ]]; then
    return
  fi
  log "creating overlay disk $path"
  run_root qemu-img create -f qcow2 -F qcow2 -b "$BASE_IMAGE_PATH" "$path" "${size_gb}G" >/dev/null
}

create_data_disk() {
  local path="$1"
  local size_gb="$2"
  if [[ -f "$path" ]]; then
    return
  fi
  log "creating data disk $path"
  run_root qemu-img create -f qcow2 "$path" "${size_gb}G" >/dev/null
}

create_control_plane_vm() {
  local disk_path="${LAB_DIR}/${CONTROL_NAME}.qcow2"
  local seed_path
  seed_path="$(create_seed_image "$CONTROL_NAME" control-plane)"
  create_overlay_disk "$disk_path" "$CONTROL_DISK_GB"

  if vm_exists "$CONTROL_NAME"; then
    log "domain already exists: $CONTROL_NAME"
    return
  fi

  log "creating domain $CONTROL_NAME"
  run_virt_install \
    --name "$CONTROL_NAME" \
    --virt-type "$(detect_virt_type)" \
    --memory "$CONTROL_MEMORY_MB" \
    --vcpus "$CONTROL_VCPUS" \
    --cpu host-passthrough,cache.mode=passthrough \
    --import \
    --osinfo detect=on,require=off \
    --graphics none \
    --noautoconsole \
    --rng /dev/urandom \
    --channel unix,target_type=virtio,name=org.qemu.guest_agent.0 \
    --network "network=${NETWORK_NAME},model=virtio,mac=${CONTROL_MAC}" \
    --disk "path=${disk_path},format=qcow2,bus=virtio" \
    --disk "path=${seed_path},device=cdrom"
}

create_edge_vm() {
  local disk_path="${LAB_DIR}/${EDGE_NAME}.qcow2"
  local data_path="${LAB_DIR}/${EDGE_NAME}-lvm.qcow2"
  local seed_path
  seed_path="$(create_seed_image "$EDGE_NAME" edge)"
  create_overlay_disk "$disk_path" "$EDGE_DISK_GB"
  create_data_disk "$data_path" "$EDGE_LVM_DISK_GB"

  if vm_exists "$EDGE_NAME"; then
    log "domain already exists: $EDGE_NAME"
    return
  fi

  log "creating domain $EDGE_NAME"
  run_virt_install \
    --name "$EDGE_NAME" \
    --virt-type "$(detect_virt_type)" \
    --memory "$EDGE_MEMORY_MB" \
    --vcpus "$EDGE_VCPUS" \
    --cpu host-passthrough,cache.mode=passthrough \
    --import \
    --osinfo detect=on,require=off \
    --graphics none \
    --noautoconsole \
    --rng /dev/urandom \
    --channel unix,target_type=virtio,name=org.qemu.guest_agent.0 \
    --network "network=${NETWORK_NAME},model=virtio,mac=${EDGE_MAC}" \
    --disk "path=${disk_path},format=qcow2,bus=virtio" \
    --disk "path=${data_path},format=qcow2,bus=virtio" \
    --disk "path=${seed_path},device=cdrom"
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
  local state
  state="$(vm_state "$name" || true)"
  if [[ "$state" == "running" ]]; then
    log "shutting down $name"
    run_virsh shutdown "$name" >/dev/null || true
  fi
}

destroy_vm() {
  local name="$1"
  if ! vm_exists "$name"; then
    return
  fi
  local state
  state="$(vm_state "$name" || true)"
  if [[ "$state" == "running" || "$state" == "in shutdown" ]]; then
    run_virsh destroy "$name" >/dev/null || true
  fi
  run_virsh undefine "$name" --nvram >/dev/null 2>&1 || run_virsh undefine "$name" >/dev/null 2>&1 || true
}

remove_lab_storage() {
  log "removing lab storage from $LAB_DIR"
  run_root rm -f \
    "${LAB_DIR}/${CONTROL_NAME}.qcow2" \
    "${LAB_DIR}/${CONTROL_NAME}-seed.iso" \
    "${LAB_DIR}/${EDGE_NAME}.qcow2" \
    "${LAB_DIR}/${EDGE_NAME}-seed.iso" \
    "${LAB_DIR}/${EDGE_NAME}-lvm.qcow2"
}

up() {
  ensure_prereqs
  ensure_lab_dir
  download_base_image
  ensure_network
  create_control_plane_vm
  create_edge_vm
  start_vm_if_needed "$CONTROL_NAME"
  start_vm_if_needed "$EDGE_NAME"
  wait_for_vm_state "$CONTROL_NAME" running
  wait_for_vm_state "$EDGE_NAME" running

  log "control plane: ${CONTROL_NAME} ${CONTROL_IP}"
  log "edge host:     ${EDGE_NAME} ${EDGE_IP}"

  if [[ -n "$SSH_PUBLIC_KEY" ]]; then
    if wait_for_ssh "$CONTROL_IP" "$SSH_WAIT_TIMEOUT"; then
      log "ssh ready on ${CONTROL_NAME}"
    else
      log "ssh did not become ready on ${CONTROL_NAME} within ${SSH_WAIT_TIMEOUT}s"
    fi

    if wait_for_ssh "$EDGE_IP" "$SSH_WAIT_TIMEOUT"; then
      log "ssh ready on ${EDGE_NAME}"
    else
      log "ssh did not become ready on ${EDGE_NAME} within ${SSH_WAIT_TIMEOUT}s"
    fi
  else
    log "no SSH public key found; skipping SSH readiness checks"
  fi

  cat <<EOF

Lab ready.

SSH:
  ssh ${VM_SSH_USER}@${CONTROL_IP}
  ssh ${VM_SSH_USER}@${EDGE_IP}

Next steps:
  1. Build the Orion packages locally:
       make build
       ./packaging/deb/build.sh
  2. Copy packages to the guests:
       scp packaging/deb/out/orion-common_*.deb packaging/deb/out/orion-control-plane_*.deb packaging/deb/out/orion-cli_*.deb ${VM_SSH_USER}@${CONTROL_IP}:~/
       scp packaging/deb/out/orion-common_*.deb packaging/deb/out/orion-host-agents_*.deb ${VM_SSH_USER}@${EDGE_IP}:~/
  3. Follow the manual Orion install from:
       docs/two-vm-install.md

Useful commands:
  sudo virsh -c ${LIBVIRT_URI} list --all
  sudo virsh -c ${LIBVIRT_URI} console ${CONTROL_NAME}
  sudo virsh -c ${LIBVIRT_URI} console ${EDGE_NAME}
EOF
}

down() {
  shutdown_vm_if_running "$CONTROL_NAME"
  shutdown_vm_if_running "$EDGE_NAME"
}

destroy() {
  destroy_vm "$CONTROL_NAME"
  destroy_vm "$EDGE_NAME"
  if run_virsh net-info "$NETWORK_NAME" >/dev/null 2>&1; then
    run_virsh net-destroy "$NETWORK_NAME" >/dev/null 2>&1 || true
    run_virsh net-undefine "$NETWORK_NAME" >/dev/null 2>&1 || true
  fi
  remove_lab_storage
}

status() {
  run_virsh list --all || true
  printf '\n'
  printf '%-20s %-15s %-15s\n' "domain" "ip" "state"
  printf '%-20s %-15s %-15s\n' "$CONTROL_NAME" "$CONTROL_IP" "$(vm_state "$CONTROL_NAME" || echo missing)"
  printf '%-20s %-15s %-15s\n' "$EDGE_NAME" "$EDGE_IP" "$(vm_state "$EDGE_NAME" || echo missing)"
}

case "$ACTION" in
  up)
    up
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
  -h|--help|help)
    usage
    ;;
  *)
    usage >&2
    exit 1
    ;;
esac

use std::collections::HashMap;
use std::path::{Path, PathBuf};
use std::sync::Arc;
use std::time::{Duration, Instant};

use anyhow::{anyhow, Context, Result};
use parking_lot::RwLock;
use serde::{Deserialize, Serialize};
use virt::{
    connect::Connect, domain::Domain, storage_pool::StoragePool, storage_vol::StorageVol, sys,
};

use crate::http_types::{ImageSource, NetworkPort};

const DEVICE_PERSIST_FLAGS: u32 = 1 | 2;
const UNDEFINE_NVRAM_FLAG: u32 = 4;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DesiredInstance {
    pub instance_id: String,
    pub name: String,
    pub vcpus: u32,
    pub memory_mb: u32,
    pub disk_gb: u32,
    pub image_id: String,
    #[serde(default)]
    pub image_source_path: String,
    pub desired_state: InstanceState,
    pub generation: i64,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
pub enum InstanceState {
    Running,
    Stopped,
    Paused,
    Crashed,
}

impl Default for InstanceState {
    fn default() -> Self {
        InstanceState::Stopped
    }
}

impl std::fmt::Display for InstanceState {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            InstanceState::Running => write!(f, "RUNNING"),
            InstanceState::Stopped => write!(f, "STOPPED"),
            InstanceState::Paused => write!(f, "PAUSED"),
            InstanceState::Crashed => write!(f, "CRASHED"),
        }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ObservedInstance {
    pub instance_id: String,
    pub name: String,
    pub status: InstanceState,
    pub vcpus: u32,
    pub memory_mb: u32,
    pub disk_gb: u32,
    pub domain_name: String,
    pub generation: i64,
}

#[derive(Debug, Clone, Default)]
pub struct ReconcileStatus {
    pub last_reconcile_at: Option<Instant>,
    pub last_success_at: Option<Instant>,
    pub consecutive_errors: u32,
    pub circuit_open: bool,
    pub circuit_open_since: Option<Instant>,
}

impl ReconcileStatus {
    pub fn new() -> Self {
        Self::default()
    }

    pub fn record_success(&mut self) {
        self.last_success_at = Some(Instant::now());
        self.consecutive_errors = 0;
        if self.circuit_open {
            self.circuit_open = false;
            self.circuit_open_since = None;
        }
    }

    pub fn record_error(&mut self) {
        self.consecutive_errors += 1;
        if self.consecutive_errors >= 5 {
            self.circuit_open = true;
            self.circuit_open_since = Some(Instant::now());
        }
    }

    pub fn can_reconcile(&self) -> bool {
        !self.circuit_open
    }

    pub fn circuit_breaker_cooldown() -> Duration {
        Duration::from_secs(30)
    }

    pub fn is_circuit_breaker_ready(&self) -> bool {
        if !self.circuit_open {
            return true;
        }
        if let Some(since) = self.circuit_open_since {
            return since.elapsed() >= Self::circuit_breaker_cooldown();
        }
        false
    }
}

#[derive(Debug, Clone, Default)]
pub struct DesiredState {
    pub instances: HashMap<String, DesiredInstance>,
    pub generation: i64,
}

#[derive(Debug, Clone, Default)]
pub struct ObservedState {
    pub instances: HashMap<String, ObservedInstance>,
}

pub fn open_libvirt() -> Result<Connect> {
    Connect::open(Some("qemu:///system"))
        .map_err(|e| anyhow!("failed to open libvirt connection: {e}"))
}

pub fn resolve_image_path(storage_dir: &Path, image: &ImageSource) -> Result<PathBuf> {
    if !image.source_path.is_empty() {
        let p = PathBuf::from(&image.source_path);
        if p.exists() {
            return Ok(p);
        }
    }
    let cached = storage_dir.join(format!("{}.qcow2", image.id));
    if cached.exists() {
        return Ok(cached);
    }
    Err(anyhow!(
        "image {} not found at {} or cache",
        image.id,
        image.source_path
    ))
}

pub fn create_overlay_disk(
    conn: &Connect,
    storage_dir: &Path,
    disk_path: &Path,
    backing_path: &Path,
    disk_gb: u32,
) -> Result<PathBuf> {
    let pool = ensure_storage_pool(conn, storage_dir)?;
    let vol_name = disk_path
        .file_name()
        .and_then(|n| n.to_str())
        .unwrap_or("disk.qcow2");

    if let Ok(existing) = StorageVol::lookup_by_name(&pool, vol_name) {
        let _ = existing.delete(0);
    }

    let size_bytes = disk_gb as i64 * 1024 * 1024 * 1024;
    let xml = format!(
        "<volume type='file'>\
<name>{vol_name}</name>\
<capacity unit='b'>{size_bytes}</capacity>\
<target>\
<format type='qcow2'/>\
</target>\
<backingStore>\
<path>{}</path>\
<format type='qcow2'/>\
</backingStore>\
</volume>",
        backing_path.display()
    );

    let vol = StorageVol::create_xml(&pool, &xml, 0)?;
    let path = vol
        .get_path()
        .map_err(|e| anyhow!("volume path error: {e}"))?;
    Ok(PathBuf::from(path))
}

pub fn delete_overlay_disk(_conn: &Connect, disk_path: &Path) -> Result<()> {
    if !disk_path.exists() {
        return Ok(());
    }
    std::fs::remove_file(disk_path).ok();
    Ok(())
}

pub fn ensure_domain_absent(conn: &Connect, domain_name: &str) -> Result<()> {
    if let Ok(domain) = Domain::lookup_by_name(conn, domain_name) {
        if domain.is_active().ok() == Some(true) {
            domain.destroy().ok();
        }
        domain.undefine_flags(UNDEFINE_NVRAM_FLAG).ok();
    }
    Ok(())
}

pub fn ensure_ovs_ready(ports: &[NetworkPort]) -> Result<()> {
    use std::process::Command;
    let br_int = std::path::Path::new("/sys/class/net/br-int");
    if !br_int.exists() {
        if !ports.is_empty() {
            return Err(anyhow!("OVS integration bridge br-int is required"));
        }
        return Ok(());
    }
    for port in ports {
        let out = Command::new("ovs-vsctl")
            .args(["--", "add-port", "br-int", &port.mac_address])
            .output();
        if let Err(e) = out {
            tracing::warn!(port_id = %port.id, error = %e, "ovs-vsctl failed");
        }
    }
    Ok(())
}

pub fn write_domain_xml(
    xml_path: &Path,
    domain_name: &str,
    disk_path: &Path,
    ports: &[NetworkPort],
    vcpus: u32,
    memory_mb: u32,
) -> Result<String> {
    let mut xml = format!(
        r#"<domain type='qemu'>
<name>{domain_name}</name>
<memory unit='MiB'>{memory_mb}</memory>
<vcpu placement='static'>{vcpus}</vcpu>
<os>
<type arch='x86_64' machine='pc'>hvm</type>
<boot dev='hd'/>
</os>
<devices>
<disk type='file' device='disk'>
<driver name='qemu' type='qcow2'/>
<source file='{}'/>
<target dev='vda' bus='virtio'/>
</disk>
<console type='pty'/>
<rng model='virtio'><backend model='random'>/dev/urandom</backend></rng>
"#,
        disk_path.display()
    );
    for port in ports {
        let dev = "vdc";
        xml.push_str(&format!(
            r#"<interface type='bridge'>
<source bridge='br-int'/>
<mac address='{}'/>
<virtualport type='openvswitch'/>
<target dev='{}'/>
<model type='virtio'/>
</interface>
"#,
            port.mac_address, dev
        ));
    }
    xml.push_str("</devices>\n</domain>");
    std::fs::write(xml_path, &xml)
        .with_context(|| format!("failed to write {}", xml_path.display()))?;
    Ok(xml)
}

pub fn next_disk_target(domain: &Domain) -> Result<String> {
    let xml = domain.get_xml_desc(0)?;
    let targets = find_disk_targets(&xml);
    let letters: Vec<char> = "vdbcedfghijklmnopqrstuvwxyz".chars().collect();
    for letter in &letters {
        let target = format!("vd{}", letter);
        if !targets.contains(&target) {
            return Ok(target);
        }
    }
    Err(anyhow!("no available virtio disk target for domain"))
}

fn find_disk_targets(xml: &str) -> Vec<String> {
    let mut targets = Vec::new();
    for line in xml.lines() {
        let line = line.trim();
        if line.starts_with("<target dev='") {
            if let Some(rest) = line.strip_prefix("<target dev='") {
                if let Some(dev) = rest.split('\'').next() {
                    targets.push(dev.to_string());
                }
            }
        }
    }
    targets
}

pub fn find_attached_disk_in_xml(
    xml: &str,
    _volume_id: &str,
    device_path: &str,
) -> Option<(String, String)> {
    let mut in_disk = false;
    let mut current_target = String::new();
    let mut current_source = String::new();

    for line in xml.lines() {
        let line = line.trim();
        if line == "<disk type='block' device='disk'>" || line.starts_with("<disk ") {
            in_disk = true;
            current_target.clear();
            current_source.clear();
        } else if line == "</disk>" && in_disk {
            if current_source == device_path && !current_target.is_empty() {
                return Some((current_target.clone(), current_source.clone()));
            }
            in_disk = false;
        } else if in_disk {
            if line.starts_with("<target dev='") {
                if let Some(dev) = line
                    .strip_prefix("<target dev='")
                    .and_then(|s| s.split('\'').next())
                {
                    current_target = dev.to_string();
                }
            } else if line.starts_with("<source dev='") {
                if let Some(dev) = line
                    .strip_prefix("<source dev='")
                    .and_then(|s| s.split('\'').next())
                {
                    current_source = dev.to_string();
                }
            }
        }
    }
    None
}

pub fn wait_for_detach(domain: &Domain, volume_id: &str, device_path: &str) -> Result<()> {
    for _ in 0..30 {
        std::thread::sleep(std::time::Duration::from_secs(1));
        let xml = domain.get_xml_desc(0)?;
        if find_attached_disk_in_xml(&xml, volume_id, device_path).is_none() {
            return Ok(());
        }
    }
    Err(anyhow!("volume did not detach within timeout"))
}

pub fn wait_for_attach(domain: &Domain, target: &str) -> Result<()> {
    for _ in 0..30 {
        std::thread::sleep(std::time::Duration::from_secs(1));
        let xml = domain.get_xml_desc(0)?;
        if xml.contains(&format!("<target dev='{}'", target)) {
            return Ok(());
        }
    }
    Err(anyhow!("volume did not attach within timeout"))
}

pub fn ensure_storage_pool(conn: &Connect, dir: &Path) -> Result<StoragePool> {
    let target = dir.display().to_string();
    if let Ok(pool) = StoragePool::lookup_by_target_path(conn, &target) {
        pool.refresh(0)
            .map_err(|e| anyhow!("failed to refresh storage pool for {target}: {e}"))?;
        return Ok(pool);
    }
    std::fs::create_dir_all(dir).with_context(|| format!("failed to create {}", dir.display()))?;
    let pool_name = format!(
        "orion-agent-{}",
        uuid::Uuid::new_v4().to_string().replace("-", "")
    );
    let xml = format!(
        "<pool type='dir'>\
<name>{pool_name}</name>\
<target><path>{target}</path></target>\
</pool>"
    );
    let pool = match StoragePool::define_xml(conn, &xml, 0) {
        Ok(pool) => pool,
        Err(_) => StoragePool::lookup_by_name(conn, &pool_name)
            .map_err(|e| anyhow!("failed to define or lookup storage pool {pool_name}: {e}"))?,
    };
    if !pool
        .is_active()
        .map_err(|e| anyhow!("failed to inspect storage pool {pool_name}: {e}"))?
    {
        pool.create(0)
            .map_err(|e| anyhow!("failed to activate storage pool {pool_name}: {e}"))?;
    }
    pool.refresh(0)
        .map_err(|e| anyhow!("failed to refresh storage pool {pool_name}: {e}"))?;
    Ok(pool)
}

pub fn observe_all_instances(conn: &Connect) -> Result<ObservedState> {
    let names = conn.list_domains()?;
    let mut instances = HashMap::new();

    for id in names {
        if let Ok(domain) = Domain::lookup_by_id(conn, id) {
            if let Ok(info) = domain.get_info() {
                let name = domain.get_name().unwrap_or_default();
                let status = match info.state {
                    sys::VIR_DOMAIN_RUNNING => InstanceState::Running,
                    sys::VIR_DOMAIN_PAUSED => InstanceState::Paused,
                    sys::VIR_DOMAIN_SHUTDOWN | sys::VIR_DOMAIN_SHUTOFF => InstanceState::Stopped,
                    _ => InstanceState::Crashed,
                };

                let instance = ObservedInstance {
                    instance_id: name.clone(),
                    name: name.clone(),
                    status,
                    vcpus: info.nr_virt_cpu as u32,
                    memory_mb: (info.max_mem / 1024) as u32,
                    disk_gb: 0,
                    domain_name: name,
                    generation: 0,
                };
                instances.insert(instance.instance_id.clone(), instance);
            }
        }
    }

    Ok(ObservedState { instances })
}

#[derive(Debug, Clone)]
pub enum ReconcileAction {
    Create { instance: DesiredInstance },
    Start { instance_id: String },
    Stop { instance_id: String },
    Delete { instance_id: String },
    NoAction,
}

pub fn compute_reconcile_actions(
    desired: &DesiredState,
    observed: &ObservedState,
) -> Vec<ReconcileAction> {
    let mut actions = Vec::new();

    for (id, desired_inst) in &desired.instances {
        match observed.instances.get(id) {
            Some(observed_inst) => {
                if observed_inst.status == InstanceState::Stopped
                    && desired_inst.desired_state == InstanceState::Running
                {
                    actions.push(ReconcileAction::Start {
                        instance_id: id.clone(),
                    });
                } else if observed_inst.status == InstanceState::Running
                    && desired_inst.desired_state == InstanceState::Stopped
                {
                    actions.push(ReconcileAction::Stop {
                        instance_id: id.clone(),
                    });
                }
            }
            None => {
                if desired_inst.desired_state == InstanceState::Running
                    || desired_inst.desired_state == InstanceState::Stopped
                {
                    actions.push(ReconcileAction::Create {
                        instance: desired_inst.clone(),
                    });
                }
            }
        }
    }

    for (id, observed_inst) in &observed.instances {
        if !desired.instances.contains_key(id) {
            actions.push(ReconcileAction::Delete {
                instance_id: id.clone(),
            });
        }
    }

    if actions.is_empty() {
        actions.push(ReconcileAction::NoAction);
    }

    actions
}

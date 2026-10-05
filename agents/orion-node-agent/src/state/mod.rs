use std::collections::HashSet;
use std::fs;
use std::path::PathBuf;
use std::sync::Arc;
use std::time::Instant;

use parking_lot::{Mutex, RwLock};

use crate::domain::{DesiredState, ReconcileStatus};

#[derive(Clone)]
pub struct AgentState {
    pub host_id: Arc<String>,
    pub storage_dir: Arc<PathBuf>,
    pub libvirt_healthy: Arc<RwLock<bool>>,
    pub last_reconcile_at: Arc<RwLock<Option<chrono::DateTime<chrono::Utc>>>>,
    processed_commands: Arc<Mutex<HashSet<String>>>,
    pub desired_state: Arc<RwLock<DesiredState>>,
    pub reconcile_status: Arc<RwLock<ReconcileStatus>>,
    pub last_heartbeat_at: Arc<RwLock<Option<Instant>>>,
    pub agent_ready: Arc<RwLock<bool>>,
}

impl AgentState {
    pub fn new(storage_dir: PathBuf) -> Self {
        let processed_path = storage_dir.join("processed_commands.json");
        let processed_commands = fs::read(&processed_path)
            .ok()
            .and_then(|data| serde_json::from_slice::<HashSet<String>>(&data).ok())
            .unwrap_or_default();
        Self {
            host_id: Arc::new(
                std::env::var("ORION_NODE_AGENT_HOST_ID")
                    .unwrap_or_else(|_| "host_local".to_string()),
            ),
            storage_dir: Arc::new(storage_dir),
            libvirt_healthy: Arc::new(RwLock::new(false)),
            last_reconcile_at: Arc::new(RwLock::new(None)),
            processed_commands: Arc::new(Mutex::new(processed_commands)),
            desired_state: Arc::new(RwLock::new(DesiredState::default())),
            reconcile_status: Arc::new(RwLock::new(ReconcileStatus::new())),
            last_heartbeat_at: Arc::new(RwLock::new(None)),
            agent_ready: Arc::new(RwLock::new(false)),
        }
    }

    pub fn set_libvirt_healthy(&self, healthy: bool) {
        *self.libvirt_healthy.write() = healthy;
    }

    pub fn is_libvirt_healthy(&self) -> bool {
        *self.libvirt_healthy.read()
    }

    pub fn update_reconcile_time(&self) {
        *self.last_reconcile_at.write() = Some(chrono::Utc::now());
    }

    pub fn claim_command(&self, message_id: &str) -> anyhow::Result<bool> {
        if message_id.is_empty() {
            anyhow::bail!("command message_id is required")
        }

        let mut processed = self.processed_commands.lock();
        if processed.contains(message_id) {
            return Ok(false);
        }
        processed.insert(message_id.to_string());

        let path = self.storage_dir.join("processed_commands.json");
        let tmp_path = self.storage_dir.join("processed_commands.json.tmp");
        let data = serde_json::to_vec(&*processed)?;
        if let Err(err) = fs::write(&tmp_path, data).and_then(|_| fs::rename(&tmp_path, &path)) {
            processed.remove(message_id);
            return Err(err.into());
        }
        Ok(true)
    }

    pub fn update_desired_state(&self, desired: DesiredState) {
        *self.desired_state.write() = desired;
    }

    pub fn apply_desired_update(
        &self,
        instances: std::collections::HashMap<String, crate::domain::DesiredInstance>,
        deleted_instances: &[String],
        generation: i64,
    ) {
        let mut desired = self.desired_state.write();
        if generation < desired.generation {
            return;
        }
        for (id, instance) in instances {
            desired.instances.insert(id, instance);
        }
        for id in deleted_instances {
            desired.instances.remove(id);
        }
        desired.generation = generation;
    }

    pub fn get_desired_state(&self) -> DesiredState {
        self.desired_state.read().clone()
    }

    pub fn record_heartbeat(&self) {
        *self.last_heartbeat_at.write() = Some(Instant::now());
    }

    pub fn is_ready(&self) -> bool {
        if !self.is_libvirt_healthy() {
            return false;
        }
        self.last_reconcile_at
            .read()
            .map(|timestamp| {
                chrono::Utc::now().signed_duration_since(timestamp) < chrono::Duration::seconds(90)
            })
            .unwrap_or(false)
    }

    pub fn set_ready(&self, ready: bool) {
        *self.agent_ready.write() = ready;
    }

    pub fn record_reconcile_success(&self) {
        self.update_reconcile_time();
        self.reconcile_status.write().record_success();
    }

    pub fn record_reconcile_error(&self) {
        self.reconcile_status.write().record_error();
    }

    pub fn can_reconcile(&self) -> bool {
        let status = self.reconcile_status.read();
        status.can_reconcile() && status.is_circuit_breaker_ready()
    }
}

use std::time::{Duration, SystemTime};

use anyhow::Result;
use tokio::time::sleep;

use crate::domain::{
    compute_reconcile_actions, observe_all_instances, open_libvirt, DesiredInstance, InstanceState,
    ReconcileAction,
};
use crate::state::AgentState;
use crate::sync::{build_server_sync, delete_server_sync};

const RECONCILE_INTERVAL_SECS: u64 = 30;
const INITIAL_BACKOFF_MS: u64 = 1000;
const MAX_BACKOFF_MS: u64 = 30000;

pub struct Reconciler {
    state: AgentState,
}

impl Reconciler {
    pub fn new(state: AgentState) -> Self {
        Self { state }
    }

    pub async fn run(&self) {
        tracing::info!("starting reconciler loop");
        let mut backoff_ms = INITIAL_BACKOFF_MS;

        loop {
            if !self.state.can_reconcile() {
                {
                    let status = self.state.reconcile_status.read();
                    if status.circuit_open {
                        tracing::warn!(
                            "circuit breaker open, waiting for cooldown; consecutive_errors={}",
                            status.consecutive_errors
                        );
                    }
                }
                sleep(Duration::from_secs(5)).await;
                continue;
            }

            match self.reconcile().await {
                Ok(()) => {
                    backoff_ms = INITIAL_BACKOFF_MS;
                    sleep(Duration::from_secs(RECONCILE_INTERVAL_SECS)).await;
                }
                Err(e) => {
                    tracing::error!(error = %e, "reconciliation failed");
                    self.state.record_reconcile_error();
                    let jitter_bound = (backoff_ms / 4).max(1);
                    let jitter = SystemTime::now()
                        .duration_since(SystemTime::UNIX_EPOCH)
                        .map(|duration| duration.subsec_millis() as u64 % jitter_bound)
                        .unwrap_or(0);
                    sleep(Duration::from_millis(backoff_ms + jitter)).await;
                    backoff_ms = (backoff_ms * 2).min(MAX_BACKOFF_MS);
                }
            }
        }
    }

    async fn reconcile(&self) -> Result<()> {
        let desired = self.state.get_desired_state();
        tracing::debug!(
            generation = desired.generation,
            instances = desired.instances.len(),
            "reconciling desired state"
        );

        let conn = open_libvirt()?;
        let observed = observe_all_instances(&conn)?;
        tracing::debug!(
            observed_instances = observed.instances.len(),
            "observed state from libvirt"
        );

        let actions = compute_reconcile_actions(&desired, &observed);
        let mut applied = 0;
        let mut first_error = None;

        for action in &actions {
            match action {
                ReconcileAction::Create { instance } => {
                    tracing::info!(instance_id = %instance.instance_id, "creating instance");
                    if let Err(e) = self.create_instance(instance).await {
                        tracing::error!(error = %e, instance_id = %instance.instance_id, "failed to create instance");
                        first_error.get_or_insert(e);
                    } else {
                        applied += 1;
                    }
                }
                ReconcileAction::Start { instance_id } => {
                    tracing::info!(instance_id = %instance_id, "starting instance");
                    if let Err(e) = self.start_instance(instance_id).await {
                        tracing::error!(error = %e, instance_id = %instance_id, "failed to start instance");
                        first_error.get_or_insert(e);
                    } else {
                        applied += 1;
                    }
                }
                ReconcileAction::Stop { instance_id } => {
                    tracing::info!(instance_id = %instance_id, "stopping instance");
                    if let Err(e) = self.stop_instance(instance_id).await {
                        tracing::error!(error = %e, instance_id = %instance_id, "failed to stop instance");
                        first_error.get_or_insert(e);
                    } else {
                        applied += 1;
                    }
                }
                ReconcileAction::Delete { instance_id } => {
                    tracing::info!(instance_id = %instance_id, "deleting instance");
                    if let Err(e) = self.delete_instance(instance_id).await {
                        tracing::error!(error = %e, instance_id = %instance_id, "failed to delete instance");
                        first_error.get_or_insert(e);
                    } else {
                        applied += 1;
                    }
                }
                ReconcileAction::NoAction => {
                    tracing::debug!("no reconcile actions needed");
                }
            }
        }

        tracing::info!(applied_actions = applied, "reconciliation complete");
        if let Some(error) = first_error {
            return Err(error);
        }
        self.state.record_reconcile_success();
        Ok(())
    }

    async fn create_instance(&self, desired: &DesiredInstance) -> Result<()> {
        let storage_dir = self.state.storage_dir.clone();
        let req = crate::http_types::BuildServerRequest {
            server_id: desired.instance_id.clone(),
            host_id: String::new(),
            name: desired.name.clone(),
            image: crate::http_types::ImageSource {
                id: desired.image_id.clone(),
                source_path: desired.image_source_path.clone(),
                source_url: String::new(),
                checksum_sha256: String::new(),
                size_bytes: 0,
            },
            ports: vec![],
            vcpus: desired.vcpus,
            memory_mb: desired.memory_mb,
            disk_gb: desired.disk_gb,
        };

        let result =
            tokio::task::spawn_blocking(move || build_server_sync(&storage_dir, req)).await??;

        if desired.desired_state == InstanceState::Stopped && result.status == "running" {
            let conn = open_libvirt()?;
            if let Ok(domain) = virt::domain::Domain::lookup_by_name(&conn, &result.domain_name) {
                let _ = domain.shutdown();
            }
        }

        Ok(())
    }

    async fn start_instance(&self, instance_id: &str) -> Result<()> {
        let conn = open_libvirt()?;
        if let Ok(domain) = virt::domain::Domain::lookup_by_name(&conn, instance_id) {
            if domain.is_active().unwrap_or(false) {
                return Ok(());
            }
            domain.create()?;
        }
        Ok(())
    }

    async fn stop_instance(&self, instance_id: &str) -> Result<()> {
        let conn = open_libvirt()?;
        if let Ok(domain) = virt::domain::Domain::lookup_by_name(&conn, instance_id) {
            if !domain.is_active().unwrap_or(false) {
                return Ok(());
            }
            domain.shutdown()?;
            for _ in 0..30 {
                if !domain.is_active().unwrap_or(false) {
                    return Ok(());
                }
                sleep(Duration::from_secs(1)).await;
            }
        }
        Ok(())
    }

    async fn delete_instance(&self, instance_id: &str) -> Result<()> {
        let storage_dir = self.state.storage_dir.clone();
        let instance_id = instance_id.to_string();
        tokio::task::spawn_blocking(move || delete_server_sync(&storage_dir, &instance_id))
            .await??;
        Ok(())
    }
}

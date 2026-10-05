use std::path::PathBuf;
use std::process::Command;

use anyhow::{anyhow, Context, Result};
use async_trait::async_trait;

use super::{BackendType, DevicePath, VolumeBackend, VolumeError, VolumeHandle, VolumeSpec};

pub struct CephBackend {
    pub pool: String,
    pub state_dir: PathBuf,
    pub rbd_namespace: Option<String>,
}

impl CephBackend {
    pub fn new(pool: String, state_dir: PathBuf) -> Self {
        Self {
            pool,
            state_dir,
            rbd_namespace: None,
        }
    }

    pub fn with_namespace(mut self, namespace: String) -> Self {
        self.rbd_namespace = Some(namespace);
        self
    }

    fn rbd_name(&self, volume_id: &str) -> String {
        let suffix = volume_id
            .split_once('_')
            .map(|(_, tail)| tail)
            .unwrap_or(volume_id);
        suffix.replace('_', "-")
    }

    fn device_path(&self, rbd_name: &str) -> String {
        format!("/dev/rbd/{}/{}", self.pool, rbd_name)
    }

    fn marker_path(&self, volume_id: &str) -> PathBuf {
        self.state_dir.join(format!("ceph-{}.json", volume_id))
    }

    fn run_command(&self, cmd: &mut Command) -> Result<()> {
        let rendered = format!("{cmd:?}");
        let output = cmd
            .output()
            .with_context(|| format!("failed to execute {rendered}"))?;
        if output.status.success() {
            return Ok(());
        }
        let stderr = String::from_utf8_lossy(&output.stderr).trim().to_string();
        let stdout = String::from_utf8_lossy(&output.stdout).trim().to_string();
        Err(anyhow!(
            "command failed: {rendered}; stdout={stdout}; stderr={stderr}"
        ))
    }

    fn rbd_cmd(&self) -> Command {
        let mut cmd = Command::new("rbd");
        cmd.arg("--pool").arg(&self.pool);
        if let Some(ns) = &self.rbd_namespace {
            cmd.arg("--namespace").arg(ns);
        }
        cmd
    }
}

#[async_trait]
impl VolumeBackend for CephBackend {
    fn name(&self) -> &'static str {
        "ceph"
    }

    fn backend_type(&self) -> BackendType {
        BackendType::Ceph
    }

    async fn create(&self, spec: &VolumeSpec) -> Result<VolumeHandle, VolumeError> {
        if spec.size_gb <= 0 {
            return Err(VolumeError::InvalidArgument(
                "volume size must be positive".to_string(),
            ));
        }

        let rbd_name = self.rbd_name(&spec.volume_id);
        let device_path = self.device_path(&rbd_name);

        if PathBuf::from(&device_path).exists() {
            return Ok(VolumeHandle {
                volume_id: spec.volume_id.clone(),
                device_path,
                backend: BackendType::Ceph,
            });
        }

        let mut cmd = self.rbd_cmd();
        cmd.arg("create")
            .arg("--image-feature")
            .arg("layering")
            .arg("--image-format")
            .arg("2")
            .arg("-s")
            .arg(format!("{}", spec.size_gb))
            .arg(&rbd_name);

        self.run_command(&mut cmd)
            .map_err(|e| VolumeError::OperationFailed(e.to_string()))?;

        let marker_path = self.marker_path(&spec.volume_id);
        let marker_content = format!(
            "{{\"volume_id\":\"{}\",\"rbd_name\":\"{}\",\"pool\":\"{}\"}}",
            spec.volume_id, rbd_name, self.pool
        );
        std::fs::write(&marker_path, marker_content)
            .context("failed to write volume state marker")
            .map_err(|e| VolumeError::OperationFailed(e.to_string()))?;

        Ok(VolumeHandle {
            volume_id: spec.volume_id.clone(),
            device_path,
            backend: BackendType::Ceph,
        })
    }

    async fn delete(&self, volume_id: &str) -> Result<(), VolumeError> {
        let rbd_name = self.rbd_name(volume_id);

        let mut check_cmd = self.rbd_cmd();
        check_cmd.arg("status").arg(&rbd_name);
        if self.run_command(&mut check_cmd).is_err() {
            return Err(VolumeError::NotFound(volume_id.to_string()));
        }

        let mut cmd = self.rbd_cmd();
        cmd.arg("rm").arg(&rbd_name);

        self.run_command(&mut cmd)
            .map_err(|e| VolumeError::OperationFailed(e.to_string()))?;

        let marker_path = self.marker_path(volume_id);
        let _ = std::fs::remove_file(marker_path);

        Ok(())
    }

    async fn attach(
        &self,
        volume: &VolumeHandle,
        _node_id: &str,
    ) -> Result<DevicePath, VolumeError> {
        let rbd_name = self.rbd_name(&volume.volume_id);

        let mut map_cmd = self.rbd_cmd();
        map_cmd.arg("map").arg(&rbd_name);

        if let Err(e) = self.run_command(&mut map_cmd) {
            if !e.to_string().contains("already mapped") {
                return Err(VolumeError::OperationFailed(e.to_string()));
            }
        }

        Ok(volume.device_path.clone())
    }

    async fn detach(&self, volume: &VolumeHandle, _node_id: &str) -> Result<(), VolumeError> {
        let rbd_name = self.rbd_name(&volume.volume_id);

        let mut unmap_cmd = self.rbd_cmd();
        unmap_cmd.arg("unmap").arg(&rbd_name);

        if let Err(e) = self.run_command(&mut unmap_cmd) {
            if !e.to_string().contains("not mapped") {
                return Err(VolumeError::OperationFailed(e.to_string()));
            }
        }

        Ok(())
    }

    async fn exists(&self, volume_id: &str) -> Result<bool, VolumeError> {
        let rbd_name = self.rbd_name(volume_id);
        let mut cmd = self.rbd_cmd();
        cmd.arg("status").arg(&rbd_name);
        Ok(self.run_command(&mut cmd).is_ok())
    }

    async fn create_snapshot(&self, volume_id: &str, snapshot_id: &str) -> Result<(), VolumeError> {
        let mut cmd = self.rbd_cmd();
        cmd.arg("snap").arg("create").arg(format!(
            "{}@{}",
            self.rbd_name(volume_id),
            self.rbd_name(snapshot_id)
        ));
        self.run_command(&mut cmd)
            .map_err(|e| VolumeError::OperationFailed(e.to_string()))
    }
    async fn delete_snapshot(&self, volume_id: &str, snapshot_id: &str) -> Result<(), VolumeError> {
        let mut cmd = self.rbd_cmd();
        cmd.arg("snap").arg("rm").arg(format!(
            "{}@{}",
            self.rbd_name(volume_id),
            self.rbd_name(snapshot_id)
        ));
        self.run_command(&mut cmd)
            .map_err(|e| VolumeError::OperationFailed(e.to_string()))
    }
    async fn restore_snapshot(
        &self,
        volume_id: &str,
        snapshot_id: &str,
    ) -> Result<(), VolumeError> {
        let mut cmd = self.rbd_cmd();
        cmd.arg("snap").arg("rollback").arg(format!(
            "{}@{}",
            self.rbd_name(volume_id),
            self.rbd_name(snapshot_id)
        ));
        self.run_command(&mut cmd)
            .map_err(|e| VolumeError::OperationFailed(e.to_string()))
    }
}

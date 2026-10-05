use std::{path::PathBuf, process::Command};

use anyhow::{anyhow, Context, Result};
use async_trait::async_trait;

use super::{BackendType, DevicePath, VolumeBackend, VolumeError, VolumeHandle, VolumeSpec};

pub struct LvmBackend {
    pub vg_name: String,
    pub state_dir: PathBuf,
}

impl LvmBackend {
    pub fn new(vg_name: String, state_dir: PathBuf) -> Self {
        Self { vg_name, state_dir }
    }

    fn lv_name(&self, volume_id: &str) -> String {
        let suffix = volume_id
            .split_once('_')
            .map(|(_, tail)| tail)
            .unwrap_or(volume_id);
        format!("orion-{}", suffix.replace('_', "-"))
    }

    fn device_path(&self, lv_name: &str) -> String {
        format!("/dev/{}/{}", self.vg_name, lv_name)
    }

    fn marker_path(&self, volume_id: &str) -> PathBuf {
        self.state_dir.join(format!("{}.json", volume_id))
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
}

#[async_trait]
impl VolumeBackend for LvmBackend {
    fn name(&self) -> &'static str {
        "lvm"
    }

    fn backend_type(&self) -> BackendType {
        BackendType::LVM
    }

    async fn create(&self, spec: &VolumeSpec) -> Result<VolumeHandle, VolumeError> {
        if spec.size_gb <= 0 {
            return Err(VolumeError::InvalidArgument(
                "volume size must be positive".to_string(),
            ));
        }

        let lv_name = self.lv_name(&spec.volume_id);
        let device_path = self.device_path(&lv_name);

        if PathBuf::from(&device_path).exists() {
            return Ok(VolumeHandle {
                volume_id: spec.volume_id.clone(),
                device_path,
                backend: BackendType::LVM,
            });
        }

        let mut cmd = Command::new("lvcreate");
        cmd.arg("--wipesignatures")
            .arg("n")
            .arg("--zero")
            .arg("n")
            .arg("--noudevsync")
            .arg("-L")
            .arg(format!("{}G", spec.size_gb))
            .arg("-n")
            .arg(&lv_name)
            .arg(&self.vg_name)
            .arg("-y");

        self.run_command(&mut cmd)
            .map_err(|e| VolumeError::OperationFailed(e.to_string()))?;

        let marker_path = self.marker_path(&spec.volume_id);
        let marker_content = format!(
            "{{\"volume_id\":\"{}\",\"device_path\":\"{}\"}}",
            spec.volume_id, device_path
        );
        std::fs::write(&marker_path, marker_content)
            .context("failed to write volume state marker")
            .map_err(|e| VolumeError::OperationFailed(e.to_string()))?;

        Ok(VolumeHandle {
            volume_id: spec.volume_id.clone(),
            device_path,
            backend: BackendType::LVM,
        })
    }

    async fn delete(&self, volume_id: &str) -> Result<(), VolumeError> {
        let lv_name = self.lv_name(volume_id);
        let device_path = self.device_path(&lv_name);

        if !PathBuf::from(&device_path).exists() {
            return Err(VolumeError::NotFound(volume_id.to_string()));
        }

        let mut cmd = Command::new("lvremove");
        cmd.arg("--noudevsync").arg("-y").arg(&device_path);

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
        if !PathBuf::from(&volume.device_path).exists() {
            return Err(VolumeError::NotFound(volume.volume_id.clone()));
        }
        Ok(volume.device_path.clone())
    }

    async fn detach(&self, volume: &VolumeHandle, _node_id: &str) -> Result<(), VolumeError> {
        if !PathBuf::from(&volume.device_path).exists() {
            return Err(VolumeError::NotFound(volume.volume_id.clone()));
        }
        Ok(())
    }

    async fn exists(&self, volume_id: &str) -> Result<bool, VolumeError> {
        let lv_name = self.lv_name(volume_id);
        let device_path = self.device_path(&lv_name);
        Ok(PathBuf::from(&device_path).exists())
    }

    async fn create_snapshot(&self, volume_id: &str, snapshot_id: &str) -> Result<(), VolumeError> {
        let origin = self.device_path(&self.lv_name(volume_id));
        if !PathBuf::from(&origin).exists() {
            return Err(VolumeError::NotFound(volume_id.to_string()));
        }
        let snapshot = self.device_path(&self.lv_name(snapshot_id));
        if PathBuf::from(&snapshot).exists() {
            return Ok(());
        }
        let mut cmd = Command::new("lvcreate");
        cmd.arg("--snapshot")
            .arg("--extents")
            .arg("100%ORIGIN")
            .arg("--noudevsync")
            .arg("-n")
            .arg(self.lv_name(snapshot_id))
            .arg(&origin)
            .arg("-y");
        self.run_command(&mut cmd)
            .map_err(|e| VolumeError::OperationFailed(e.to_string()))
    }

    async fn delete_snapshot(&self, volume_id: &str, snapshot_id: &str) -> Result<(), VolumeError> {
        let origin = self.device_path(&self.lv_name(volume_id));
        let snapshot = self.device_path(&self.lv_name(snapshot_id));
        if !PathBuf::from(&origin).exists() || !PathBuf::from(&snapshot).exists() {
            return Err(VolumeError::NotFound(snapshot_id.to_string()));
        }
        let mut cmd = Command::new("lvremove");
        cmd.arg("--noudevsync").arg("-y").arg(&snapshot);
        self.run_command(&mut cmd)
            .map_err(|e| VolumeError::OperationFailed(e.to_string()))
    }

    async fn restore_snapshot(
        &self,
        volume_id: &str,
        snapshot_id: &str,
    ) -> Result<(), VolumeError> {
        let origin = self.device_path(&self.lv_name(volume_id));
        let snapshot = self.device_path(&self.lv_name(snapshot_id));
        if !PathBuf::from(&origin).exists() || !PathBuf::from(&snapshot).exists() {
            return Err(VolumeError::NotFound(snapshot_id.to_string()));
        }
        let mut cmd = Command::new("lvconvert");
        cmd.arg("--merge")
            .arg("--noudevsync")
            .arg(&snapshot)
            .arg("-y");
        self.run_command(&mut cmd)
            .map_err(|e| VolumeError::OperationFailed(e.to_string()))
    }
}

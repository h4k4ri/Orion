use std::collections::{HashMap, HashSet};
use std::sync::RwLock;

use anyhow::Result;
use async_trait::async_trait;

use super::{BackendType, DevicePath, VolumeBackend, VolumeError, VolumeHandle, VolumeSpec};

pub struct MockBackend {
    volumes: RwLock<HashMap<String, VolumeHandle>>,
    snapshots: RwLock<HashSet<(String, String)>>,
}

impl MockBackend {
    pub fn new() -> Self {
        Self {
            volumes: RwLock::new(HashMap::new()),
            snapshots: RwLock::new(HashSet::new()),
        }
    }

    fn next_device_path(volume_id: &str) -> String {
        format!("/dev/mock/{}", volume_id.replace('_', "-"))
    }
}

impl Default for MockBackend {
    fn default() -> Self {
        Self::new()
    }
}

#[async_trait]
impl VolumeBackend for MockBackend {
    fn name(&self) -> &'static str {
        "mock"
    }

    fn backend_type(&self) -> BackendType {
        BackendType::Mock
    }

    async fn create(&self, spec: &VolumeSpec) -> Result<VolumeHandle, VolumeError> {
        let volumes = self.volumes.read().unwrap();

        if let Some(existing) = volumes.get(&spec.volume_id) {
            return Ok(existing.clone());
        }

        drop(volumes);

        let device_path = Self::next_device_path(&spec.volume_id);
        let handle = VolumeHandle {
            volume_id: spec.volume_id.clone(),
            device_path,
            backend: BackendType::Mock,
        };

        let mut volumes = self.volumes.write().unwrap();
        volumes.insert(spec.volume_id.clone(), handle.clone());

        Ok(handle)
    }

    async fn delete(&self, volume_id: &str) -> Result<(), VolumeError> {
        let mut volumes = self.volumes.write().unwrap();

        match volumes.remove(volume_id) {
            Some(_) => Ok(()),
            None => Err(VolumeError::NotFound(volume_id.to_string())),
        }
    }

    async fn attach(
        &self,
        volume: &VolumeHandle,
        _node_id: &str,
    ) -> Result<DevicePath, VolumeError> {
        let volumes = self.volumes.read().unwrap();

        if volumes.contains_key(&volume.volume_id) {
            Ok(volume.device_path.clone())
        } else {
            Err(VolumeError::NotFound(volume.volume_id.clone()))
        }
    }

    async fn detach(&self, volume: &VolumeHandle, _node_id: &str) -> Result<(), VolumeError> {
        let volumes = self.volumes.read().unwrap();

        if volumes.contains_key(&volume.volume_id) {
            Ok(())
        } else {
            Err(VolumeError::NotFound(volume.volume_id.clone()))
        }
    }

    async fn exists(&self, volume_id: &str) -> Result<bool, VolumeError> {
        let volumes = self.volumes.read().unwrap();
        Ok(volumes.contains_key(volume_id))
    }

    async fn create_snapshot(&self, volume_id: &str, snapshot_id: &str) -> Result<(), VolumeError> {
        if !self.volumes.read().unwrap().contains_key(volume_id) {
            return Err(VolumeError::NotFound(volume_id.to_string()));
        }
        self.snapshots
            .write()
            .unwrap()
            .insert((volume_id.to_string(), snapshot_id.to_string()));
        Ok(())
    }
    async fn delete_snapshot(&self, volume_id: &str, snapshot_id: &str) -> Result<(), VolumeError> {
        if self
            .snapshots
            .write()
            .unwrap()
            .remove(&(volume_id.to_string(), snapshot_id.to_string()))
        {
            Ok(())
        } else {
            Err(VolumeError::NotFound(snapshot_id.to_string()))
        }
    }
    async fn restore_snapshot(
        &self,
        volume_id: &str,
        snapshot_id: &str,
    ) -> Result<(), VolumeError> {
        if self
            .snapshots
            .read()
            .unwrap()
            .contains(&(volume_id.to_string(), snapshot_id.to_string()))
        {
            Ok(())
        } else {
            Err(VolumeError::NotFound(snapshot_id.to_string()))
        }
    }
}

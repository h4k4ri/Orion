use anyhow::Result;
use async_trait::async_trait;

pub mod ceph;
pub mod lvm;
pub mod mock;

pub use ceph::CephBackend;
pub use lvm::LvmBackend;
pub use mock::MockBackend;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct VolumeHandle {
    pub volume_id: String,
    pub device_path: String,
    pub backend: BackendType,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum BackendType {
    LVM,
    Ceph,
    Mock,
}

impl std::fmt::Display for BackendType {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            BackendType::LVM => write!(f, "lvm"),
            BackendType::Ceph => write!(f, "ceph"),
            BackendType::Mock => write!(f, "mock"),
        }
    }
}

#[derive(Debug, Clone)]
pub struct VolumeSpec {
    pub volume_id: String,
    pub size_gb: i32,
    pub backend: BackendType,
    pub pool: Option<String>,
}

pub type DevicePath = String;

#[async_trait]
pub trait VolumeBackend: Send + Sync {
    fn name(&self) -> &'static str;
    fn backend_type(&self) -> BackendType;

    async fn create(&self, spec: &VolumeSpec) -> Result<VolumeHandle, VolumeError>;
    async fn delete(&self, volume_id: &str) -> Result<(), VolumeError>;
    async fn attach(&self, volume: &VolumeHandle, node_id: &str)
        -> Result<DevicePath, VolumeError>;
    async fn detach(&self, volume: &VolumeHandle, node_id: &str) -> Result<(), VolumeError>;
    async fn exists(&self, volume_id: &str) -> Result<bool, VolumeError>;
    async fn create_snapshot(&self, volume_id: &str, snapshot_id: &str) -> Result<(), VolumeError>;
    async fn delete_snapshot(&self, volume_id: &str, snapshot_id: &str) -> Result<(), VolumeError>;
    async fn restore_snapshot(&self, volume_id: &str, snapshot_id: &str)
        -> Result<(), VolumeError>;
}

#[derive(Debug, thiserror::Error)]
pub enum VolumeError {
    #[error("volume not found: {0}")]
    NotFound(String),

    #[error("volume already exists: {0}")]
    AlreadyExists(String),

    #[error("backend unavailable: {0}")]
    BackendUnavailable(String),

    #[error("invalid argument: {0}")]
    InvalidArgument(String),

    #[error("operation failed: {0}")]
    OperationFailed(String),
}

impl VolumeError {
    pub fn is_not_found(&self) -> bool {
        matches!(self, VolumeError::NotFound(_))
    }

    pub fn is_already_exists(&self) -> bool {
        matches!(self, VolumeError::AlreadyExists(_))
    }

    pub fn is_retryable(&self) -> bool {
        matches!(
            self,
            VolumeError::BackendUnavailable(_) | VolumeError::OperationFailed(_)
        )
    }
}

pub fn parse_backend(backend_type: &str) -> BackendType {
    match backend_type.to_lowercase().as_str() {
        "lvm" => BackendType::LVM,
        "ceph" | "rbd" => BackendType::Ceph,
        "mock" => BackendType::Mock,
        _ => BackendType::LVM,
    }
}

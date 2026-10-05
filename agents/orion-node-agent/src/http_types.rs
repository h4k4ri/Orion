use std::path::PathBuf;

use serde::{Deserialize, Serialize};

#[derive(Clone)]
pub struct AppState {
    pub storage_dir: std::sync::Arc<PathBuf>,
}

#[derive(Debug, Deserialize)]
pub struct BuildServerRequest {
    pub server_id: String,
    pub host_id: String,
    pub name: String,
    pub image: ImageSource,
    #[serde(default)]
    pub ports: Vec<NetworkPort>,
    pub vcpus: u32,
    pub memory_mb: u32,
    pub disk_gb: u32,
}

#[derive(Debug, Deserialize)]
pub struct ImageSource {
    pub id: String,
    #[serde(default)]
    pub source_path: String,
    #[serde(default)]
    pub source_url: String,
    #[serde(default)]
    pub checksum_sha256: String,
    #[serde(default)]
    pub size_bytes: i64,
}

#[derive(Debug, Deserialize)]
pub struct NetworkPort {
    pub id: String,
    #[serde(default)]
    pub mac_address: String,
}

#[derive(Debug, Serialize)]
pub struct BuildServerResponse {
    pub domain_name: String,
    pub status: String,
    pub disk_path: String,
}

#[derive(Debug, Serialize)]
pub struct ServerStatusResponse {
    pub server_id: String,
    pub domain_name: String,
    pub status: String,
}

#[derive(Debug, Deserialize)]
pub struct AttachVolumeRequest {
    pub volume_id: String,
    #[serde(default)]
    pub device_path: String,
}

#[derive(Debug, Deserialize)]
pub struct DetachVolumeRequest {
    pub volume_id: String,
    #[serde(default)]
    pub device_path: String,
}

#[derive(Debug, Serialize)]
pub struct VolumeActionResponse {
    pub status: &'static str,
}

#[derive(Debug, Deserialize)]
pub struct VolumeActionRequest {
    pub volume_id: String,
    #[serde(default)]
    pub device_path: String,
}

#[derive(Debug, Serialize)]
pub struct DeleteServerResponse {
    pub success: bool,
}

#[derive(Debug, Serialize)]
pub struct HealthResponse {
    pub status: &'static str,
}

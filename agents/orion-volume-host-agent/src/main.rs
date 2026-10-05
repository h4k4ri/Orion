use std::{collections::HashMap, env, fs, path::PathBuf, sync::Arc};

use anyhow::{anyhow, Context};
use axum::{
    extract::{Path, State},
    http::StatusCode,
    routing::{get, post},
    Json, Router,
};
use futures_util::StreamExt;
use serde::{Deserialize, Serialize};
use tonic::{Request, Response, Status};
use tower_http::trace::TraceLayer;

mod backend;
#[cfg(test)]
mod backend_tests;
mod generated;

use backend::{
    parse_backend, BackendType, CephBackend, LvmBackend, MockBackend, VolumeBackend, VolumeSpec,
};
use generated::volume::{
    volume_host_agent_server::{VolumeHostAgent, VolumeHostAgentServer},
    CreateSnapshotRequest, CreateVolumeRequest as GrpcCreateVolumeRequest, DeleteSnapshotRequest,
    DeleteSnapshotResponse, DeleteVolumeRequest, DeleteVolumeResponse, HealthRequest,
    HealthResponse as GrpcHealthResponse, ObserveVolumeRequest, RestoreSnapshotRequest,
    SnapshotResponse, VolumeResponse,
};

#[derive(Clone)]
struct AppState {
    backend: Arc<dyn VolumeBackend>,
    backend_name: String,
    healthy: bool,
}

#[derive(Debug, Deserialize)]
struct CreateVolumeRequest {
    volume_id: String,
    size_gb: i32,
}
#[derive(Debug, Serialize)]
struct CreateVolumeResponse {
    volume_id: String,
    device_path: String,
    status: &'static str,
}
#[derive(Debug, Serialize)]
struct VolumeStatusResponse {
    volume_id: String,
    device_path: String,
    status: &'static str,
}
#[derive(Debug, Serialize)]
struct HealthResponse {
    status: &'static str,
    backend: String,
}
#[derive(Debug, Deserialize)]
struct DesiredVolume {
    volume_id: String,
    size_gb: i32,
    #[serde(default)]
    desired_state: String,
}
#[derive(Debug, Deserialize)]
struct DesiredVolumesUpdate {
    #[serde(default)]
    volumes: HashMap<String, DesiredVolume>,
    #[serde(default)]
    deleted_volumes: Vec<String>,
}

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    orion_host_kit::init_logging("orion-volume-host-agent");
    let vg_name = env::var("ORION_VOLUME_LVM_VG").unwrap_or_default();
    let state_dir = env::var("ORION_VOLUME_HOST_AGENT_STATE_DIR")
        .map(PathBuf::from)
        .unwrap_or_else(|_| PathBuf::from("/var/tmp/orion-volume-host-agent"));
    fs::create_dir_all(&state_dir)
        .with_context(|| format!("failed to create {}", state_dir.display()))?;
    let backend_type =
        parse_backend(&env::var("ORION_VOLUME_BACKEND").unwrap_or_else(|_| "lvm".to_string()));
    let healthy = backend_type != BackendType::LVM || !vg_name.trim().is_empty();
    let backend: Arc<dyn VolumeBackend> = match backend_type {
        BackendType::LVM => Arc::new(LvmBackend::new(vg_name, state_dir.clone())),
        BackendType::Ceph => {
            let pool = env::var("ORION_VOLUME_CEPH_POOL").unwrap_or_else(|_| "orion".to_string());
            let backend = CephBackend::new(pool, state_dir.clone());
            match env::var("ORION_VOLUME_CEPH_NAMESPACE") {
                Ok(namespace) if !namespace.trim().is_empty() => {
                    Arc::new(backend.with_namespace(namespace))
                }
                _ => Arc::new(backend),
            }
        }
        BackendType::Mock => Arc::new(MockBackend::new()),
    };
    let app_state = AppState {
        backend,
        backend_name: backend_type.to_string(),
        healthy,
    };
    let nats_url =
        env::var("ORION_NATS_URL").unwrap_or_else(|_| "nats://127.0.0.1:4222".to_string());
    tokio::spawn(start_desired_state_listener(nats_url, app_state.clone()));
    let app = Router::new()
        .route("/healthz", get(health))
        .route("/metrics", get(metrics))
        .route("/v1/volumes", post(create_volume))
        .route(
            "/v1/volumes/{volume_id}",
            get(get_volume).delete(delete_volume),
        )
        .with_state(app_state.clone());
    let addr = env::var("ORION_VOLUME_HOST_AGENT_LISTEN_ADDR")
        .unwrap_or_else(|_| "0.0.0.0:8088".to_string());
    let grpc_addr = env::var("ORION_VOLUME_HOST_AGENT_GRPC_ADDR")
        .unwrap_or_else(|_| "0.0.0.0:50055".to_string());
    let listener = tokio::net::TcpListener::bind(&addr)
        .await
        .with_context(|| format!("failed to bind {addr}"))?;
    let grpc_addr = grpc_addr.parse().context("invalid volume gRPC address")?;
    tracing::info!(addr, %grpc_addr, backend = %app_state.backend_name, "volume host agent listening");
    let http_server = axum::serve(listener, app);
    let mut grpc_builder = tonic::transport::Server::builder().layer(TraceLayer::new_for_grpc());
    if let Some(tls) = orion_host_kit::tls::server_tls_config_from_env()? {
        grpc_builder = grpc_builder.tls_config(tls)?;
    }
    let grpc_server = grpc_builder
        .add_service(VolumeHostAgentServer::new(VolumeGrpcService {
            state: app_state,
        }))
        .serve(grpc_addr);
    tokio::select! { _ = tokio::signal::ctrl_c() => tracing::info!("volume host agent shutdown requested"), result = http_server => result.context("volume HTTP server exited")?, result = grpc_server => result.context("volume gRPC server exited")? }
    Ok(())
}

async fn start_desired_state_listener(nats_url: String, state: AppState) {
    let client = match async_nats::connect(&nats_url).await {
        Ok(client) => client,
        Err(error) => {
            tracing::error!(error = %error, "volume desired-state NATS connection failed");
            return;
        }
    };
    let jetstream = async_nats::jetstream::new(client);
    let stream = match jetstream
        .get_or_create_stream(async_nats::jetstream::stream::Config {
            name: "ORION_DESIRED_STATE".to_string(),
            subjects: vec![
                "orion.desired.compute.>".to_string(),
                "orion.desired.volume.>".to_string(),
            ],
            ..Default::default()
        })
        .await
    {
        Ok(stream) => stream,
        Err(error) => {
            tracing::error!(error = %error, "volume desired-state stream unavailable");
            return;
        }
    };
    let consumer = match stream
        .get_or_create_consumer(
            "volume-host-agent-desired-state",
            async_nats::jetstream::consumer::pull::Config {
                durable_name: Some("volume-host-agent-desired-state".to_string()),
                filter_subject: "orion.desired.volume.>".to_string(),
                ack_policy: async_nats::jetstream::consumer::AckPolicy::Explicit,
                ..Default::default()
            },
        )
        .await
    {
        Ok(consumer) => consumer,
        Err(error) => {
            tracing::error!(error = %error, "volume desired-state consumer unavailable");
            return;
        }
    };
    let mut messages = match consumer.messages().await {
        Ok(messages) => messages,
        Err(error) => {
            tracing::error!(error = %error, "volume desired-state subscription failed");
            return;
        }
    };
    while let Some(Ok(message)) = messages.next().await {
        if let Ok(update) = serde_json::from_slice::<DesiredVolumesUpdate>(&message.message.payload)
        {
            for volume in update.volumes.into_values() {
                let result = if volume.desired_state.eq_ignore_ascii_case("deleted") {
                    state.backend.delete(&volume.volume_id).await.map(|_| ())
                } else {
                    state
                        .backend
                        .create(&VolumeSpec {
                            volume_id: volume.volume_id.clone(),
                            size_gb: volume.size_gb,
                            backend: state.backend.backend_type(),
                            pool: None,
                        })
                        .await
                        .map(|_| ())
                };
                if let Err(error) = result {
                    tracing::error!(volume_id = %volume.volume_id, error = %error, "desired volume reconcile failed");
                }
            }
            for volume_id in update.deleted_volumes {
                if let Err(error) = state.backend.delete(&volume_id).await {
                    tracing::debug!(volume_id = %volume_id, error = %error, "desired volume delete skipped");
                }
            }
        }
        let _ = message.ack().await;
    }
}

async fn health(State(state): State<AppState>) -> Json<HealthResponse> {
    Json(HealthResponse {
        status: if state.healthy { "ok" } else { "degraded" },
        backend: state.backend_name,
    })
}
async fn metrics() -> &'static str {
    "# HELP orion_volume_agent_operations_total Volume host-agent operations.\n# TYPE orion_volume_agent_operations_total counter\norion_volume_agent_operations_total 0\n"
}

#[derive(Clone)]
struct VolumeGrpcService {
    state: AppState,
}

#[tonic::async_trait]
impl VolumeHostAgent for VolumeGrpcService {
    async fn create_volume(
        &self,
        request: Request<GrpcCreateVolumeRequest>,
    ) -> Result<Response<VolumeResponse>, Status> {
        let req = request.into_inner();
        let response = create_volume_backend(
            &self.state,
            CreateVolumeRequest {
                volume_id: req.volume_id,
                size_gb: req.size_gb,
            },
        )
        .await
        .map_err(map_grpc_error)?;
        Ok(Response::new(VolumeResponse {
            volume_id: response.volume_id,
            device_path: response.device_path,
            status: response.status.to_string(),
            exists: true,
        }))
    }
    async fn delete_volume(
        &self,
        request: Request<DeleteVolumeRequest>,
    ) -> Result<Response<DeleteVolumeResponse>, Status> {
        delete_volume_backend(&self.state, &request.into_inner().volume_id)
            .await
            .map_err(map_grpc_error)?;
        Ok(Response::new(DeleteVolumeResponse { deleted: true }))
    }
    async fn observe_volume(
        &self,
        request: Request<ObserveVolumeRequest>,
    ) -> Result<Response<VolumeResponse>, Status> {
        let response = observe_volume_backend(&self.state, &request.into_inner().volume_id)
            .await
            .map_err(map_grpc_error)?;
        Ok(Response::new(VolumeResponse {
            volume_id: response.volume_id,
            device_path: response.device_path,
            status: response.status.to_string(),
            exists: true,
        }))
    }
    async fn create_snapshot(
        &self,
        request: Request<CreateSnapshotRequest>,
    ) -> Result<Response<SnapshotResponse>, Status> {
        let req = request.into_inner();
        self.state
            .backend
            .create_snapshot(&req.volume_id, &req.snapshot_id)
            .await
            .map_err(|error| map_grpc_error(anyhow!(error.to_string())))?;
        Ok(Response::new(SnapshotResponse {
            volume_id: req.volume_id,
            snapshot_id: req.snapshot_id,
            status: "available".to_string(),
        }))
    }
    async fn delete_snapshot(
        &self,
        request: Request<DeleteSnapshotRequest>,
    ) -> Result<Response<DeleteSnapshotResponse>, Status> {
        let req = request.into_inner();
        self.state
            .backend
            .delete_snapshot(&req.volume_id, &req.snapshot_id)
            .await
            .map_err(|error| map_grpc_error(anyhow!(error.to_string())))?;
        Ok(Response::new(DeleteSnapshotResponse { deleted: true }))
    }
    async fn restore_snapshot(
        &self,
        request: Request<RestoreSnapshotRequest>,
    ) -> Result<Response<SnapshotResponse>, Status> {
        let req = request.into_inner();
        self.state
            .backend
            .restore_snapshot(&req.volume_id, &req.snapshot_id)
            .await
            .map_err(|error| map_grpc_error(anyhow!(error.to_string())))?;
        Ok(Response::new(SnapshotResponse {
            volume_id: req.volume_id,
            snapshot_id: req.snapshot_id,
            status: "restored".to_string(),
        }))
    }
    async fn health(
        &self,
        _request: Request<HealthRequest>,
    ) -> Result<Response<GrpcHealthResponse>, Status> {
        Ok(Response::new(GrpcHealthResponse {
            healthy: self.state.healthy,
            message: format!("backend={}", self.state.backend_name),
        }))
    }
}

async fn create_volume_backend(
    state: &AppState,
    req: CreateVolumeRequest,
) -> anyhow::Result<CreateVolumeResponse> {
    let handle = state
        .backend
        .create(&VolumeSpec {
            volume_id: req.volume_id.clone(),
            size_gb: req.size_gb,
            backend: state.backend.backend_type(),
            pool: None,
        })
        .await
        .map_err(|error| anyhow!(error.to_string()))?;
    Ok(CreateVolumeResponse {
        volume_id: req.volume_id,
        device_path: handle.device_path,
        status: "available",
    })
}
async fn delete_volume_backend(state: &AppState, volume_id: &str) -> anyhow::Result<()> {
    state
        .backend
        .delete(volume_id)
        .await
        .map_err(|error| anyhow!(error.to_string()))
}
async fn observe_volume_backend(
    state: &AppState,
    volume_id: &str,
) -> anyhow::Result<VolumeStatusResponse> {
    if !state
        .backend
        .exists(volume_id)
        .await
        .map_err(|error| anyhow!(error.to_string()))?
    {
        return Err(anyhow!("volume not found"));
    }
    let handle = state
        .backend
        .create(&VolumeSpec {
            volume_id: volume_id.to_string(),
            size_gb: 1,
            backend: state.backend.backend_type(),
            pool: None,
        })
        .await
        .map_err(|error| anyhow!(error.to_string()))?;
    Ok(VolumeStatusResponse {
        volume_id: volume_id.to_string(),
        device_path: handle.device_path,
        status: "available",
    })
}

async fn create_volume(
    State(state): State<AppState>,
    Json(req): Json<CreateVolumeRequest>,
) -> Result<(StatusCode, Json<CreateVolumeResponse>), (StatusCode, String)> {
    create_volume_backend(&state, req)
        .await
        .map(|response| (StatusCode::CREATED, Json(response)))
        .map_err(http_error)
}
async fn delete_volume(
    State(state): State<AppState>,
    Path(volume_id): Path<String>,
) -> Result<StatusCode, (StatusCode, String)> {
    delete_volume_backend(&state, &volume_id)
        .await
        .map(|_| StatusCode::NO_CONTENT)
        .map_err(http_error)
}
async fn get_volume(
    State(state): State<AppState>,
    Path(volume_id): Path<String>,
) -> Result<Json<VolumeStatusResponse>, (StatusCode, String)> {
    observe_volume_backend(&state, &volume_id)
        .await
        .map(Json)
        .map_err(http_error)
}

fn map_grpc_error(error: anyhow::Error) -> Status {
    let message = error.to_string();
    if message.contains("not found") {
        Status::not_found(message)
    } else if message.contains("unavailable") || message.contains("required") {
        Status::unavailable(message)
    } else if message.contains("invalid") || message.contains("positive") {
        Status::invalid_argument(message)
    } else {
        Status::internal(message)
    }
}
fn http_error(error: anyhow::Error) -> (StatusCode, String) {
    let message = error.to_string();
    let status = if message.contains("not found") {
        StatusCode::NOT_FOUND
    } else if message.contains("unavailable") || message.contains("required") {
        StatusCode::SERVICE_UNAVAILABLE
    } else if message.contains("invalid") || message.contains("positive") {
        StatusCode::BAD_REQUEST
    } else {
        StatusCode::INTERNAL_SERVER_ERROR
    };
    (status, message)
}

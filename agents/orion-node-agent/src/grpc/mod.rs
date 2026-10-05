use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::Arc;
use std::time::Instant;

use async_trait::async_trait;
use tonic::{Request, Response, Status};
use tracing::Instrument;

use crate::generated::node::node_agent_service_server::{NodeAgentService, NodeAgentServiceServer};
use crate::state::AgentState;

pub struct NodeAgentGrpcService {
    state: AgentState,
    metrics: Arc<GrpcMetrics>,
}

impl NodeAgentGrpcService {
    pub fn new(state: AgentState) -> Self {
        Self {
            state,
            metrics: Arc::new(GrpcMetrics::new()),
        }
    }

    pub fn into_service(self) -> NodeAgentServiceServer<Self> {
        NodeAgentServiceServer::new(self)
    }

    pub fn metrics(&self) -> Arc<GrpcMetrics> {
        self.metrics.clone()
    }
}

#[derive(Clone)]
pub struct GrpcMetrics {
    requests_total: Arc<AtomicU64>,
    errors_total: Arc<AtomicU64>,
    duration_micros_total: Arc<AtomicU64>,
}

impl GrpcMetrics {
    pub fn new() -> Self {
        Self {
            requests_total: Arc::new(AtomicU64::new(0)),
            errors_total: Arc::new(AtomicU64::new(0)),
            duration_micros_total: Arc::new(AtomicU64::new(0)),
        }
    }

    pub fn inc_requests(&self) {
        self.requests_total.fetch_add(1, Ordering::Relaxed);
    }

    pub fn observe<T>(&self, elapsed: f64, result: &Result<Response<T>, Status>) {
        self.duration_micros_total
            .fetch_add((elapsed.max(0.0) * 1_000_000.0) as u64, Ordering::Relaxed);
        if result.is_err() {
            self.errors_total.fetch_add(1, Ordering::Relaxed);
        }
    }

    pub fn prometheus(&self) -> String {
        format!(
            "# HELP orion_grpc_requests_total Total gRPC requests handled.\n# TYPE orion_grpc_requests_total counter\norion_grpc_requests_total {}\n",
            self.requests_total.load(Ordering::Relaxed)
        ) + &format!(
            "# HELP orion_grpc_errors_total Total failed gRPC requests.\n# TYPE orion_grpc_errors_total counter\norion_grpc_errors_total {}\n# HELP orion_grpc_duration_micros_total Total gRPC request duration in microseconds.\n# TYPE orion_grpc_duration_micros_total counter\norion_grpc_duration_micros_total {}\n",
            self.errors_total.load(Ordering::Relaxed),
            self.duration_micros_total.load(Ordering::Relaxed)
        )
    }
}

fn get_request_id<T>(req: &Request<T>) -> Option<String> {
    req.metadata()
        .get("x-request-id")
        .or_else(|| req.metadata().get("request-id"))
        .and_then(|v| v.to_str().ok())
        .map(|s| s.to_string())
}

fn code_to_status(code: tonic::Code) -> &'static str {
    match code {
        tonic::Code::Ok => "ok",
        tonic::Code::InvalidArgument => "invalid_argument",
        tonic::Code::NotFound => "not_found",
        tonic::Code::Internal => "internal",
        _ => "unknown",
    }
}

#[async_trait]
impl NodeAgentService for NodeAgentGrpcService {
    async fn register_node(
        &self,
        request: Request<crate::generated::node::RegisterNodeRequest>,
    ) -> Result<Response<crate::generated::node::RegisterNodeResponse>, Status> {
        let request_id = get_request_id(&request);
        let span = tracing::info_span!("grpc.register_node", request_id = ?request_id);
        let metrics = self.metrics.clone();
        let start = Instant::now();

        async move {
            metrics.inc_requests();
            let req = request.into_inner();
            let node_id = req.node.as_ref().and_then(|n| n.id.as_ref()).map(|i| i.value.clone());
            tracing::info!(node_id = ?node_id, "register_node");
            self.state.set_libvirt_healthy(true);
            let result: Result<Response<crate::generated::node::RegisterNodeResponse>, Status> = Ok(Response::new(crate::generated::node::RegisterNodeResponse {
                node_id: req.node.and_then(|n| n.id),
                accepted: true,
                message: String::new(),
            }));
            let elapsed = start.elapsed().as_secs_f64();
            metrics.observe(elapsed, &result);
            match &result {
                Ok(_) => tracing::info!(method = "RegisterNode", status = "ok", duration_ms = elapsed * 1000.0, "request completed"),
                Err(e) => tracing::error!(method = "RegisterNode", status = %code_to_status(e.code()), duration_ms = elapsed * 1000.0, error = %e, "request failed"),
            }
            result
        }
        .instrument(span)
        .await
    }

    async fn heartbeat(
        &self,
        request: Request<crate::generated::node::HeartbeatRequest>,
    ) -> Result<Response<crate::generated::node::HeartbeatResponse>, Status> {
        let request_id = get_request_id(&request);
        let span = tracing::info_span!("grpc.heartbeat", request_id = ?request_id);
        let metrics = self.metrics.clone();
        let start = Instant::now();

        async move {
            metrics.inc_requests();
            let req = request.into_inner();
            tracing::debug!(node_id = ?req.node_id.as_ref().map(|n| &n.value), "heartbeat");
            self.state.set_libvirt_healthy(true);
            self.state.record_heartbeat();
            let result: Result<Response<crate::generated::node::HeartbeatResponse>, Status> = Ok(Response::new(crate::generated::node::HeartbeatResponse {
                ok: true,
                desired_updates: vec![],
            }));
            let elapsed = start.elapsed().as_secs_f64();
            metrics.observe(elapsed, &result);
            match &result {
                Ok(_) => tracing::info!(method = "Heartbeat", status = "ok", duration_ms = elapsed * 1000.0, "request completed"),
                Err(e) => tracing::error!(method = "Heartbeat", status = %code_to_status(e.code()), duration_ms = elapsed * 1000.0, error = %e, "request failed"),
            }
            result
        }
        .instrument(span)
        .await
    }

    async fn get_inventory(
        &self,
        request: Request<crate::generated::node::GetInventoryRequest>,
    ) -> Result<Response<crate::generated::node::GetInventoryResponse>, Status> {
        let request_id = get_request_id(&request);
        let span = tracing::info_span!("grpc.get_inventory", request_id = ?request_id);
        let metrics = self.metrics.clone();
        let start = Instant::now();

        async move {
            metrics.inc_requests();
            let req = request.into_inner();
            tracing::info!(node_id = ?req.node_id.as_ref().map(|n| &n.value), "get_inventory");
            let result: Result<Response<crate::generated::node::GetInventoryResponse>, Status> = Ok(Response::new(crate::generated::node::GetInventoryResponse {
                resources: Some(crate::generated::node::NodeResources {
                    cpu: Some(crate::generated::node::CpuResources { total: 64, reserved: 0, allocated: 0 }),
                    memory: Some(crate::generated::node::MemoryResources { total_mb: 262144, reserved_mb: 0, allocated_mb: 0 }),
                    disk: Some(crate::generated::node::DiskResources { total_gb: 4096, reserved_gb: 0, allocated_gb: 0 }),
                }),
                traits: Some(crate::generated::node::NodeTraits {
                    traits: vec!["KVM".to_string()],
                    hypervisors: vec!["qemu".to_string()],
                    cpu_vendors: vec![],
                    network_capabilities: vec!["OVS".to_string()],
                    storage_capabilities: vec!["LVM".to_string()],
                }),
                instances: vec![],
            }));
            let elapsed = start.elapsed().as_secs_f64();
            metrics.observe(elapsed, &result);
            match &result {
                Ok(_) => tracing::info!(method = "GetInventory", status = "ok", duration_ms = elapsed * 1000.0, "request completed"),
                Err(e) => tracing::error!(method = "GetInventory", status = %code_to_status(e.code()), duration_ms = elapsed * 1000.0, error = %e, "request failed"),
            }
            result
        }
        .instrument(span)
        .await
    }

    async fn build_instance(
        &self,
        request: Request<crate::generated::node::BuildInstanceRequest>,
    ) -> Result<Response<crate::generated::node::BuildInstanceResponse>, Status> {
        let request_id = get_request_id(&request);
        let span = tracing::info_span!("grpc.build_instance", request_id = ?request_id);
        let metrics = self.metrics.clone();
        let state = self.state.clone();
        let start = Instant::now();

        async move {
            metrics.inc_requests();
            let req = request.into_inner();
            let result = Self::do_build_instance(req, state).await;
            let elapsed = start.elapsed().as_secs_f64();
            metrics.observe(elapsed, &result);
            match &result {
                Ok(_) => tracing::info!(method = "BuildInstance", status = "ok", duration_ms = elapsed * 1000.0, "request completed"),
                Err(e) => tracing::error!(method = "BuildInstance", status = %code_to_status(e.code()), duration_ms = elapsed * 1000.0, error = %e, "request failed"),
            }
            result
        }
        .instrument(span)
        .await
    }

    async fn destroy_instance(
        &self,
        request: Request<crate::generated::node::DestroyInstanceRequest>,
    ) -> Result<Response<crate::generated::node::DestroyInstanceResponse>, Status> {
        let request_id = get_request_id(&request);
        let span = tracing::info_span!("grpc.destroy_instance", request_id = ?request_id);
        let metrics = self.metrics.clone();
        let state = self.state.clone();
        let start = Instant::now();

        async move {
            metrics.inc_requests();
            let req = request.into_inner();
            let result = Self::do_destroy_instance(req, state).await;
            let elapsed = start.elapsed().as_secs_f64();
            metrics.observe(elapsed, &result);
            match &result {
                Ok(_) => tracing::info!(method = "DestroyInstance", status = "ok", duration_ms = elapsed * 1000.0, "request completed"),
                Err(e) => tracing::error!(method = "DestroyInstance", status = %code_to_status(e.code()), duration_ms = elapsed * 1000.0, error = %e, "request failed"),
            }
            result
        }
        .instrument(span)
        .await
    }

    async fn observe_instance(
        &self,
        request: Request<crate::generated::node::ObserveInstanceRequest>,
    ) -> Result<Response<crate::generated::node::ObserveInstanceResponse>, Status> {
        let request_id = get_request_id(&request);
        let span = tracing::info_span!("grpc.observe_instance", request_id = ?request_id);
        let metrics = self.metrics.clone();
        let start = Instant::now();

        async move {
            metrics.inc_requests();
            let req = request.into_inner();
            let result = Self::do_observe_instance(req).await;
            let elapsed = start.elapsed().as_secs_f64();
            metrics.observe(elapsed, &result);
            match &result {
                Ok(_) => tracing::info!(method = "ObserveInstance", status = "ok", duration_ms = elapsed * 1000.0, "request completed"),
                Err(e) => tracing::error!(method = "ObserveInstance", status = %code_to_status(e.code()), duration_ms = elapsed * 1000.0, error = %e, "request failed"),
            }
            result
        }
        .instrument(span)
        .await
    }

    async fn attach_volume(
        &self,
        request: Request<crate::generated::node::AttachVolumeRequest>,
    ) -> Result<Response<crate::generated::node::AttachVolumeResponse>, Status> {
        let request_id = get_request_id(&request);
        let span = tracing::info_span!("grpc.attach_volume", request_id = ?request_id);
        let metrics = self.metrics.clone();
        let state = self.state.clone();
        let start = Instant::now();

        async move {
            metrics.inc_requests();
            let req = request.into_inner();
            let result = Self::do_attach_volume(req, state).await;
            let elapsed = start.elapsed().as_secs_f64();
            metrics.observe(elapsed, &result);
            match &result {
                Ok(_) => tracing::info!(method = "AttachVolume", status = "ok", duration_ms = elapsed * 1000.0, "request completed"),
                Err(e) => tracing::error!(method = "AttachVolume", status = %code_to_status(e.code()), duration_ms = elapsed * 1000.0, error = %e, "request failed"),
            }
            result
        }
        .instrument(span)
        .await
    }

    async fn detach_volume(
        &self,
        request: Request<crate::generated::node::DetachVolumeRequest>,
    ) -> Result<Response<crate::generated::node::DetachVolumeResponse>, Status> {
        let request_id = get_request_id(&request);
        let span = tracing::info_span!("grpc.detach_volume", request_id = ?request_id);
        let metrics = self.metrics.clone();
        let state = self.state.clone();
        let start = Instant::now();

        async move {
            metrics.inc_requests();
            let req = request.into_inner();
            let result = Self::do_detach_volume(req, state).await;
            let elapsed = start.elapsed().as_secs_f64();
            metrics.observe(elapsed, &result);
            match &result {
                Ok(_) => tracing::info!(method = "DetachVolume", status = "ok", duration_ms = elapsed * 1000.0, "request completed"),
                Err(e) => tracing::error!(method = "DetachVolume", status = %code_to_status(e.code()), duration_ms = elapsed * 1000.0, error = %e, "request failed"),
            }
            result
        }
        .instrument(span)
        .await
    }

    async fn health(
        &self,
        request: Request<()>,
    ) -> Result<Response<crate::generated::node::HealthResponse>, Status> {
        let request_id = get_request_id(&request);
        let span = tracing::info_span!("grpc.health", request_id = ?request_id);
        async move {
            let libvirt_ok = crate::domain::open_libvirt().is_ok();
            self.state.set_libvirt_healthy(libvirt_ok);
            let status_msg = if libvirt_ok {
                "connected"
            } else {
                "disconnected"
            };
            Ok(Response::new(crate::generated::node::HealthResponse {
                healthy: libvirt_ok,
                version: "0.1.0".to_string(),
                libvirt_status: status_msg.to_string(),
            }))
        }
        .instrument(span)
        .await
    }
}

impl NodeAgentGrpcService {
    async fn do_build_instance(
        req: crate::generated::node::BuildInstanceRequest,
        state: AgentState,
    ) -> Result<Response<crate::generated::node::BuildInstanceResponse>, Status> {
        let instance_id = req
            .instance_id
            .as_ref()
            .map(|i| i.value.clone())
            .unwrap_or_default();
        let host_id = req
            .host_id
            .as_ref()
            .map(|h| h.value.clone())
            .unwrap_or_default();
        let name = req.name.clone();
        tracing::info!(instance_id = %instance_id, host_id = %host_id, "build_instance");

        let image = req.spec.as_ref().and_then(|s| s.image.as_ref());
        let ports = req
            .spec
            .as_ref()
            .map(|s| s.ports.clone())
            .unwrap_or_default();
        let vcpus = req.spec.as_ref().map(|s| s.vcpus).unwrap_or(0) as u32;
        let memory_mb = req.spec.as_ref().map(|s| s.memory_mb).unwrap_or(0) as u32;
        let disk_gb = req.spec.as_ref().map(|s| s.disk_gb).unwrap_or(0) as u32;

        let http_req = crate::http_types::BuildServerRequest {
            server_id: instance_id.clone(),
            host_id: host_id.clone(),
            name: name.clone(),
            image: crate::http_types::ImageSource {
                id: image
                    .as_ref()
                    .and_then(|i| i.id.as_ref())
                    .map(|id| id.value.clone())
                    .unwrap_or_default(),
                source_path: image
                    .as_ref()
                    .map(|i| i.source_path.clone())
                    .unwrap_or_default(),
                source_url: image
                    .as_ref()
                    .map(|i| i.source_url.clone())
                    .unwrap_or_default(),
                checksum_sha256: image
                    .as_ref()
                    .map(|i| i.checksum_sha256.clone())
                    .unwrap_or_default(),
                size_bytes: image.as_ref().map(|i| i.size_bytes).unwrap_or(0),
            },
            ports: ports
                .iter()
                .map(|p| crate::http_types::NetworkPort {
                    id: p.id.as_ref().map(|i| i.value.clone()).unwrap_or_default(),
                    mac_address: p.mac_address.clone(),
                })
                .collect(),
            vcpus,
            memory_mb,
            disk_gb,
        };

        let storage_dir = state.storage_dir.clone();
        let result = tokio::task::spawn_blocking(move || {
            crate::sync::build_server_sync(&storage_dir, http_req)
        })
        .await
        .map_err(|e| Status::internal(format!("task join error: {e}")))?
        .map_err(|e| Status::internal(e.to_string()))?;

        Ok(Response::new(
            crate::generated::node::BuildInstanceResponse {
                domain_name: result.domain_name,
                status: result.status,
                disk_path: result.disk_path,
                network_ports: vec![],
            },
        ))
    }

    async fn do_destroy_instance(
        req: crate::generated::node::DestroyInstanceRequest,
        state: AgentState,
    ) -> Result<Response<crate::generated::node::DestroyInstanceResponse>, Status> {
        let instance_id = req
            .instance_id
            .as_ref()
            .map(|i| i.value.clone())
            .unwrap_or_default();
        tracing::info!(instance_id = %instance_id, "destroy_instance");

        let storage_dir = state.storage_dir.clone();
        let join_result = tokio::task::spawn_blocking(move || {
            crate::sync::delete_server_sync(&storage_dir, &instance_id)
        })
        .await;

        match join_result {
            Ok(Ok(())) => Ok(Response::new(
                crate::generated::node::DestroyInstanceResponse {
                    success: true,
                    error: String::new(),
                },
            )),
            Ok(Err(e)) => Ok(Response::new(
                crate::generated::node::DestroyInstanceResponse {
                    success: false,
                    error: e.to_string(),
                },
            )),
            Err(e) => Err(Status::internal(format!("task join error: {e}"))),
        }
    }

    async fn do_observe_instance(
        req: crate::generated::node::ObserveInstanceRequest,
    ) -> Result<Response<crate::generated::node::ObserveInstanceResponse>, Status> {
        let instance_id = req
            .instance_id
            .as_ref()
            .map(|i| i.value.clone())
            .unwrap_or_default();
        tracing::info!(instance_id = %instance_id, "observe_instance");

        let result =
            tokio::task::spawn_blocking(move || crate::sync::get_server_sync(&instance_id))
                .await
                .map_err(|e| Status::internal(format!("task join error: {e}")))?
                .map_err(|e| {
                    let msg = e.to_string();
                    if msg.contains("not") && msg.contains("found") {
                        Status::not_found(msg)
                    } else {
                        Status::internal(msg)
                    }
                })?;

        Ok(Response::new(
            crate::generated::node::ObserveInstanceResponse {
                status: result.status.to_string(),
                domain_name: result.domain_name.clone(),
                observed_state: result.status.to_string(),
            },
        ))
    }

    async fn do_attach_volume(
        req: crate::generated::node::AttachVolumeRequest,
        state: AgentState,
    ) -> Result<Response<crate::generated::node::AttachVolumeResponse>, Status> {
        let instance_id = req
            .instance_id
            .as_ref()
            .map(|i| i.value.clone())
            .unwrap_or_default();
        let volume_id = req
            .volume_id
            .as_ref()
            .map(|v| v.value.clone())
            .unwrap_or_default();
        let device_path = req.device_path.clone();
        tracing::info!(instance_id = %instance_id, volume_id = %volume_id, "attach_volume");

        let vol_req = crate::http_types::AttachVolumeRequest {
            volume_id: volume_id.clone(),
            device_path: device_path.clone(),
        };

        let storage_dir = state.storage_dir.clone();
        let join_result = tokio::task::spawn_blocking(move || {
            crate::sync::attach_volume_sync(&instance_id, vol_req)
        })
        .await;

        match join_result {
            Ok(Ok(())) => Ok(Response::new(
                crate::generated::node::AttachVolumeResponse {
                    success: true,
                    device_path,
                    error: String::new(),
                },
            )),
            Ok(Err(e)) => Ok(Response::new(
                crate::generated::node::AttachVolumeResponse {
                    success: false,
                    device_path,
                    error: e.to_string(),
                },
            )),
            Err(e) => Err(Status::internal(format!("task join error: {e}"))),
        }
    }

    async fn do_detach_volume(
        req: crate::generated::node::DetachVolumeRequest,
        state: AgentState,
    ) -> Result<Response<crate::generated::node::DetachVolumeResponse>, Status> {
        let instance_id = req
            .instance_id
            .as_ref()
            .map(|i| i.value.clone())
            .unwrap_or_default();
        let volume_id = req
            .volume_id
            .as_ref()
            .map(|v| v.value.clone())
            .unwrap_or_default();
        tracing::info!(instance_id = %instance_id, volume_id = %volume_id, "detach_volume");

        let vol_req = crate::http_types::DetachVolumeRequest {
            volume_id,
            device_path: String::new(),
        };

        let storage_dir = state.storage_dir.clone();
        let join_result = tokio::task::spawn_blocking(move || {
            crate::sync::detach_volume_sync(&instance_id, vol_req)
        })
        .await;

        match join_result {
            Ok(Ok(())) => Ok(Response::new(
                crate::generated::node::DetachVolumeResponse {
                    success: true,
                    error: String::new(),
                },
            )),
            Ok(Err(e)) => Ok(Response::new(
                crate::generated::node::DetachVolumeResponse {
                    success: false,
                    error: e.to_string(),
                },
            )),
            Err(e) => Err(Status::internal(format!("task join error: {e}"))),
        }
    }
}

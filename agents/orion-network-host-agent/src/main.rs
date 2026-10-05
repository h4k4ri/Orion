use std::{
    collections::{BTreeMap, BTreeSet},
    env, fs,
    path::{Path, PathBuf},
    process::Command,
    sync::mpsc::{self, Receiver, RecvTimeoutError, Sender},
    sync::{Arc, RwLock},
    thread,
    time::{Duration, SystemTime, UNIX_EPOCH},
};

use anyhow::{anyhow, Context};
use axum::{
    extract::{Path as AxumPath, State},
    http::StatusCode,
    routing::get,
    Json, Router,
};
use futures_util::StreamExt;
use reqwest::blocking::Client;
use serde::{de::DeserializeOwned, Deserialize, Serialize};

mod backend;
mod generated;

use backend::{ovs::OvsBackend, NetworkBackend};

use generated::network::{
    network_host_agent_server::{NetworkHostAgent, NetworkHostAgentServer},
    GetResourceRequest, HealthRequest, HealthResponse as GrpcHealthResponse, ResourceResponse,
};
use tonic::{Request, Response, Status};
use tower_http::trace::TraceLayer;

#[derive(Clone)]
struct AppState {
    host_id: String,
    state_dir: PathBuf,
    status: Arc<RwLock<AgentStatus>>,
    backend_name: String,
}

#[derive(Clone)]
struct AgentConfig {
    host_id: String,
    network_url: String,
    nats_url: String,
    state_dir: PathBuf,
    reconcile_interval: Duration,
    client: Client,
    backend: Arc<dyn NetworkBackend>,
}

#[derive(Debug, Clone, Default, Serialize)]
struct AgentStatus {
    last_reconcile_unix: u64,
    last_event_unix: u64,
    last_trigger: String,
    managed_ports: usize,
    managed_networks: usize,
    bridge_ready: bool,
    last_error: String,
}

#[derive(Debug, Deserialize)]
struct RemotePortsResponse {
    ports: Vec<RemotePort>,
}

#[derive(Debug, Deserialize)]
struct ResourceEvent {
    event_type: String,
    resource_id: String,
}

#[derive(Debug, Clone, Deserialize, Serialize)]
struct FixedIP {
    subnet_id: String,
    ip_address: String,
}

#[derive(Debug, Clone, Deserialize)]
struct RemotePort {
    id: String,
    network_id: String,
    device_id: String,
    device_owner: String,
    binding_host_id: String,
    mac_address: String,
    fixed_ips: Vec<FixedIP>,
    status: String,
    vif_type: String,
    vnic_type: String,
}

#[derive(Debug, Serialize)]
struct UpdatePortBindingRequest {
    binding_host_id: String,
    binding_status: String,
    binding_detail: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
struct PortState {
    id: String,
    network_id: String,
    device_id: String,
    device_owner: String,
    binding_host_id: String,
    mac_address: String,
    fixed_ips: Vec<FixedIP>,
    remote_status: String,
    vif_type: String,
    vnic_type: String,
    local_bridge: String,
    local_status: String,
    observed_at_unix: u64,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
struct NetworkState {
    id: String,
    host_id: String,
    port_ids: Vec<String>,
    local_bridge: String,
    local_status: String,
    observed_at_unix: u64,
}

#[derive(Debug, Serialize)]
struct HealthResponse {
    status: &'static str,
    host_id: String,
    last_reconcile_unix: u64,
    last_event_unix: u64,
    last_trigger: String,
    managed_ports: usize,
    managed_networks: usize,
    bridge_ready: bool,
    last_error: String,
}

#[derive(Debug)]
struct DesiredState {
    ports: BTreeMap<String, PortState>,
    networks: BTreeMap<String, NetworkState>,
}

#[derive(Clone, Copy, Debug)]
enum ReconcileTrigger {
    Startup,
    Timer,
    PortEvent,
}

fn main() -> anyhow::Result<()> {
    orion_host_kit::init_logging("orion-network-host-agent");

    let host_id = resolve_host_id()?;
    let network_url =
        env::var("ORION_NETWORK_URL").unwrap_or_else(|_| "http://127.0.0.1:8086".to_string());
    let nats_url =
        env::var("ORION_NATS_URL").unwrap_or_else(|_| "nats://127.0.0.1:4222".to_string());
    let state_dir = env::var("ORION_NETWORK_HOST_AGENT_STATE_DIR")
        .map(PathBuf::from)
        .unwrap_or_else(|_| PathBuf::from("/var/tmp/orion-network-host-agent"));
    let reconcile_interval = Duration::from_secs(
        env::var("ORION_NETWORK_HOST_AGENT_RECONCILE_INTERVAL")
            .ok()
            .and_then(|value| value.parse::<u64>().ok())
            .unwrap_or(15),
    );
    ensure_state_layout(&state_dir)?;

    let status = Arc::new(RwLock::new(AgentStatus::default()));
    let bridge = env::var("ORION_NETWORK_OVS_BRIDGE").unwrap_or_else(|_| "br-int".to_string());
    let backend: Arc<dyn NetworkBackend> = Arc::new(OvsBackend::new(bridge));
    let app_state = AppState {
        host_id: host_id.clone(),
        state_dir: state_dir.clone(),
        status: Arc::clone(&status),
        backend_name: backend.name().to_string(),
    };
    let agent_config = AgentConfig {
        host_id: host_id.clone(),
        network_url,
        nats_url,
        state_dir,
        reconcile_interval,
        backend,
        client: Client::builder()
            .timeout(Duration::from_secs(10))
            .build()
            .context("failed to build network client")?,
    };

    let (trigger_tx, trigger_rx) = mpsc::channel();
    start_reconcile_worker(agent_config.clone(), Arc::clone(&status), trigger_rx);
    let _ = trigger_tx.send(ReconcileTrigger::Startup);
    start_nats_listener(agent_config, trigger_tx);

    let app = Router::new()
        .route("/healthz", get(health))
        .route("/metrics", get(metrics))
        .route("/v1/ports", get(list_ports))
        .route("/v1/ports/{port_id}", get(get_port))
        .route("/v1/networks", get(list_networks))
        .route("/v1/networks/{network_id}", get(get_network))
        .with_state(app_state.clone());

    let addr = env::var("ORION_NETWORK_HOST_AGENT_LISTEN_ADDR")
        .unwrap_or_else(|_| "0.0.0.0:8087".to_string());
    let grpc_addr = env::var("ORION_NETWORK_HOST_AGENT_GRPC_ADDR")
        .unwrap_or_else(|_| "0.0.0.0:50054".to_string());
    let runtime = tokio::runtime::Builder::new_multi_thread()
        .enable_all()
        .build()
        .context("failed to build tokio runtime")?;

    runtime.block_on(async move {
        let listener = tokio::net::TcpListener::bind(&addr)
            .await
            .with_context(|| format!("failed to bind {addr}"))?;

        let grpc_addr = grpc_addr.parse().context("invalid network gRPC address")?;
        tracing::info!(addr, %grpc_addr, host_id, backend = %app_state.backend_name, "network host agent listening");
        let http_server = axum::serve(listener, app);
        let mut grpc_builder = tonic::transport::Server::builder()
            .layer(TraceLayer::new_for_grpc());
        if let Some(tls) = orion_host_kit::tls::server_tls_config_from_env()? {
            grpc_builder = grpc_builder.tls_config(tls)?;
        }
        let grpc_server = grpc_builder
            .add_service(NetworkHostAgentServer::new(NetworkGrpcService {
                state: app_state,
            }))
            .serve(grpc_addr);
        tokio::select! {
            _ = tokio::signal::ctrl_c() => {
                tracing::info!("network host agent shutdown requested");
                Ok(())
            },
            result = http_server => result.context("network HTTP server exited"),
            result = grpc_server => result.context("network gRPC server exited"),
        }
    })
}

async fn metrics() -> &'static str {
    "# HELP orion_network_agent_reconcile_total Network host-agent reconcile runs.\n# TYPE orion_network_agent_reconcile_total counter\norion_network_agent_reconcile_total 0\n"
}

#[derive(Clone)]
struct NetworkGrpcService {
    state: AppState,
}

#[tonic::async_trait]
impl NetworkHostAgent for NetworkGrpcService {
    async fn get_port(
        &self,
        request: Request<GetResourceRequest>,
    ) -> Result<Response<ResourceResponse>, Status> {
        self.get_resource(request.into_inner().resource_id, true)
            .map(Response::new)
    }

    async fn get_network(
        &self,
        request: Request<GetResourceRequest>,
    ) -> Result<Response<ResourceResponse>, Status> {
        self.get_resource(request.into_inner().resource_id, false)
            .map(Response::new)
    }

    async fn health(
        &self,
        _request: Request<HealthRequest>,
    ) -> Result<Response<GrpcHealthResponse>, Status> {
        let status = read_status(&self.state.status);
        Ok(Response::new(GrpcHealthResponse {
            healthy: status.last_error.is_empty(),
            message: if status.last_error.is_empty() {
                "ok".to_string()
            } else {
                status.last_error
            },
        }))
    }
}

impl NetworkGrpcService {
    fn get_resource(&self, resource_id: String, port: bool) -> Result<ResourceResponse, Status> {
        let path = if port {
            ports_dir(&self.state.state_dir).join(state_file_name(&resource_id))
        } else {
            networks_dir(&self.state.state_dir).join(state_file_name(&resource_id))
        };
        match fs::read(&path) {
            Ok(state_json) => Ok(ResourceResponse {
                resource_id,
                state_json,
                exists: true,
            }),
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => {
                Err(Status::not_found("resource not found"))
            }
            Err(error) => Err(Status::internal(error.to_string())),
        }
    }
}

async fn health(State(state): State<AppState>) -> Json<HealthResponse> {
    let status = read_status(&state.status);
    Json(HealthResponse {
        status: if status.last_error.is_empty() {
            "ok"
        } else {
            "degraded"
        },
        host_id: state.host_id,
        last_reconcile_unix: status.last_reconcile_unix,
        last_event_unix: status.last_event_unix,
        last_trigger: status.last_trigger,
        managed_ports: status.managed_ports,
        managed_networks: status.managed_networks,
        bridge_ready: status.bridge_ready,
        last_error: status.last_error,
    })
}

async fn list_ports(
    State(state): State<AppState>,
) -> Result<Json<Vec<PortState>>, (StatusCode, String)> {
    load_collection::<PortState>(&ports_dir(&state.state_dir))
        .map(Json)
        .map_err(internal_error)
}

async fn get_port(
    State(state): State<AppState>,
    AxumPath(port_id): AxumPath<String>,
) -> Result<Json<PortState>, (StatusCode, String)> {
    let path = ports_dir(&state.state_dir).join(state_file_name(&port_id));
    read_json_file::<PortState>(&path).map(Json).map_err(|err| {
        match err.downcast_ref::<std::io::Error>() {
            Some(io_err) if io_err.kind() == std::io::ErrorKind::NotFound => {
                (StatusCode::NOT_FOUND, format!("port {port_id} not found"))
            }
            _ => internal_error(err),
        }
    })
}

async fn list_networks(
    State(state): State<AppState>,
) -> Result<Json<Vec<NetworkState>>, (StatusCode, String)> {
    load_collection::<NetworkState>(&networks_dir(&state.state_dir))
        .map(Json)
        .map_err(internal_error)
}

async fn get_network(
    State(state): State<AppState>,
    AxumPath(network_id): AxumPath<String>,
) -> Result<Json<NetworkState>, (StatusCode, String)> {
    let path = networks_dir(&state.state_dir).join(state_file_name(&network_id));
    read_json_file::<NetworkState>(&path)
        .map(Json)
        .map_err(|err| match err.downcast_ref::<std::io::Error>() {
            Some(io_err) if io_err.kind() == std::io::ErrorKind::NotFound => (
                StatusCode::NOT_FOUND,
                format!("network {network_id} not found"),
            ),
            _ => internal_error(err),
        })
}

fn start_reconcile_worker(
    config: AgentConfig,
    status: Arc<RwLock<AgentStatus>>,
    trigger_rx: Receiver<ReconcileTrigger>,
) {
    thread::spawn(move || loop {
        let trigger = match trigger_rx.recv_timeout(config.reconcile_interval) {
            Ok(trigger) => trigger,
            Err(RecvTimeoutError::Timeout) => ReconcileTrigger::Timer,
            Err(RecvTimeoutError::Disconnected) => ReconcileTrigger::Timer,
        };
        let previous = read_status(&status);
        let next_status = match reconcile_once(&config, trigger, &previous) {
            Ok(status) => status,
            Err(err) => AgentStatus {
                last_reconcile_unix: now_unix(),
                last_event_unix: if matches!(trigger, ReconcileTrigger::PortEvent) {
                    now_unix()
                } else {
                    previous.last_event_unix
                },
                last_trigger: trigger_label(trigger).to_string(),
                managed_ports: previous.managed_ports,
                managed_networks: previous.managed_networks,
                bridge_ready: Path::new("/sys/class/net/br-int").exists(),
                last_error: err.to_string(),
            },
        };
        write_status(&status, next_status);
    });
}

fn start_nats_listener(config: AgentConfig, trigger_tx: Sender<ReconcileTrigger>) {
    thread::spawn(move || {
        let runtime = match tokio::runtime::Builder::new_current_thread()
            .enable_all()
            .build()
        {
            Ok(runtime) => runtime,
            Err(err) => {
                tracing::error!(error = %err, "failed to build NATS listener runtime");
                return;
            }
        };

        runtime.block_on(async move {
            loop {
                match async_nats::connect(&config.nats_url).await {
                    Ok(client) => {
                        tracing::info!(url = %config.nats_url, "network host agent connected to NATS");
                        match client.subscribe("orion.event.network.port.>".to_string()).await {
                            Ok(mut subscriber) => {
                                while let Some(message) = subscriber.next().await {
                                    let event =
                                        serde_json::from_slice::<ResourceEvent>(&message.payload).ok();
                                    if matches!(
                                        event.as_ref().map(|event| event.event_type.as_str()),
                                        Some("port.binding_updated")
                                    ) {
                                        continue;
                                    }
                                    tracing::debug!(
                                        subject = %message.subject,
                                        event_type = event.as_ref().map(|event| event.event_type.as_str()).unwrap_or("unknown"),
                                        resource_id = event.as_ref().map(|event| event.resource_id.as_str()).unwrap_or(""),
                                        "received port resource event"
                                    );
                                    if trigger_tx.send(ReconcileTrigger::PortEvent).is_err() {
                                        tracing::warn!(
                                            "reconcile worker channel closed; stopping NATS listener"
                                        );
                                        return;
                                    }
                                }
                                tracing::warn!(
                                    "port resource subscription ended; reconnecting to NATS"
                                );
                            }
                            Err(err) => {
                                tracing::error!(error = %err, "failed to subscribe to NATS port resource events");
                            }
                        }
                    }
                    Err(err) => {
                        tracing::error!(error = %err, url = %config.nats_url, "failed to connect to NATS");
                    }
                }
                tokio::time::sleep(Duration::from_secs(2)).await;
            }
        });
    });
}

fn reconcile_once(
    config: &AgentConfig,
    trigger: ReconcileTrigger,
    previous: &AgentStatus,
) -> anyhow::Result<AgentStatus> {
    let response = config
        .client
        .get(format!("{}/v1/ports", config.network_url))
        .query(&[("binding_host_id", &config.host_id)])
        .send()
        .with_context(|| format!("failed to fetch bound ports for host {}", config.host_id))?;
    if !response.status().is_success() {
        return Err(anyhow!(
            "network service returned status {} while listing bound ports",
            response.status()
        ));
    }

    let payload = response
        .json::<RemotePortsResponse>()
        .context("failed to decode bound ports response")?;
    let bridge_ready = match config
        .backend
        .ensure_integration_bridge(payload.ports.is_empty())
    {
        Ok(bridge_ready) => bridge_ready,
        Err(err) => {
            report_ports_binding_state(config, &payload.ports, "error", &err.to_string())?;
            return Err(err);
        }
    };
    let desired = build_desired_state(&config.host_id, &payload.ports, bridge_ready, now_unix());

    if let Err(err) = persist_desired_state(&config.state_dir, &desired) {
        report_ports_binding_state(config, &payload.ports, "error", &err.to_string())?;
        return Err(err);
    }
    report_ports_binding_state(config, &payload.ports, "ready", "")?;

    let mut last_error = String::new();
    if !bridge_ready && !desired.ports.is_empty() {
        last_error = "OVS integration bridge br-int is required for host-bound ports".to_string();
    }

    Ok(AgentStatus {
        last_reconcile_unix: now_unix(),
        last_event_unix: if matches!(trigger, ReconcileTrigger::PortEvent) {
            now_unix()
        } else {
            previous.last_event_unix
        },
        last_trigger: trigger_label(trigger).to_string(),
        managed_ports: desired.ports.len(),
        managed_networks: desired.networks.len(),
        bridge_ready,
        last_error,
    })
}

fn report_ports_binding_state(
    config: &AgentConfig,
    ports: &[RemotePort],
    binding_status: &str,
    binding_detail: &str,
) -> anyhow::Result<()> {
    for port in ports {
        let request = UpdatePortBindingRequest {
            binding_host_id: config.host_id.clone(),
            binding_status: binding_status.to_string(),
            binding_detail: binding_detail.to_string(),
        };
        let response = config
            .client
            .post(format!(
                "{}/v1/ports/{}/binding",
                config.network_url, port.id
            ))
            .json(&request)
            .send()
            .with_context(|| format!("failed to report binding state for port {}", port.id))?;
        if !response.status().is_success() {
            return Err(anyhow!(
                "network service returned status {} while updating binding state for port {}",
                response.status(),
                port.id
            ));
        }
    }
    Ok(())
}

fn build_desired_state(
    host_id: &str,
    ports: &[RemotePort],
    bridge_ready: bool,
    observed_at_unix: u64,
) -> DesiredState {
    let local_status = if bridge_ready { "ready" } else { "pending" }.to_string();
    let mut desired_ports = BTreeMap::new();
    let mut network_ports: BTreeMap<String, Vec<String>> = BTreeMap::new();

    for port in ports {
        desired_ports.insert(
            port.id.clone(),
            PortState {
                id: port.id.clone(),
                network_id: port.network_id.clone(),
                device_id: port.device_id.clone(),
                device_owner: port.device_owner.clone(),
                binding_host_id: port.binding_host_id.clone(),
                mac_address: port.mac_address.clone(),
                fixed_ips: port.fixed_ips.clone(),
                remote_status: port.status.clone(),
                vif_type: port.vif_type.clone(),
                vnic_type: port.vnic_type.clone(),
                local_bridge: "br-int".to_string(),
                local_status: local_status.clone(),
                observed_at_unix,
            },
        );
        network_ports
            .entry(port.network_id.clone())
            .or_default()
            .push(port.id.clone());
    }

    let mut desired_networks = BTreeMap::new();
    for (network_id, mut port_ids) in network_ports {
        port_ids.sort();
        desired_networks.insert(
            network_id.clone(),
            NetworkState {
                id: network_id,
                host_id: host_id.to_string(),
                port_ids,
                local_bridge: "br-int".to_string(),
                local_status: local_status.clone(),
                observed_at_unix,
            },
        );
    }

    DesiredState {
        ports: desired_ports,
        networks: desired_networks,
    }
}

fn persist_desired_state(state_dir: &Path, desired: &DesiredState) -> anyhow::Result<()> {
    ensure_state_layout(state_dir)?;
    sync_json_dir(&ports_dir(state_dir), &desired.ports)?;
    sync_json_dir(&networks_dir(state_dir), &desired.networks)?;
    Ok(())
}

fn sync_json_dir<T>(dir: &Path, items: &BTreeMap<String, T>) -> anyhow::Result<()>
where
    T: Serialize,
{
    let desired_names: BTreeSet<String> = items.keys().map(|id| state_file_name(id)).collect();
    for entry in fs::read_dir(dir).with_context(|| format!("failed to read {}", dir.display()))? {
        let entry = entry.with_context(|| format!("failed to read entry in {}", dir.display()))?;
        let path = entry.path();
        if !path.is_file() {
            continue;
        }
        let file_name = match path.file_name().and_then(|value| value.to_str()) {
            Some(value) => value.to_string(),
            None => continue,
        };
        if !desired_names.contains(&file_name) {
            fs::remove_file(&path)
                .with_context(|| format!("failed to remove stale state {}", path.display()))?;
        }
    }

    for (id, item) in items {
        write_json_file(&dir.join(state_file_name(id)), item)?;
    }
    Ok(())
}

fn ensure_state_layout(state_dir: &Path) -> anyhow::Result<()> {
    fs::create_dir_all(ports_dir(state_dir))
        .with_context(|| format!("failed to create {}", ports_dir(state_dir).display()))?;
    fs::create_dir_all(networks_dir(state_dir))
        .with_context(|| format!("failed to create {}", networks_dir(state_dir).display()))?;
    Ok(())
}

fn ports_dir(state_dir: &Path) -> PathBuf {
    state_dir.join("ports")
}

fn networks_dir(state_dir: &Path) -> PathBuf {
    state_dir.join("networks")
}

fn state_file_name(id: &str) -> String {
    format!("{id}.json")
}

fn load_collection<T>(dir: &Path) -> anyhow::Result<Vec<T>>
where
    T: DeserializeOwned,
{
    let mut items = Vec::new();
    for entry in fs::read_dir(dir).with_context(|| format!("failed to read {}", dir.display()))? {
        let entry = entry.with_context(|| format!("failed to read entry in {}", dir.display()))?;
        let path = entry.path();
        if path.extension().and_then(|value| value.to_str()) != Some("json") {
            continue;
        }
        items.push(read_json_file(&path)?);
    }
    Ok(items)
}

fn read_json_file<T>(path: &Path) -> anyhow::Result<T>
where
    T: DeserializeOwned,
{
    let body = fs::read(path).with_context(|| format!("failed to read {}", path.display()))?;
    serde_json::from_slice(&body).with_context(|| format!("failed to decode {}", path.display()))
}

fn write_json_file<T>(path: &Path, item: &T) -> anyhow::Result<()>
where
    T: Serialize,
{
    let body = serde_json::to_vec_pretty(item)
        .with_context(|| format!("failed to encode {}", path.display()))?;
    fs::write(path, body).with_context(|| format!("failed to write {}", path.display()))
}

fn read_status(status: &Arc<RwLock<AgentStatus>>) -> AgentStatus {
    status.read().map(|value| value.clone()).unwrap_or_default()
}

fn write_status(status: &Arc<RwLock<AgentStatus>>, next: AgentStatus) {
    if let Ok(mut current) = status.write() {
        *current = next;
    }
}

fn trigger_label(trigger: ReconcileTrigger) -> &'static str {
    match trigger {
        ReconcileTrigger::Startup => "startup",
        ReconcileTrigger::Timer => "timer",
        ReconcileTrigger::PortEvent => "port_event",
    }
}

fn resolve_host_id() -> anyhow::Result<String> {
    if let Some(value) = trim_non_empty(env::var("ORION_NETWORK_HOST_ID").ok().as_deref()) {
        return Ok(value.to_string());
    }
    if let Some(value) = trim_non_empty(env::var("HOSTNAME").ok().as_deref()) {
        return Ok(value.to_string());
    }
    if let Ok(value) = fs::read_to_string("/etc/hostname") {
        if let Some(trimmed) = trim_non_empty(Some(&value)) {
            return Ok(trimmed.to_string());
        }
    }
    if let Ok(output) = Command::new("hostname").output() {
        if output.status.success() {
            let stdout = String::from_utf8_lossy(&output.stdout);
            if let Some(trimmed) = trim_non_empty(Some(&stdout)) {
                return Ok(trimmed.to_string());
            }
        }
    }
    Err(anyhow!(
        "failed to discover host ID automatically; set ORION_NETWORK_HOST_ID"
    ))
}

fn trim_non_empty(value: Option<&str>) -> Option<&str> {
    value.and_then(|item| {
        let trimmed = item.trim();
        if trimmed.is_empty() {
            None
        } else {
            Some(trimmed)
        }
    })
}

fn now_unix() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|value| value.as_secs())
        .unwrap_or_default()
}

fn internal_error(err: anyhow::Error) -> (StatusCode, String) {
    tracing::error!(error = %err, "network host agent request failed");
    (StatusCode::INTERNAL_SERVER_ERROR, err.to_string())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn build_desired_state_groups_ports_by_network() {
        let desired = build_desired_state(
            "host-a",
            &[
                RemotePort {
                    id: "port_1".to_string(),
                    network_id: "net_a".to_string(),
                    device_id: "srv_1".to_string(),
                    device_owner: "compute:orion".to_string(),
                    binding_host_id: "host-a".to_string(),
                    mac_address: "fa:16:3e:00:00:01".to_string(),
                    fixed_ips: vec![FixedIP {
                        subnet_id: "subnet_a".to_string(),
                        ip_address: "10.0.0.10".to_string(),
                    }],
                    status: "down".to_string(),
                    vif_type: "ovs".to_string(),
                    vnic_type: "normal".to_string(),
                },
                RemotePort {
                    id: "port_2".to_string(),
                    network_id: "net_a".to_string(),
                    device_id: "srv_2".to_string(),
                    device_owner: "compute:orion".to_string(),
                    binding_host_id: "host-a".to_string(),
                    mac_address: "fa:16:3e:00:00:02".to_string(),
                    fixed_ips: vec![FixedIP {
                        subnet_id: "subnet_a".to_string(),
                        ip_address: "10.0.0.11".to_string(),
                    }],
                    status: "down".to_string(),
                    vif_type: "ovs".to_string(),
                    vnic_type: "normal".to_string(),
                },
            ],
            true,
            42,
        );

        assert_eq!(desired.ports.len(), 2);
        assert_eq!(desired.networks.len(), 1);
        let network = desired.networks.get("net_a").expect("network marker");
        assert_eq!(
            network.port_ids,
            vec!["port_1".to_string(), "port_2".to_string()]
        );
        assert_eq!(network.local_status, "ready");
    }

    #[test]
    fn persist_desired_state_removes_stale_markers() {
        let temp_dir = unique_temp_dir("network-host-agent");
        ensure_state_layout(&temp_dir).expect("state layout");

        write_json_file(
            &ports_dir(&temp_dir).join("stale_port.json"),
            &PortState {
                id: "stale_port".to_string(),
                network_id: "net_stale".to_string(),
                device_id: String::new(),
                device_owner: String::new(),
                binding_host_id: "host-a".to_string(),
                mac_address: String::new(),
                fixed_ips: Vec::new(),
                remote_status: "down".to_string(),
                vif_type: "ovs".to_string(),
                vnic_type: "normal".to_string(),
                local_bridge: "br-int".to_string(),
                local_status: "ready".to_string(),
                observed_at_unix: 1,
            },
        )
        .expect("stale marker");

        let desired = build_desired_state(
            "host-a",
            &[RemotePort {
                id: "port_1".to_string(),
                network_id: "net_a".to_string(),
                device_id: "srv_1".to_string(),
                device_owner: "compute:orion".to_string(),
                binding_host_id: "host-a".to_string(),
                mac_address: "fa:16:3e:00:00:01".to_string(),
                fixed_ips: Vec::new(),
                status: "down".to_string(),
                vif_type: "ovs".to_string(),
                vnic_type: "normal".to_string(),
            }],
            false,
            7,
        );
        persist_desired_state(&temp_dir, &desired).expect("persist desired state");

        assert!(!ports_dir(&temp_dir).join("stale_port.json").exists());
        assert!(ports_dir(&temp_dir).join("port_1.json").exists());
        assert!(networks_dir(&temp_dir).join("net_a.json").exists());

        let _ = fs::remove_dir_all(temp_dir);
    }

    fn unique_temp_dir(label: &str) -> PathBuf {
        let path = env::temp_dir().join(format!("{label}-{}", now_unix()));
        let _ = fs::remove_dir_all(&path);
        path
    }
}

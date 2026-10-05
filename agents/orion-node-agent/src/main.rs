use std::net::SocketAddr;
use std::sync::Arc;
use std::time::Duration;

use anyhow::{Context, Result};
use axum::{extract::State, routing::get, Router};
use tonic::Request;
use tower_http::trace::TraceLayer;
use tracing::info;
use uuid::Uuid;

mod common;
mod domain;
mod generated;
mod grpc;
mod http_types;
mod nats;
mod reconcile;
mod state;
mod sync;

use crate::grpc::NodeAgentGrpcService;
use crate::reconcile::Reconciler;
use crate::state::AgentState;

#[tokio::main]
async fn main() -> Result<()> {
    orion_host_kit::init_logging("orion-node-agent");

    let storage_dir = std::env::var("ORION_NODE_AGENT_STORAGE_DIR")
        .map(std::path::PathBuf::from)
        .unwrap_or_else(|_| std::path::PathBuf::from("/var/tmp/orion-node-agent"));
    std::fs::create_dir_all(&storage_dir)
        .with_context(|| format!("failed to create {}", storage_dir.display()))?;

    let grpc_port: u16 = std::env::var("ORION_NODE_AGENT_GRPC_PORT")
        .unwrap_or_else(|_| "50051".to_string())
        .parse()
        .unwrap_or(50051);

    let nats_url =
        std::env::var("ORION_NATS_URL").unwrap_or_else(|_| "nats://localhost:4222".to_string());

    let agent_state = AgentState::new(storage_dir.clone());

    let grpc_addr: SocketAddr = format!("0.0.0.0:{grpc_port}").parse().unwrap();

    info!(
        grpc_addr = %grpc_addr,
        nats_url = %nats_url,
        "orion-node-agent starting"
    );

    let nats_subscriber = nats::NatsSubscriber::new(&nats_url, agent_state.clone()).await?;

    spawn_placement_heartbeat();

    let grpc_service = NodeAgentGrpcService::new(agent_state.clone());
    let metrics = grpc_service.metrics();
    let svc = grpc_service.into_service();
    let mut server_builder = tonic::transport::Server::builder().layer(TraceLayer::new_for_grpc());
    if let Some(tls) = orion_host_kit::tls::server_tls_config_from_env()? {
        server_builder = server_builder.tls_config(tls)?;
    }
    let server = server_builder
        .add_service(svc)
        .serve_with_shutdown(grpc_addr, async {
            let _ = tokio::signal::ctrl_c().await;
            tracing::info!("node agent shutdown requested");
        });

    let http_state = agent_state.clone();
    tokio::spawn(async move {
        if let Err(e) = nats_subscriber.subscribe_to_compute_commands().await {
            tracing::error!(error = %e, "NATS subscriber error");
        }
    });

    let reconciler = Reconciler::new(agent_state.clone());
    tokio::spawn(async move {
        reconciler.run().await;
    });

    let metrics_addr: SocketAddr = format!(
        "0.0.0.0:{}",
        std::env::var("ORION_NODE_AGENT_HTTP_PORT").unwrap_or_else(|_| "8084".to_string())
    )
    .parse()
    .context("invalid node agent HTTP port")?;
    tokio::spawn(serve_http(metrics_addr, http_state, metrics));

    info!("gRPC server listening on {}", grpc_addr);
    server.await.context("gRPC server error")?;

    Ok(())
}

fn spawn_placement_heartbeat() {
    let placement_addr = std::env::var("ORION_PLACEMENT_GRPC_ADDR")
        .unwrap_or_else(|_| "127.0.0.1:50053".to_string());
    let host_id =
        std::env::var("ORION_NODE_AGENT_HOST_ID").unwrap_or_else(|_| "host_local".to_string());
    let node_agent_url = std::env::var("ORION_NODE_AGENT_GRPC_ADDR")
        .unwrap_or_else(|_| "127.0.0.1:50051".to_string());
    let volume_agent_url = std::env::var("ORION_VOLUME_HOST_AGENT_GRPC_ADDR")
        .unwrap_or_else(|_| "127.0.0.1:50055".to_string());
    let network_agent_url = std::env::var("ORION_NETWORK_HOST_AGENT_GRPC_ADDR")
        .unwrap_or_else(|_| "127.0.0.1:50054".to_string());
    let endpoint = format!("http://{placement_addr}");

    tokio::spawn(async move {
        loop {
            match crate::generated::placement::placement_service_client::PlacementServiceClient::connect(endpoint.clone()).await {
                Ok(mut client) => {
                    let request = crate::generated::placement::RegisterHostRequest {
                        host_id: host_id.clone(),
                        cell_id: std::env::var("ORION_NODE_AGENT_CELL_ID").unwrap_or_else(|_| "cell_local".to_string()),
                        node_agent_url: node_agent_url.clone(),
                        volume_host_agent_url: volume_agent_url.clone(),
                        network_host_agent_url: network_agent_url.clone(),
                        vcpus_total: 64,
                        memory_mb_total: 262144,
                        disk_gb_total: 4096,
                        traits: vec!["KVM".to_string(), "qemu".to_string()],
                        availability_zone: std::env::var("ORION_NODE_AGENT_AVAILABILITY_ZONE").unwrap_or_default(),
                        host_aggregate: std::env::var("ORION_NODE_AGENT_HOST_AGGREGATE").unwrap_or_default(),
                    };
                    let mut request = Request::new(crate::generated::placement::HeartbeatRequest {
                        host: Some(request),
                    });
                    if let Ok(value) = format!("node-heartbeat-{}", Uuid::new_v4()).parse() {
                        request.metadata_mut().insert("x-request-id", value);
                    }
                    match client.heartbeat(request).await {
                        Ok(response) => {
                            if response.into_inner().accepted {
                                tracing::debug!(host_id = %host_id, "placement heartbeat accepted");
                            } else {
                                tracing::warn!(host_id = %host_id, "placement heartbeat rejected");
                            }
                        }
                        Err(error) => tracing::warn!(error = %error, "placement heartbeat failed"),
                    }
                }
                Err(error) => tracing::warn!(error = %error, "placement connection failed"),
            }
            tokio::time::sleep(Duration::from_secs(15)).await;
        }
    });
}

async fn serve_http(addr: SocketAddr, state: AgentState, metrics: Arc<grpc::GrpcMetrics>) {
    let app = Router::new()
        .route("/healthz", get(|| async { "ok" }))
        .route("/readyz", {
            let state = state.clone();
            get(move || async move {
                let libvirt_ok = state.is_libvirt_healthy();
                let agent_ready = state.is_ready();
                let status = if libvirt_ok && agent_ready {
                    "ready"
                } else if !libvirt_ok {
                    "libvirt_down"
                } else {
                    "initializing"
                };
                let code = if agent_ready && libvirt_ok {
                    axum::http::StatusCode::OK
                } else {
                    axum::http::StatusCode::SERVICE_UNAVAILABLE
                };
                (code, status)
            })
        })
        .route(
            "/metrics",
            get(
                |State(metrics): State<Arc<grpc::GrpcMetrics>>| async move { metrics.prometheus() },
            ),
        )
        .with_state(metrics);
    let listener = match tokio::net::TcpListener::bind(addr).await {
        Ok(listener) => listener,
        Err(err) => {
            tracing::error!(error = %err, "node agent HTTP server bind failed");
            return;
        }
    };
    if let Err(err) = axum::serve(listener, app).await {
        tracing::error!(error = %err, "node agent HTTP server failed");
    }
}

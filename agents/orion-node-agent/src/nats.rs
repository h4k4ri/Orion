use std::collections::HashMap;

use anyhow::{anyhow, Result};
use chrono::Utc;
use futures_util::StreamExt;
use serde::{Deserialize, Serialize};

use crate::domain::DesiredInstance;
use crate::state::AgentState;
use async_nats::jetstream::message::AckKind;

#[derive(Debug, Clone, Deserialize)]
pub struct ComputeCommand {
    pub message_id: String,
    pub operation_id: String,
    #[serde(default)]
    pub request_id: String,
    pub action: String,
    #[serde(rename = "payload")]
    pub payload: serde_json::Value,
}

#[derive(Debug, Clone, Deserialize)]
pub struct DesiredStateUpdate {
    #[serde(default)]
    pub host_id: String,
    #[serde(default)]
    pub instances: HashMap<String, DesiredInstance>,
    #[serde(default)]
    pub deleted_instances: Vec<String>,
    pub generation: i64,
}

#[derive(Debug, Clone, Deserialize)]
pub struct BuildInstancePayload {
    pub instance_id: String,
    pub host_id: String,
    pub name: String,
    pub vcpus: u32,
    pub memory_mb: u64,
    pub disk_gb: u64,
    pub image: Option<ImagePayload>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct ImagePayload {
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

#[derive(Debug, Clone, Deserialize)]
pub struct DestroyInstancePayload {
    pub instance_id: String,
}

#[derive(Debug, Clone, Deserialize)]
pub struct AttachVolumePayload {
    pub instance_id: String,
    pub volume_id: String,
    pub device_path: Option<String>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct DetachVolumePayload {
    pub instance_id: String,
    pub volume_id: String,
}

#[derive(Debug, Serialize)]
pub struct TaskEvent {
    pub event_type: String,
    pub operation_id: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub observed_state: Option<String>,
    pub task: Task,
}

#[derive(Debug, Serialize)]
pub struct Task {
    pub id: String,
    pub kind: String,
    pub scope: String,
    pub target_ref: String,
    pub status: String,
    #[serde(rename = "requested_by")]
    pub requested_by: String,
    #[serde(rename = "request_id")]
    pub request_id: String,
    #[serde(rename = "cell_id")]
    pub cell_id: Option<String>,
    #[serde(rename = "host_id")]
    pub host_id: Option<String>,
    #[serde(rename = "error_code")]
    pub error_code: Option<String>,
    #[serde(rename = "error_message")]
    pub error_message: Option<String>,
    #[serde(rename = "created_at")]
    pub created_at: String,
    #[serde(rename = "updated_at")]
    pub updated_at: String,
}

pub struct NatsSubscriber {
    client: async_nats::Client,
    state: AgentState,
}

impl NatsSubscriber {
    pub async fn new(nats_url: &str, state: AgentState) -> Result<Self> {
        let client = async_nats::connect(nats_url).await?;
        Ok(Self { client, state })
    }

    pub async fn subscribe_to_compute_commands(&self) -> Result<()> {
        let jetstream = async_nats::jetstream::new(self.client.clone());

        tracing::info!("subscribing to compute commands via JetStream");

        let stream = jetstream
            .get_or_create_stream(async_nats::jetstream::stream::Config {
                name: "ORION_COMMANDS".to_string(),
                subjects: vec!["orion.command.compute.>".to_string()],
                ..Default::default()
            })
            .await?;

        jetstream
            .get_or_create_stream(async_nats::jetstream::stream::Config {
                name: "ORION_EVENTS".to_string(),
                subjects: vec!["orion.event.>".to_string()],
                ..Default::default()
            })
            .await?;

        let desired_stream = jetstream
            .get_or_create_stream(async_nats::jetstream::stream::Config {
                name: "ORION_DESIRED_STATE".to_string(),
                subjects: vec![
                    "orion.desired.compute.>".to_string(),
                    "orion.desired.volume.>".to_string(),
                ],
                ..Default::default()
            })
            .await?;

        let desired_state_sub = desired_stream
            .get_or_create_consumer(
                "node-agent-desired-state",
                async_nats::jetstream::consumer::pull::Config {
                    durable_name: Some("node-agent-desired-state".to_string()),
                    description: Some("Node agent desired state consumer".to_string()),
                    filter_subject: "orion.desired.compute.>".to_string(),
                    ack_policy: async_nats::jetstream::consumer::AckPolicy::Explicit,
                    ..Default::default()
                },
            )
            .await?;

        let desired_state_state = self.state.clone();
        tokio::spawn(async move {
            let mut messages = desired_state_sub.messages().await.unwrap();
            while let Some(msg) = messages.next().await {
                match msg {
                    Ok(msg) => {
                        let target_host = msg.message.subject.split('.').nth(3).unwrap_or_default();
                        if target_host != desired_state_state.host_id.as_str() && target_host != "*"
                        {
                            let _ = msg.ack().await;
                            continue;
                        }
                        if let Ok(state) =
                            serde_json::from_slice::<DesiredStateUpdate>(&msg.message.payload)
                        {
                            tracing::info!(
                                generation = state.generation,
                                instances = state.instances.len(),
                                "received desired state update"
                            );
                            desired_state_state.apply_desired_update(
                                state.instances,
                                &state.deleted_instances,
                                state.generation,
                            );
                            desired_state_state.set_ready(true);
                        }
                        let _ = msg.ack().await;
                    }
                    Err(e) => tracing::error!(error = %e, "desired state message error"),
                }
            }
        });

        loop {
            match self.subscribe_once(&stream).await {
                Ok(()) => tracing::info!("subscription ended, reconnecting..."),
                Err(e) => {
                    tracing::error!(error = %e, "subscription error, reconnecting...");
                    tokio::time::sleep(tokio::time::Duration::from_secs(1)).await;
                }
            }
        }
    }

    async fn subscribe_once(&self, stream: &async_nats::jetstream::stream::Stream) -> Result<()> {
        let consumer = stream
            .get_or_create_consumer(
                "node-agent-commands",
                async_nats::jetstream::consumer::pull::Config {
                    durable_name: Some("node-agent-commands".to_string()),
                    description: Some("Node agent command consumer".to_string()),
                    filter_subject: "orion.command.compute.>".to_string(),
                    ack_policy: async_nats::jetstream::consumer::AckPolicy::Explicit,
                    ..Default::default()
                },
            )
            .await?;

        let mut messages = consumer.messages().await?;

        while let Some(message) = messages.next().await {
            match message {
                Ok(msg) => {
                    let subject = msg.subject.to_string();
                    tracing::debug!(subject = %subject, "received compute command");

                    let payload = msg.message.payload.clone();
                    let cmd: ComputeCommand = match serde_json::from_slice(&payload) {
                        Ok(c) => c,
                        Err(e) => {
                            tracing::error!(error = %e, "failed to parse compute command");
                            let (_, acker) = msg.split();
                            let _ = acker.ack_with(AckKind::Nak(None)).await;
                            continue;
                        }
                    };

                    let operation_id = cmd.operation_id.clone();
                    let action = cmd.action.clone();
                    if !self.state.claim_command(&cmd.message_id)? {
                        let (_, acker) = msg.split();
                        acker.ack().await.map_err(|err| anyhow!(err.to_string()))?;
                        continue;
                    }

                    match self.handle_message_payload(&cmd).await {
                        Ok(observed_state) => {
                            self.publish_task_event(
                                &operation_id,
                                &cmd.request_id,
                                &action,
                                "succeeded",
                                None,
                                None,
                                observed_state.as_deref(),
                            )
                            .await?;
                            let (_, acker) = msg.split();
                            if let Err(e) = acker.ack().await {
                                tracing::error!(error = %e, "failed to ack message");
                            }
                        }
                        Err(e) => {
                            tracing::error!(error = %e, "failed to handle compute command");
                            self.publish_task_event(
                                &operation_id,
                                &cmd.request_id,
                                &action,
                                "failed",
                                Some("EXECUTION_FAILED"),
                                Some(e.to_string()),
                                None,
                            )
                            .await?;
                            let (_, acker) = msg.split();
                            if let Err(e) = acker.ack_with(AckKind::Nak(None)).await {
                                tracing::error!(error = %e, "failed to nak message");
                            }
                        }
                    }
                }
                Err(e) => {
                    tracing::error!(error = %e, "message error");
                }
            }
        }

        Ok(())
    }

    async fn handle_message_payload(&self, cmd: &ComputeCommand) -> Result<Option<String>> {
        tracing::info!(
            action = %cmd.action,
            message_id = %cmd.message_id,
            operation_id = %cmd.operation_id,
            "processing compute command"
        );

        match cmd.action.as_str() {
            "build_instance" => self
                .handle_build_instance(cmd.payload.clone())
                .await
                .map(Some),
            "destroy_instance" => self
                .handle_destroy_instance(cmd.payload.clone())
                .await
                .map(|_| None),
            "attach_volume" => self
                .handle_attach_volume(cmd.payload.clone())
                .await
                .map(|_| None),
            "detach_volume" => self
                .handle_detach_volume(cmd.payload.clone())
                .await
                .map(|_| None),
            "observe_instance" => self
                .handle_observe_instance(cmd.payload.clone())
                .await
                .map(|_| None),
            _ => {
                tracing::warn!(action = %cmd.action, "unknown compute command action");
                Ok(None)
            }
        }
    }

    async fn publish_task_event(
        &self,
        operation_id: &str,
        request_id: &str,
        action: &str,
        status: &str,
        error_code: Option<&str>,
        error_message: Option<String>,
        observed_state: Option<&str>,
    ) -> Result<()> {
        let now = Utc::now().to_rfc3339();

        let task = Task {
            id: format!("tsk_{}", &operation_id[..8.min(operation_id.len())]),
            kind: format!("server.{}", action.replace("_", ".")),
            scope: "project".to_string(),
            target_ref: operation_id.to_string(),
            status: status.to_string(),
            requested_by: "node-agent".to_string(),
            request_id: if request_id.is_empty() {
                operation_id.to_string()
            } else {
                request_id.to_string()
            },
            cell_id: None,
            host_id: None,
            error_code: error_code.map(String::from),
            error_message,
            created_at: now.clone(),
            updated_at: now,
        };

        let event = TaskEvent {
            event_type: format!("task.{}", status),
            operation_id: operation_id.to_string(),
            observed_state: observed_state.map(String::from),
            task,
        };

        let payload = serde_json::to_vec(&event)?;

        let jetstream = async_nats::jetstream::new(self.client.clone());
        let subject = if status == "failed" {
            "orion.event.compute.instance.failed.v1"
        } else {
            "orion.event.compute.instance.created.v1"
        };
        let ack = jetstream.publish(subject, payload.into()).await?;
        ack.await?;

        tracing::info!(
            operation_id = %operation_id,
            status = %status,
            "published task event"
        );

        Ok(())
    }

    async fn handle_build_instance(&self, payload: serde_json::Value) -> Result<String> {
        let req: BuildInstancePayload = serde_json::from_value(payload)?;
        tracing::info!(
            instance_id = %req.instance_id,
            host_id = %req.host_id,
            name = %req.name,
            "handle_build_instance"
        );

        let image = req.image.unwrap_or(ImagePayload {
            id: String::new(),
            source_path: String::new(),
            source_url: String::new(),
            checksum_sha256: String::new(),
            size_bytes: 0,
        });

        let http_req = crate::http_types::BuildServerRequest {
            server_id: req.instance_id.clone(),
            host_id: req.host_id.clone(),
            name: req.name.clone(),
            image: crate::http_types::ImageSource {
                id: image.id,
                source_path: image.source_path,
                source_url: image.source_url,
                checksum_sha256: image.checksum_sha256,
                size_bytes: image.size_bytes,
            },
            ports: vec![],
            vcpus: req.vcpus,
            memory_mb: req.memory_mb as u32,
            disk_gb: req.disk_gb as u32,
        };

        let storage_dir = self.state.storage_dir.clone();

        let result = tokio::task::spawn_blocking(move || {
            crate::sync::build_server_sync(&storage_dir, http_req)
        })
        .await??;

        Ok(result.status)
    }

    async fn handle_destroy_instance(&self, payload: serde_json::Value) -> Result<()> {
        let req: DestroyInstancePayload = serde_json::from_value(payload)?;
        tracing::info!(instance_id = %req.instance_id, "handle_destroy_instance");

        let storage_dir = self.state.storage_dir.clone();

        tokio::task::spawn_blocking(move || {
            crate::sync::delete_server_sync(&storage_dir, &req.instance_id)
        })
        .await??;

        Ok(())
    }

    async fn handle_attach_volume(&self, payload: serde_json::Value) -> Result<()> {
        let req: AttachVolumePayload = serde_json::from_value(payload)?;
        tracing::info!(
            instance_id = %req.instance_id,
            volume_id = %req.volume_id,
            "handle_attach_volume"
        );

        let vol_req = crate::http_types::AttachVolumeRequest {
            volume_id: req.volume_id.clone(),
            device_path: req.device_path.unwrap_or_default(),
        };

        let storage_dir = self.state.storage_dir.clone();

        tokio::task::spawn_blocking(move || {
            crate::sync::attach_volume_sync(&req.instance_id, vol_req)
        })
        .await??;

        Ok(())
    }

    async fn handle_detach_volume(&self, payload: serde_json::Value) -> Result<()> {
        let req: DetachVolumePayload = serde_json::from_value(payload)?;
        tracing::info!(
            instance_id = %req.instance_id,
            volume_id = %req.volume_id,
            "handle_detach_volume"
        );

        let vol_req = crate::http_types::DetachVolumeRequest {
            volume_id: req.volume_id.clone(),
            device_path: String::new(),
        };

        let storage_dir = self.state.storage_dir.clone();

        tokio::task::spawn_blocking(move || {
            crate::sync::detach_volume_sync(&req.instance_id, vol_req)
        })
        .await??;

        Ok(())
    }

    async fn handle_observe_instance(&self, payload: serde_json::Value) -> Result<()> {
        let req: DestroyInstancePayload = serde_json::from_value(payload)?;
        tracing::info!(instance_id = %req.instance_id, "handle_observe_instance");

        tokio::task::spawn_blocking(move || crate::sync::get_server_sync(&req.instance_id))
            .await??;

        Ok(())
    }
}

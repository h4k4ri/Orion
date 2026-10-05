use crate::proto::plugin::v1::plugin_executor_server::{PluginExecutor, PluginExecutorServer};
use crate::proto::plugin::v1::{self as pb, OrionError as ProtoError};
use crate::{Plugin, RelationshipRequest, ResourceRequest};
use prost_types::{value::Kind, ListValue, Struct, Value};
use serde_json::{Map, Number, Value as JsonValue};
use std::collections::{BTreeMap, HashMap};
use std::net::SocketAddr;
use std::sync::{
    atomic::{AtomicBool, Ordering},
    Arc,
};
use tokio::sync::Mutex;
use tonic::{Request, Response, Status};

#[derive(Clone)]
pub struct Server {
    plugin: Plugin,
    address: String,
}

impl Server {
    pub fn new(plugin: Plugin, address: impl Into<String>) -> Self {
        Self {
            plugin,
            address: address.into(),
        }
    }

    pub async fn serve(self) -> anyhow::Result<()> {
        let address = normalize_address(&self.address)?.parse::<SocketAddr>()?;
        let service = ExecutorService::new(self.plugin.clone());
        println!("Starting Orion plugin server on {}", address);
        println!(
            "Plugin: {} v{}",
            self.plugin.get_config().name,
            self.plugin.get_config().version
        );

        tonic::transport::Server::builder()
            .add_service(PluginExecutorServer::new(service))
            .serve(address)
            .await?;
        Ok(())
    }
}

pub async fn run(plugin: Plugin, address: impl Into<String>) -> anyhow::Result<()> {
    Server::new(plugin, address).serve().await
}

fn normalize_address(address: &str) -> anyhow::Result<String> {
    if address.starts_with(':') {
        return Ok(format!("[::]{}", address));
    }
    if address.is_empty() {
        anyhow::bail!("server address cannot be empty");
    }
    Ok(address.to_string())
}

#[derive(Clone)]
struct ExecutorService {
    plugin: Plugin,
    operations: Arc<Mutex<HashMap<String, pb::OperationStatus>>>,
    cancellations: Arc<Mutex<HashMap<String, Arc<AtomicBool>>>>,
}

impl ExecutorService {
    fn new(plugin: Plugin) -> Self {
        Self {
            plugin,
            operations: Arc::new(Mutex::new(HashMap::new())),
            cancellations: Arc::new(Mutex::new(HashMap::new())),
        }
    }

    async fn invoke_inner(&self, request: pb::InvokeRequest) -> pb::InvokeResponse {
        let payload = struct_to_json_bytes(request.payload.as_ref());

        if let Some(handler) = self.plugin.get_resource_handler(&request.resource) {
            let Some(operation) = handler.operations.get(&request.operation) else {
                return error_response(
                    &request.request_id,
                    "NOT_SUPPORTED",
                    "operation is not registered",
                );
            };
            let input = ResourceRequest {
                request_id: request.request_id.clone(),
                provider_id: request.provider_id.clone(),
                operation: request.operation.clone(),
                payload,
            };
            return match operation(input) {
                Ok(result) => resource_response(&request.request_id, result),
                Err(error) => error_response(&request.request_id, &error.code, &error.message),
            };
        }

        let (role, operation_name) = split_relationship_operation(&request.operation);
        if let Some(handler) = self.plugin.get_relationship_handler(&request.resource) {
            if role.is_empty() {
                return error_response(
                    &request.request_id,
                    "NOT_SUPPORTED",
                    "relationship operation must include a role",
                );
            }
            if handler.role != role {
                return error_response(
                    &request.request_id,
                    "ROLE_MISMATCH",
                    "relationship role is not implemented by this plugin",
                );
            }
            let Some(operation) = handler.operations.get(operation_name) else {
                return error_response(
                    &request.request_id,
                    "NOT_SUPPORTED",
                    "relationship operation is not registered",
                );
            };
            let input = RelationshipRequest {
                request_id: request.request_id.clone(),
                provider_id: request.provider_id.clone(),
                source_id: request
                    .metadata
                    .get("source_resource_id")
                    .cloned()
                    .unwrap_or_default(),
                target_id: request
                    .metadata
                    .get("target_resource_id")
                    .cloned()
                    .unwrap_or_default(),
                operation: operation_name.to_string(),
                payload,
            };
            return match operation(input) {
                Ok(result) => relationship_response(&request.request_id, result),
                Err(error) => error_response(&request.request_id, &error.code, &error.message),
            };
        }

        error_response(&request.request_id, "NOT_FOUND", "plugin handler not found")
    }
}

#[tonic::async_trait]
impl PluginExecutor for ExecutorService {
    async fn invoke(
        &self,
        request: Request<pb::InvokeRequest>,
    ) -> Result<Response<pb::InvokeResponse>, Status> {
        Ok(Response::new(self.invoke_inner(request.into_inner()).await))
    }

    async fn invoke_async(
        &self,
        request: Request<pb::InvokeRequest>,
    ) -> Result<Response<pb::OperationStatus>, Status> {
        let request = request.into_inner();
        let operation_id = if request.idempotency_key.is_empty() {
            if request.request_id.is_empty() {
                format!("operation-{}", uuid_like_id())
            } else {
                request.request_id.clone()
            }
        } else {
            request.idempotency_key.clone()
        };

        let mut operations = self.operations.lock().await;
        if let Some(existing) = operations.get(&operation_id) {
            return Ok(Response::new(existing.clone()));
        }
        let status = pb::OperationStatus {
            operation_id: operation_id.clone(),
            state: pb::OperationState::Running as i32,
            resource: request.resource.clone(),
            resource_version: request.resource_version.clone(),
            operation_name: request.operation.clone(),
            started_at: Some(now()),
            updated_at: Some(now()),
            ..Default::default()
        };
        operations.insert(operation_id.clone(), status.clone());
        drop(operations);

        let cancellation = Arc::new(AtomicBool::new(false));
        self.cancellations
            .lock()
            .await
            .insert(operation_id.clone(), cancellation.clone());
        let service = self.clone();
        tokio::spawn(async move {
            let response = service.invoke_inner(request).await;
            let mut operations = service.operations.lock().await;
            if let Some(status) = operations.get_mut(&operation_id) {
                status.updated_at = Some(now());
                status.completed_at = status.updated_at.clone();
                status.progress_percent = 100;
                if cancellation.load(Ordering::Relaxed)
                    || status.state == pb::OperationState::Cancelling as i32
                {
                    status.state = pb::OperationState::Cancelled as i32;
                    status.error = Some(proto_error("CANCELLED", "operation cancelled"));
                } else if response.success {
                    status.state = pb::OperationState::Succeeded as i32;
                    status.result = response.result;
                } else {
                    status.state = pb::OperationState::Failed as i32;
                    status.error = response.error;
                }
            }
            service.cancellations.lock().await.remove(&operation_id);
        });

        Ok(Response::new(status))
    }

    async fn get_operation_status(
        &self,
        request: Request<pb::GetOperationStatusRequest>,
    ) -> Result<Response<pb::OperationStatus>, Status> {
        let id = request.into_inner().operation_id;
        let operations = self.operations.lock().await;
        Ok(Response::new(operations.get(&id).cloned().unwrap_or_else(
            || pb::OperationStatus {
                operation_id: id,
                error: Some(proto_error("NOT_FOUND", "operation not found")),
                ..Default::default()
            },
        )))
    }

    async fn cancel_operation(
        &self,
        request: Request<pb::CancelOperationRequest>,
    ) -> Result<Response<pb::OperationStatus>, Status> {
        let id = request.into_inner().operation_id;
        let mut operations = self.operations.lock().await;
        let Some(status) = operations.get_mut(&id) else {
            return Ok(Response::new(pb::OperationStatus {
                operation_id: id,
                error: Some(proto_error("NOT_FOUND", "operation not found")),
                ..Default::default()
            }));
        };
        if status.state == pb::OperationState::Pending as i32
            || status.state == pb::OperationState::Running as i32
        {
            status.state = pb::OperationState::Cancelling as i32;
            status.updated_at = Some(now());
            if let Some(token) = self.cancellations.lock().await.get(&id) {
                token.store(true, Ordering::Relaxed);
            }
        }
        Ok(Response::new(status.clone()))
    }

    async fn reconcile(
        &self,
        _request: Request<pb::ReconcileRequest>,
    ) -> Result<Response<pb::ReconcileResponse>, Status> {
        Ok(Response::new(pb::ReconcileResponse {
            success: false,
            error: Some(proto_error(
                "NOT_SUPPORTED",
                "reconcile is owned by Orion core",
            )),
            ..Default::default()
        }))
    }

    async fn health_check(
        &self,
        _request: Request<pb::HealthCheckRequest>,
    ) -> Result<Response<pb::HealthCheckResponse>, Status> {
        Ok(Response::new(pb::HealthCheckResponse {
            healthy: true,
            message: "ok".to_string(),
        }))
    }
}

fn split_relationship_operation(operation: &str) -> (&str, &str) {
    operation.split_once(':').unwrap_or(("", operation))
}

fn uuid_like_id() -> u128 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .unwrap_or_default()
        .as_nanos()
}

fn now() -> prost_types::Timestamp {
    prost_types::Timestamp::from(std::time::SystemTime::now())
}

fn proto_error(code: &str, message: &str) -> ProtoError {
    ProtoError {
        code: code.to_string(),
        message: message.to_string(),
        ..Default::default()
    }
}

fn error_response(request_id: &str, code: &str, message: &str) -> pb::InvokeResponse {
    pb::InvokeResponse {
        request_id: request_id.to_string(),
        success: false,
        error: Some(proto_error(code, message)),
        ..Default::default()
    }
}

fn resource_response(request_id: &str, response: crate::ResourceResponse) -> pb::InvokeResponse {
    let mut result = pb::InvokeResponse {
        request_id: request_id.to_string(),
        success: response.success,
        ..Default::default()
    };
    result.result = response
        .result
        .as_deref()
        .map(json_to_struct)
        .transpose()
        .unwrap_or_default();
    result.error = response
        .error
        .map(|error| proto_error(&error.code, &error.message));
    result
}

fn relationship_response(
    request_id: &str,
    response: crate::RelationshipResponse,
) -> pb::InvokeResponse {
    let mut result = pb::InvokeResponse {
        request_id: request_id.to_string(),
        success: response.success,
        ..Default::default()
    };
    result.result = response
        .result
        .as_deref()
        .map(json_to_struct)
        .transpose()
        .unwrap_or_default();
    result.error = response
        .error
        .map(|error| proto_error(&error.code, &error.message));
    result
}

fn struct_to_json_bytes(value: Option<&Struct>) -> Vec<u8> {
    let json = value
        .map(struct_to_json)
        .unwrap_or(JsonValue::Object(Map::new()));
    serde_json::to_vec(&json).unwrap_or_else(|_| b"{}".to_vec())
}

fn struct_to_json(value: &Struct) -> JsonValue {
    let mut object = Map::new();
    for (key, value) in &value.fields {
        object.insert(key.clone(), value_to_json(value));
    }
    JsonValue::Object(object)
}

fn value_to_json(value: &Value) -> JsonValue {
    match &value.kind {
        Some(Kind::NullValue(_)) | None => JsonValue::Null,
        Some(Kind::NumberValue(value)) => {
            if value.is_finite()
                && value.fract() == 0.0
                && *value >= i64::MIN as f64
                && *value <= i64::MAX as f64
            {
                JsonValue::Number(Number::from(*value as i64))
            } else {
                Number::from_f64(*value)
                    .map(JsonValue::Number)
                    .unwrap_or(JsonValue::Null)
            }
        }
        Some(Kind::StringValue(value)) => JsonValue::String(value.clone()),
        Some(Kind::BoolValue(value)) => JsonValue::Bool(*value),
        Some(Kind::StructValue(value)) => struct_to_json(value),
        Some(Kind::ListValue(ListValue { values })) => {
            JsonValue::Array(values.iter().map(value_to_json).collect())
        }
    }
}

fn json_to_struct(bytes: &[u8]) -> Result<Struct, serde_json::Error> {
    let value: JsonValue = serde_json::from_slice(bytes)?;
    Ok(match value {
        JsonValue::Object(object) => Struct {
            fields: object
                .into_iter()
                .map(|(key, value)| (key, json_to_value(value)))
                .collect(),
        },
        value => Struct {
            fields: BTreeMap::from([(String::from("value"), json_to_value(value))]),
        },
    })
}

fn json_to_value(value: JsonValue) -> Value {
    let kind = match value {
        JsonValue::Null => Kind::NullValue(0),
        JsonValue::Bool(value) => Kind::BoolValue(value),
        JsonValue::Number(value) => Kind::NumberValue(value.as_f64().unwrap_or_default()),
        JsonValue::String(value) => Kind::StringValue(value),
        JsonValue::Array(values) => Kind::ListValue(ListValue {
            values: values.into_iter().map(json_to_value).collect(),
        }),
        JsonValue::Object(values) => Kind::StructValue(Struct {
            fields: values
                .into_iter()
                .map(|(key, value)| (key, json_to_value(value)))
                .collect(),
        }),
    };
    Value { kind: Some(kind) }
}

use serde::de::DeserializeOwned;
use serde::Serialize;
use std::collections::HashMap;

pub struct ResourceHandler {
    pub kind: String,
    pub version: String,
    pub operations: HashMap<String, ResourceOperation>,
    pub capabilities: HashMap<String, serde_json::Value>,
}

impl ResourceHandler {
    pub fn new(kind: impl Into<String>, version: impl Into<String>) -> Self {
        Self {
            kind: kind.into(),
            version: version.into(),
            operations: HashMap::new(),
            capabilities: HashMap::new(),
        }
    }

    pub fn operation(mut self, name: impl Into<String>, op: ResourceOperation) -> Self {
        self.operations.insert(name.into(), op);
        self
    }

    pub fn capability(mut self, name: impl Into<String>, value: serde_json::Value) -> Self {
        self.capabilities.insert(name.into(), value);
        self
    }
}

pub type ResourceOperation =
    Box<dyn Fn(ResourceRequest) -> Result<ResourceResponse, OrionError> + Send + Sync>;

#[derive(Clone)]
pub struct ResourceRequest {
    pub request_id: String,
    pub provider_id: String,
    pub operation: String,
    pub payload: Vec<u8>,
}

impl ResourceRequest {
    pub fn payload_json<T: DeserializeOwned>(&self) -> Result<T, serde_json::Error> {
        serde_json::from_slice(&self.payload)
    }
}

#[derive(Clone)]
pub struct ResourceResponse {
    pub success: bool,
    pub result: Option<Vec<u8>>,
    pub error: Option<OrionError>,
}

impl ResourceResponse {
    pub fn success<T: Serialize>(data: T) -> Self {
        Self {
            success: true,
            result: serde_json::to_vec(&data).ok(),
            error: None,
        }
    }

    pub fn error(code: impl Into<String>, message: impl Into<String>) -> Self {
        Self {
            success: false,
            result: None,
            error: Some(OrionError::new(code, message)),
        }
    }
}

pub use crate::error::OrionError;

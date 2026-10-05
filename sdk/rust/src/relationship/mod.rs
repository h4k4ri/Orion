use serde::de::DeserializeOwned;
use std::collections::HashMap;

pub struct RelationshipHandler {
    pub kind: String,
    pub role: String,
    pub operations: HashMap<String, RelationshipOperation>,
}

impl RelationshipHandler {
    pub fn new(kind: impl Into<String>, role: impl Into<String>) -> Self {
        Self {
            kind: kind.into(),
            role: role.into(),
            operations: HashMap::new(),
        }
    }

    pub fn operation(mut self, name: impl Into<String>, op: RelationshipOperation) -> Self {
        self.operations.insert(name.into(), op);
        self
    }
}

pub type RelationshipOperation =
    Box<dyn Fn(RelationshipRequest) -> Result<RelationshipResponse, OrionError> + Send + Sync>;

#[derive(Clone)]
pub struct RelationshipRequest {
    pub request_id: String,
    pub provider_id: String,
    pub source_id: String,
    pub target_id: String,
    pub operation: String,
    pub payload: Vec<u8>,
}

impl RelationshipRequest {
    pub fn payload_json<T: DeserializeOwned>(&self) -> Result<T, serde_json::Error> {
        serde_json::from_slice(&self.payload)
    }
}

#[derive(Clone)]
pub struct RelationshipResponse {
    pub success: bool,
    pub result: Option<Vec<u8>>,
    pub error: Option<OrionError>,
}

pub use crate::error::OrionError;

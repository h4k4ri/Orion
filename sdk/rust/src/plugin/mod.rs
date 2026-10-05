pub use crate::error::OrionError;
pub use crate::resource::{ResourceHandler, ResourceOperation, ResourceRequest, ResourceResponse};
pub use crate::relationship::{RelationshipHandler, RelationshipOperation, RelationshipRequest, RelationshipResponse};
pub use crate::server::Server;

use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::sync::{Arc, RwLock};

#[derive(Clone)]
pub struct Config {
    pub id: String,
    pub name: String,
    pub version: String,
    pub vendor: String,
}

#[derive(Clone)]
pub struct Plugin {
    config: Config,
    resources: Arc<RwLock<HashMap<String, ResourceHandler>>>,
    relationships: Arc<RwLock<HashMap<String, RelationshipHandler>>>,
}

impl Plugin {
    pub fn new(config: Config) -> Self {
        Self {
            config,
            resources: Arc::new(RwLock::new(HashMap::new())),
            relationships: Arc::new(RwLock::new(HashMap::new())),
        }
    }

    pub fn resource(&self, kind: impl Into<String>, version: impl Into<String>) -> ResourceBuilder {
        ResourceBuilder::new(self.clone(), kind.into(), version.into())
    }

    pub fn relationship(
        &self,
        kind: impl Into<String>,
        role: impl Into<String>,
    ) -> RelationshipBuilder {
        RelationshipBuilder::new(self.clone(), kind.into(), role.into())
    }

    pub fn get_resource_handler(
        &self,
        kind: &str,
    ) -> Option<crate::resource::ResourceHandler> {
        self.resources.read().unwrap().get(kind).cloned()
    }

    pub fn get_relationship_handler(
        &self,
        kind: &str,
    ) -> Option<crate::relationship::RelationshipHandler> {
        self.relationships.read().unwrap().get(kind).cloned()
    }

    pub fn list_resources(&self) -> Vec<crate::resource::ResourceHandler> {
        self.resources.read().unwrap().values().cloned().collect()
    }

    pub fn list_relationships(&self) -> Vec<crate::relationship::RelationshipHandler> {
        self.relationships.read().unwrap().values().cloned().collect()
    }

    pub fn get_config(&self) -> Config {
        self.config.clone()
    }

    pub(in crate) fn register_resource(&self, handler: ResourceHandler) {
        self.resources
            .write()
            .unwrap()
            .insert(handler.kind.clone(), handler);
    }

    pub(in crate) fn register_relationship(&self, handler: RelationshipHandler) {
        self.relationships
            .write()
            .unwrap()
            .insert(handler.kind.clone(), handler);
    }
}

#[derive(Clone)]
pub struct ResourceBuilder {
    plugin: Plugin,
    kind: String,
    version: String,
    operations: HashMap<String, crate::resource::ResourceOperation>,
    capabilities: HashMap<String, serde_json::Value>,
}

impl ResourceBuilder {
    fn new(plugin: Plugin, kind: String, version: String) -> Self {
        Self {
            plugin,
            kind,
            version,
            operations: HashMap::new(),
            capabilities: HashMap::new(),
        }
    }

    pub fn handle(
        mut self,
        operation: impl Into<String>,
        handler: crate::resource::ResourceOperation,
    ) -> Self {
        self.operations.insert(operation.into(), handler);
        self
    }

    pub fn add_capability(
        mut self,
        name: impl Into<String>,
        value: impl Into<serde_json::Value>,
    ) -> Self {
        self.capabilities.insert(name.into(), value.into());
        self
    }

    pub fn register(self) {
        let handler = ResourceHandler {
            kind: self.kind,
            version: self.version,
            operations: self.operations,
            capabilities: self.capabilities,
        };
        self.plugin.register_resource(handler);
    }
}

#[derive(Clone)]
pub struct RelationshipBuilder {
    plugin: Plugin,
    kind: String,
    role: String,
    operations: HashMap<String, crate::relationship::RelationshipOperation>,
}

impl RelationshipBuilder {
    fn new(plugin: Plugin, kind: String, role: String) -> Self {
        Self {
            plugin,
            kind,
            role,
            operations: HashMap::new(),
        }
    }

    pub fn handle(
        mut self,
        operation: impl Into<String>,
        handler: crate::relationship::RelationshipOperation,
    ) -> Self {
        self.operations.insert(operation.into(), handler);
        self
    }

    pub fn register(self) {
        let handler = RelationshipHandler {
            kind: self.kind,
            role: self.role,
            operations: self.operations,
        };
        self.plugin.register_relationship(handler);
    }
}

#[derive(Debug, Serialize, Deserialize)]
pub struct PluginManifest {
    pub api_version: String,
    pub plugin_id: String,
    pub name: String,
    pub version: String,
    pub vendor: String,
    pub runtime: RuntimeInfo,
    pub implements: Vec<ImplementedResource>,
    pub implements_relationships: Vec<ImplementedRelationship>,
}

#[derive(Debug, Serialize, Deserialize)]
pub struct RuntimeInfo {
    pub protocol: String,
    pub protocol_version: String,
}

#[derive(Debug, Serialize, Deserialize)]
pub struct ImplementedResource {
    pub kind: String,
    pub version: String,
    pub operations: Vec<String>,
    pub capabilities: HashMap<String, serde_json::Value>,
}

#[derive(Debug, Serialize, Deserialize)]
pub struct ImplementedRelationship {
    pub kind: String,
    pub version: String,
    pub role: String,
    pub operations: Vec<String>,
}

impl Plugin {
    pub fn build_manifest(&self) -> PluginManifest {
        let config = self.get_config();

        let implements: Vec<ImplementedResource> = self
            .list_resources()
            .into_iter()
            .map(|r| ImplementedResource {
                kind: r.kind,
                version: r.version,
                operations: r.operations.keys().cloned().collect(),
                capabilities: r.capabilities,
            })
            .collect();

        let implements_relationships: Vec<ImplementedRelationship> = self
            .list_relationships()
            .into_iter()
            .map(|r| ImplementedRelationship {
                kind: r.kind,
                version: r.version,
                role: r.role,
                operations: r.operations.keys().cloned().collect(),
            })
            .collect();

        PluginManifest {
            api_version: "orion.io/v1".to_string(),
            plugin_id: config.id,
            name: config.name,
            version: config.version,
            vendor: config.vendor,
            runtime: RuntimeInfo {
                protocol: "grpc".to_string(),
                protocol_version: "v1".to_string(),
            },
            implements,
            implements_relationships,
        }
    }
}

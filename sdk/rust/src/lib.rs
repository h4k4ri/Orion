use std::collections::HashMap;
use std::sync::{Arc, RwLock};

pub mod error;
pub mod relationship;
pub mod resource;
pub mod server;

pub mod proto {
    pub mod plugin {
        pub mod v1 {
            tonic::include_proto!("plugin.v1");
        }
    }
}

pub use error::OrionError;
pub use relationship::{
    RelationshipHandler, RelationshipOperation, RelationshipRequest, RelationshipResponse,
};
pub use resource::{ResourceHandler, ResourceOperation, ResourceRequest, ResourceResponse};
pub use server::run as serve;

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
    resources: Arc<RwLock<HashMap<String, Arc<ResourceHandler>>>>,
    relationships: Arc<RwLock<HashMap<String, Arc<RelationshipHandler>>>>,
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
        ResourceBuilder::new(Arc::new(self.clone()), kind.into(), version.into())
    }

    pub fn relationship(
        &self,
        kind: impl Into<String>,
        role: impl Into<String>,
    ) -> RelationshipBuilder {
        RelationshipBuilder::new(Arc::new(self.clone()), kind.into(), role.into())
    }

    pub fn get_resource_handler(&self, kind: &str) -> Option<Arc<ResourceHandler>> {
        self.resources.read().unwrap().get(kind).cloned()
    }

    pub fn list_resources(&self) -> Vec<Arc<ResourceHandler>> {
        self.resources.read().unwrap().values().cloned().collect()
    }

    pub fn get_relationship_handler(&self, kind: &str) -> Option<Arc<RelationshipHandler>> {
        self.relationships.read().unwrap().get(kind).cloned()
    }

    pub fn list_relationships(&self) -> Vec<Arc<RelationshipHandler>> {
        self.relationships
            .read()
            .unwrap()
            .values()
            .cloned()
            .collect()
    }

    pub fn get_config(&self) -> Config {
        self.config.clone()
    }

    pub fn register_resource(&self, handler: ResourceHandler) {
        self.resources
            .write()
            .unwrap()
            .insert(handler.kind.clone(), Arc::new(handler));
    }

    pub fn register_relationship(&self, handler: RelationshipHandler) {
        self.relationships
            .write()
            .unwrap()
            .insert(handler.kind.clone(), Arc::new(handler));
    }
}

pub struct ResourceBuilder {
    plugin: Arc<Plugin>,
    kind: String,
    version: String,
    operations: HashMap<String, ResourceOperation>,
    capabilities: HashMap<String, serde_json::Value>,
}

impl ResourceBuilder {
    fn new(plugin: Arc<Plugin>, kind: String, version: String) -> Self {
        Self {
            plugin,
            kind,
            version,
            operations: HashMap::new(),
            capabilities: HashMap::new(),
        }
    }

    pub fn handle(mut self, operation: impl Into<String>, handler: ResourceOperation) -> Self {
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

pub struct RelationshipBuilder {
    plugin: Arc<Plugin>,
    kind: String,
    role: String,
    operations: HashMap<String, RelationshipOperation>,
}

impl RelationshipBuilder {
    fn new(plugin: Arc<Plugin>, kind: String, role: String) -> Self {
        Self {
            plugin,
            kind,
            role,
            operations: HashMap::new(),
        }
    }

    pub fn handle(mut self, operation: impl Into<String>, handler: RelationshipOperation) -> Self {
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

pub fn new_plugin(config: Config) -> Plugin {
    Plugin::new(config)
}

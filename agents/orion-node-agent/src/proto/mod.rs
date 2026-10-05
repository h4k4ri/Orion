#![allow(clippy::derive_partial_eq_without_eq)]
#![allow(clippy::large_enum_variant)]

use std::collections::HashMap;

use prost::{DecodeError, Message};
use serde::{Deserialize, Serialize};

pub mod node {
    use super::*;

    #[derive(Clone, PartialEq, Eq, Debug, Serialize, Deserialize)]
    pub struct ResourceID {
        pub value: String,
    }

    #[derive(Clone, PartialEq, Eq, Debug, Serialize, Deserialize)]
    pub struct ProjectID {
        pub value: String,
    }

    #[derive(Clone, PartialEq, Eq, Debug, Serialize, Deserialize)]
    pub struct RequestID {
        pub value: String,
    }

    #[derive(Clone, PartialEq, Eq, Debug, Serialize, Deserialize)]
    pub struct OperationID {
        pub value: String,
    }

    #[derive(Clone, PartialEq, Eq, Debug, Serialize, Deserialize)]
    pub struct NodeID {
        pub value: String,
    }

    #[derive(Clone, PartialEq, Eq, Debug, Serialize, Deserialize)]
    pub struct InstanceID {
        pub value: String,
    }

    #[derive(Clone, PartialEq, Eq, Debug, Serialize, Deserialize)]
    pub struct VolumeID {
        pub value: String,
    }

    #[derive(Clone, PartialEq, Eq, Debug, Serialize, Deserialize)]
    pub struct NetworkID {
        pub value: String,
    }

    #[derive(Clone, PartialEq, Eq, Debug, Serialize, Deserialize)]
    pub struct SubnetID {
        pub value: String,
    }

    #[derive(Clone, PartialEq, Eq, Debug, Serialize, Deserialize)]
    pub struct PortID {
        pub value: String,
    }

    #[derive(Clone, PartialEq, Eq, Debug, Serialize, Deserialize)]
    pub struct NodeResources {
        pub cpu: Option<CpuResources>,
        pub memory: Option<MemoryResources>,
        pub disk: Option<DiskResources>,
    }

    #[derive(Clone, PartialEq, Eq, Debug, Serialize, Deserialize)]
    pub struct CpuResources {
        pub total: i64,
        pub reserved: i64,
        pub allocated: i64,
    }

    #[derive(Clone, PartialEq, Eq, Debug, Serialize, Deserialize)]
    pub struct MemoryResources {
        pub total_mb: i64,
        pub reserved_mb: i64,
        pub allocated_mb: i64,
    }

    #[derive(Clone, PartialEq, Eq, Debug, Serialize, Deserialize)]
    pub struct DiskResources {
        pub total_gb: i64,
        pub reserved_gb: i64,
        pub allocated_gb: i64,
    }

    #[derive(Clone, PartialEq, Eq, Debug, Serialize, Deserialize)]
    pub struct NodeTraits {
        pub traits: Vec<String>,
        pub hypervisors: Vec<String>,
        pub cpu_vendors: Vec<String>,
        pub network_capabilities: Vec<String>,
        pub storage_capabilities: Vec<String>,
    }

    #[derive(Clone, PartialEq, Eq, Debug, Serialize, Deserialize)]
    pub struct NodeTopology {
        pub datacenter: String,
        pub availability_zone: String,
        pub rack: String,
        pub host_aggregates: Vec<String>,
    }

    #[derive(Clone, PartialEq, Eq, Debug, Serialize, Deserialize)]
    pub enum NodeStatus {
        Unspecified = 0,
        Online = 1,
        Offline = 2,
        Maintenance = 3,
        Draining = 4,
    }

    #[derive(Clone, PartialEq, Eq, Debug, Serialize, Deserialize)]
    pub struct InstanceSummary {
        pub id: Option<InstanceID>,
        pub name: String,
        pub status: String,
        pub vcpus: i32,
        pub memory_mb: i64,
        pub disk_gb: i64,
    }

    #[derive(Clone, PartialEq, Eq, Debug, Serialize, Deserialize)]
    pub struct ImageSpec {
        pub id: Option<ResourceID>,
        pub source_path: String,
        pub checksum_sha256: String,
        pub size_bytes: i64,
        pub source_url: String,
    }

    #[derive(Clone, PartialEq, Eq, Debug, Serialize, Deserialize)]
    pub struct FixedIP {
        pub subnet_id: String,
        pub ip_address: String,
    }

    #[derive(Clone, PartialEq, Eq, Debug, Serialize, Deserialize)]
    pub struct PortSpec {
        pub id: Option<PortID>,
        pub network_id: String,
        pub mac_address: String,
        pub fixed_ips: Vec<FixedIP>,
        pub vlan_tag: i32,
    }

    #[derive(Clone, PartialEq, Eq, Debug, Serialize, Deserialize)]
    pub struct VolumeAttachmentSpec {
        pub volume_id: Option<VolumeID>,
        pub device_index: i32,
    }

    #[derive(Clone, PartialEq, Eq, Debug, Serialize, Deserialize)]
    pub struct InstanceSpec {
        pub vcpus: i32,
        pub memory_mb: i64,
        pub disk_gb: i64,
        pub flavor_id: String,
        pub image: Option<ImageSpec>,
        pub ports: Vec<PortSpec>,
        pub volumes: Vec<VolumeAttachmentSpec>,
    }

    #[derive(Clone, PartialEq, Eq, Debug, Serialize, Deserialize)]
    pub struct DesiredStateUpdate {
        pub resource_type: String,
        pub resource_id: String,
        pub action: String,
        pub payload: Vec<u8>,
    }

    #[derive(Clone, PartialEq, Eq, Debug, Serialize, Deserialize)]
    pub struct RegisterNodeRequest {
        pub node: Option<Node>,
    }

    #[derive(Clone, PartialEq, Eq, Debug, Serialize, Deserialize)]
    pub struct RegisterNodeResponse {
        pub node_id: Option<NodeID>,
        pub accepted: bool,
        pub message: String,
    }

    #[derive(Clone, PartialEq, Eq, Debug, Serialize, Deserialize)]
    pub struct Node {
        pub id: Option<NodeID>,
        pub resources: Option<NodeResources>,
        pub traits: Option<NodeTraits>,
        pub topology: Option<NodeTopology>,
        pub status: i32,
        pub registered_at: Option<prost_types::Timestamp>,
        pub last_heartbeat_at: Option<prost_types::Timestamp>,
    }

    #[derive(Clone, PartialEq, Eq, Debug, Serialize, Deserialize)]
    pub struct HeartbeatRequest {
        pub node_id: Option<NodeID>,
        pub resources: Option<NodeResources>,
        pub status: i32,
        pub timestamp: Option<prost_types::Timestamp>,
    }

    #[derive(Clone, PartialEq, Eq, Debug, Serialize, Deserialize)]
    pub struct HeartbeatResponse {
        pub ok: bool,
        pub desired_updates: Vec<DesiredStateUpdate>,
    }

    #[derive(Clone, PartialEq, Eq, Debug, Serialize, Deserialize)]
    pub struct GetInventoryRequest {
        pub node_id: Option<NodeID>,
    }

    #[derive(Clone, PartialEq, Eq, Debug, Serialize, Deserialize)]
    pub struct GetInventoryResponse {
        pub resources: Option<NodeResources>,
        pub traits: Option<NodeTraits>,
        pub instances: Vec<InstanceSummary>,
    }

    #[derive(Clone, PartialEq, Eq, Debug, Serialize, Deserialize)]
    pub struct BuildInstanceRequest {
        pub instance_id: Option<InstanceID>,
        pub host_id: Option<NodeID>,
        pub name: String,
        pub spec: Option<InstanceSpec>,
    }

    #[derive(Clone, PartialEq, Eq, Debug, Serialize, Deserialize)]
    pub struct BuildInstanceResponse {
        pub domain_name: String,
        pub status: String,
        pub disk_path: String,
        pub network_ports: Vec<String>,
    }

    #[derive(Clone, PartialEq, Eq, Debug, Serialize, Deserialize)]
    pub struct DestroyInstanceRequest {
        pub instance_id: Option<InstanceID>,
        pub host_id: Option<NodeID>,
    }

    #[derive(Clone, PartialEq, Eq, Debug, Serialize, Deserialize)]
    pub struct DestroyInstanceResponse {
        pub success: bool,
        pub error: String,
    }

    #[derive(Clone, PartialEq, Eq, Debug, Serialize, Deserialize)]
    pub struct ObserveInstanceRequest {
        pub instance_id: Option<InstanceID>,
        pub host_id: Option<NodeID>,
    }

    #[derive(Clone, PartialEq, Eq, Debug, Serialize, Deserialize)]
    pub struct ObserveInstanceResponse {
        pub status: String,
        pub domain_name: String,
        pub observed_state: String,
    }

    #[derive(Clone, PartialEq, Eq, Debug, Serialize, Deserialize)]
    pub struct AttachVolumeRequest {
        pub instance_id: Option<InstanceID>,
        pub host_id: Option<NodeID>,
        pub volume_id: Option<VolumeID>,
        pub device_path: String,
    }

    #[derive(Clone, PartialEq, Eq, Debug, Serialize, Deserialize)]
    pub struct AttachVolumeResponse {
        pub success: bool,
        pub device_path: String,
        pub error: String,
    }

    #[derive(Clone, PartialEq, Eq, Debug, Serialize, Deserialize)]
    pub struct DetachVolumeRequest {
        pub instance_id: Option<InstanceID>,
        pub host_id: Option<NodeID>,
        pub volume_id: Option<VolumeID>,
    }

    #[derive(Clone, PartialEq, Eq, Debug, Serialize, Deserialize)]
    pub struct DetachVolumeResponse {
        pub success: bool,
        pub error: String,
    }

    #[derive(Clone, PartialEq, Eq, Debug, Serialize, Deserialize)]
    pub struct HealthResponse {
        pub healthy: bool,
        pub version: String,
        pub libvirt_status: String,
    }
}

pub mod node_grpc {
    #![allow(clippy::derive_partial_eq_without_eq)]
    use super::node::*;

    tonic::include_proto!("node.v1");

    pub mod node_agent_service_server {
        use super::*;
        use tonic::{Response, Status};
        use async_trait::async_trait;

        #[async_trait]
        pub trait NodeAgentService: Send + Sync {
            async fn register_node(
                &self,
                request: Request<RegisterNodeRequest>,
            ) -> Result<Response<RegisterNodeResponse>, Status>;

            async fn heartbeat(
                &self,
                request: Request<HeartbeatRequest>,
            ) -> Result<Response<HeartbeatResponse>, Status>;

            async fn get_inventory(
                &self,
                request: Request<GetInventoryRequest>,
            ) -> Result<Response<GetInventoryResponse>, Status>;

            async fn build_instance(
                &self,
                request: Request<BuildInstanceRequest>,
            ) -> Result<Response<BuildInstanceResponse>, Status>;

            async fn destroy_instance(
                &self,
                request: Request<DestroyInstanceRequest>,
            ) -> Result<Response<DestroyInstanceResponse>, Status>;

            async fn observe_instance(
                &self,
                request: Request<ObserveInstanceRequest>,
            ) -> Result<Response<ObserveInstanceResponse>, Status>;

            async fn attach_volume(
                &self,
                request: Request<AttachVolumeRequest>,
            ) -> Result<Response<AttachVolumeResponse>, Status>;

            async fn detach_volume(
                &self,
                request: Request<DetachVolumeRequest>,
            ) -> Result<Response<DetachVolumeResponse>, Status>;

            async fn health(
                &self,
                request: Request<()>,
            ) -> Result<Response<HealthResponse>, Status>;
        }
    }
}

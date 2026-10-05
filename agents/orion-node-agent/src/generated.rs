#[path = "generated/node.v1.rs"]
pub mod node;

pub mod common_v1 {
    include!("generated/common.v1.rs");
}

#[path = "generated/placement.v1.rs"]
pub mod placement;

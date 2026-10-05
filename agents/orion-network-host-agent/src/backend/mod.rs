use anyhow::Result;

pub mod ovs;

pub trait NetworkBackend: Send + Sync {
    fn name(&self) -> &'static str;
    fn ensure_integration_bridge(&self, allow_missing: bool) -> Result<bool>;
}

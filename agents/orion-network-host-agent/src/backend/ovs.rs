use std::{path::Path, process::Command};

use anyhow::{anyhow, Context, Result};

use super::NetworkBackend;

#[derive(Debug, Clone)]
pub struct OvsBackend {
    bridge: String,
}

impl OvsBackend {
    pub fn new(bridge: String) -> Self {
        Self { bridge }
    }
}

impl NetworkBackend for OvsBackend {
    fn name(&self) -> &'static str {
        "ovs"
    }

    fn ensure_integration_bridge(&self, allow_missing: bool) -> Result<bool> {
        let bridge_path = Path::new("/sys/class/net").join(&self.bridge);
        if bridge_path.exists() {
            return Ok(true);
        }
        if allow_missing {
            return Ok(false);
        }
        let output = Command::new("ovs-vsctl")
            .arg("--may-exist")
            .arg("add-br")
            .arg(&self.bridge)
            .output()
            .context("failed to execute ovs-vsctl")?;
        if !output.status.success() {
            return Err(anyhow!(
                "failed to ensure OVS integration bridge {}: {}",
                self.bridge,
                String::from_utf8_lossy(&output.stderr).trim()
            ));
        }
        Ok(bridge_path.exists())
    }
}

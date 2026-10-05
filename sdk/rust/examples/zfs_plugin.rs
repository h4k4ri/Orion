use orion_plugin_sdk::{new_plugin, serve, Config, OrionError};
use serde::{Deserialize, Serialize};

#[derive(Debug, Serialize, Deserialize)]
struct VolumeSpec {
    name: String,
    size: u64,
    pool: String,
}

#[derive(Debug, Serialize, Deserialize)]
struct VolumeResponse {
    id: String,
    name: String,
    size: u64,
}

fn create_volume(
    req: orion_plugin_sdk::ResourceRequest,
) -> Result<orion_plugin_sdk::ResourceResponse, OrionError> {
    let spec: VolumeSpec = req
        .payload_json()
        .map_err(|e| OrionError::new("INVALID_INPUT", e.to_string()))?;

    let id = format!("{}/{}", spec.pool, spec.name);

    Ok(orion_plugin_sdk::ResourceResponse::success(
        VolumeResponse {
            id,
            name: spec.name,
            size: spec.size,
        },
    ))
}

fn delete_volume(
    req: orion_plugin_sdk::ResourceRequest,
) -> Result<orion_plugin_sdk::ResourceResponse, OrionError> {
    let input: serde_json::Value = req
        .payload_json()
        .map_err(|e| OrionError::new("INVALID_INPUT", e.to_string()))?;

    println!("Deleting volume: {:?}", input);

    Ok(orion_plugin_sdk::ResourceResponse::success(()))
}

fn resize_volume(
    req: orion_plugin_sdk::ResourceRequest,
) -> Result<orion_plugin_sdk::ResourceResponse, OrionError> {
    let input: serde_json::Value = req
        .payload_json()
        .map_err(|e| OrionError::new("INVALID_INPUT", e.to_string()))?;

    println!("Resizing volume: {:?}", input);

    Ok(orion_plugin_sdk::ResourceResponse::success(()))
}

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let plugin = new_plugin(Config {
        id: "zfs-plugin".to_string(),
        name: "ZFS".to_string(),
        version: "1.0.0".to_string(),
        vendor: "Orion".to_string(),
    });

    plugin
        .resource("orion.io/storage.volume", "v1")
        .handle("create", Box::new(create_volume))
        .handle("delete", Box::new(delete_volume))
        .handle("resize", Box::new(resize_volume))
        .add_capability("snapshots", true)
        .add_capability("encryption", false)
        .register();

    let address = std::env::var("ORION_PLUGIN_ENDPOINT").unwrap_or_else(|_| ":50052".to_string());
    println!("ZFS plugin starting on {}", address);
    serve(plugin, address).await?;
    Ok(())
}

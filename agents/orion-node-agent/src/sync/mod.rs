use std::path::{Path, PathBuf};

use anyhow::{anyhow, Context, Result};
use virt::domain::Domain;

use crate::http_types::{
    AttachVolumeRequest, BuildServerRequest, BuildServerResponse, DetachVolumeRequest, ImageSource,
    NetworkPort, ServerStatusResponse,
};

pub const DEVICE_PERSIST_FLAGS: u32 = 1 | 2;

pub fn build_server_sync(
    storage_dir: &PathBuf,
    req: BuildServerRequest,
) -> Result<BuildServerResponse> {
    use virt::connect::Connect;

    let domain_name = format!("orion-{}", req.server_id);
    let disk_path = storage_dir.join(format!("{}.qcow2", req.server_id));
    let xml_path = storage_dir.join(format!("{}.xml", req.server_id));

    let conn = crate::domain::open_libvirt()?;
    if let Ok(domain) = Domain::lookup_by_name(&conn, &domain_name) {
        if !domain.is_active().unwrap_or(false) {
            domain
                .create()
                .map_err(|e| anyhow!("failed to start existing domain {domain_name}: {e}"))?;
        }
        return Ok(BuildServerResponse {
            domain_name,
            status: "active".to_string(),
            disk_path: disk_path.display().to_string(),
        });
    }
    let backing_path = crate::domain::resolve_image_path(storage_dir, &req.image)?;
    let disk_path = if disk_path.exists() {
        disk_path
    } else {
        crate::domain::create_overlay_disk(
            &conn,
            storage_dir,
            &disk_path,
            &backing_path,
            req.disk_gb,
        )?
    };
    crate::domain::ensure_ovs_ready(&req.ports)?;
    let xml_str = crate::domain::write_domain_xml(
        &xml_path,
        &domain_name,
        &disk_path,
        &req.ports,
        req.vcpus,
        req.memory_mb,
    )?;
    let domain = Domain::define_xml(&conn, &xml_str)
        .map_err(|e| anyhow!("failed to define domain {domain_name}: {e}"))?;

    domain
        .create()
        .map_err(|e| anyhow!("failed to start domain {domain_name}: {e}"))?;

    tracing::info!(
        server_id = %req.server_id,
        host_id = %req.host_id,
        domain_name = %domain_name,
        disk_path = %disk_path.display(),
        "libvirt domain started"
    );

    Ok(BuildServerResponse {
        domain_name,
        status: "active".to_string(),
        disk_path: disk_path.display().to_string(),
    })
}

pub fn delete_server_sync(storage_dir: &PathBuf, server_id: &str) -> Result<()> {
    let domain_name = format!("orion-{server_id}");
    let disk_path = storage_dir.join(format!("{server_id}.qcow2"));
    let xml_path = storage_dir.join(format!("{server_id}.xml"));

    let conn = crate::domain::open_libvirt()?;
    crate::domain::ensure_domain_absent(&conn, &domain_name)?;

    let _ = crate::domain::delete_overlay_disk(&conn, &disk_path);
    let _ = std::fs::remove_file(disk_path);
    let _ = std::fs::remove_file(xml_path);
    Ok(())
}

pub fn get_server_sync(server_id: &str) -> Result<ServerStatusResponse> {
    use virt::connect::Connect;

    let domain_name = format!("orion-{server_id}");
    let conn = crate::domain::open_libvirt()?;
    let domain = Domain::lookup_by_name(&conn, &domain_name)
        .map_err(|e| anyhow!("domain not found: {domain_name}: {e}"))?;

    let status: &'static str = if domain
        .is_active()
        .map_err(|e| anyhow!("failed to inspect domain {domain_name}: {e}"))?
    {
        "active"
    } else {
        "shutoff"
    };

    Ok(ServerStatusResponse {
        server_id: server_id.to_string(),
        domain_name,
        status: status.to_string(),
    })
}

pub fn attach_volume_sync(server_id: &str, req: AttachVolumeRequest) -> Result<()> {
    use virt::connect::Connect;

    let domain_name = format!("orion-{server_id}");
    let conn = crate::domain::open_libvirt()?;
    let domain = Domain::lookup_by_name(&conn, &domain_name)
        .map_err(|e| anyhow!("domain {domain_name} not found: {e}"))?;

    let target = crate::domain::next_disk_target(&domain)?;
    tracing::info!(
        server_id,
        volume_id = %req.volume_id,
        device_path = %req.device_path,
        target = %target,
        "attaching volume"
    );

    let disk_xml = format!(
        "<disk type='block' device='disk'>\
<driver name='qemu' type='raw'/>\
<source dev='{}'/>\
<target dev='{}' bus='virtio'/>\
</disk>",
        req.device_path, target
    );

    domain
        .attach_device_flags(&disk_xml, DEVICE_PERSIST_FLAGS)
        .map(|_| ())
        .map_err(|e| anyhow!("failed to attach disk to {domain_name}: {e}"))
}

pub fn detach_volume_sync(server_id: &str, req: DetachVolumeRequest) -> Result<()> {
    use virt::connect::Connect;

    let domain_name = format!("orion-{server_id}");
    let conn = crate::domain::open_libvirt()?;
    let domain = Domain::lookup_by_name(&conn, &domain_name)
        .map_err(|e| anyhow!("domain {domain_name} not found: {e}"))?;

    let domain_xml = domain
        .get_xml_desc(0)
        .map_err(|e| anyhow!("failed to get XML for {domain_name}: {e}"))?;

    let Some((target, source)) =
        crate::domain::find_attached_disk_in_xml(&domain_xml, &req.volume_id, &req.device_path)
    else {
        tracing::info!(
            server_id,
            volume_id = %req.volume_id,
            device_path = %req.device_path,
            "volume already detached from domain"
        );
        return Ok(());
    };

    tracing::info!(
        server_id,
        volume_id = %req.volume_id,
        device_path = %req.device_path,
        target = %target,
        source = %source,
        "detaching volume"
    );

    let detach_xml = format!(
        "<disk type='block' device='disk'>\
<driver name='qemu' type='raw'/>\
<source dev='{}'/>\
<target dev='{}' bus='virtio'/>\
</disk>",
        source, target
    );

    domain
        .detach_device_flags(&detach_xml, DEVICE_PERSIST_FLAGS)
        .map(|_| ())
        .map_err(|e| anyhow!("failed to detach disk from {domain_name}: {e}"))
}

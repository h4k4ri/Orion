use std::env;

use anyhow::{Context, Result};
use tonic::transport::{Certificate, Identity, ServerTlsConfig};

pub fn server_tls_config_from_env() -> Result<Option<ServerTlsConfig>> {
    let cert = env::var("ORION_TLS_CERT_FILE").unwrap_or_default();
    let key = env::var("ORION_TLS_KEY_FILE").unwrap_or_default();
    let ca = env::var("ORION_TLS_CA_FILE").unwrap_or_default();
    if cert.is_empty() && key.is_empty() && ca.is_empty() {
        return Ok(None);
    }
    if cert.is_empty() || key.is_empty() || ca.is_empty() {
        anyhow::bail!("ORION_TLS_CERT_FILE, ORION_TLS_KEY_FILE and ORION_TLS_CA_FILE are required for agent mTLS");
    }
    let cert_pem = std::fs::read(&cert).with_context(|| format!("read TLS certificate {cert}"))?;
    let key_pem = std::fs::read(&key).with_context(|| format!("read TLS key {key}"))?;
    let ca_pem = std::fs::read(&ca).with_context(|| format!("read TLS CA {ca}"))?;
    Ok(Some(
        ServerTlsConfig::new()
            .identity(Identity::from_pem(cert_pem, key_pem))
            .client_ca_root(Certificate::from_pem(ca_pem)),
    ))
}

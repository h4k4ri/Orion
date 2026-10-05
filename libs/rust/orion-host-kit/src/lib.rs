pub mod nats;
pub mod tls;

use tracing_subscriber::{fmt, EnvFilter};

pub fn init_logging(service: &str) {
    let filter = EnvFilter::try_from_default_env().unwrap_or_else(|_| EnvFilter::new("info"));

    fmt()
        .with_env_filter(filter)
        .json()
        .with_target(true)
        .with_thread_names(true)
        .with_writer(std::io::stdout)
        .init();

    tracing::info!(service, "logging initialized");
}

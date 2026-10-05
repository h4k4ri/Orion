pub mod subjects {
    pub const STREAM_ORION_COMMANDS: &str = "ORION_COMMANDS";
    pub const STREAM_ORION_EVENTS: &str = "ORION_EVENTS";
    pub const STREAM_ORION_DESIRED_STATE: &str = "ORION_DESIRED_STATE";

    pub const SUBJECT_COMMAND_COMPUTE: &str = "orion.command.compute.>";
    pub const SUBJECT_EVENT_COMPUTE: &str = "orion.event.compute.>";
    pub const SUBJECT_COMMAND_NETWORK: &str = "orion.command.network.>";
    pub const SUBJECT_EVENT_NETWORK: &str = "orion.event.network.>";
    pub const SUBJECT_COMMAND_VOLUME: &str = "orion.command.volume.>";
    pub const SUBJECT_EVENT_VOLUME: &str = "orion.event.volume.>";
    pub const SUBJECT_COMMAND_IMAGE: &str = "orion.command.image.>";
    pub const SUBJECT_EVENT_IMAGE: &str = "orion.event.image.>";
    pub const SUBJECT_COMMAND_PLACEMENT: &str = "orion.command.placement.>";
    pub const SUBJECT_EVENT_PLACEMENT: &str = "orion.event.placement.>";
    pub const SUBJECT_DESIRED_COMPUTE: &str = "orion.desired.compute.>";
    pub const SUBJECT_DESIRED_VOLUME: &str = "orion.desired.volume.>";
}

pub use subjects::*;

pub mod jetstream {
    use async_nats::jetstream::stream;
    use std::time::Duration;

    pub async fn ensure_stream(
        client: &async_nats::Client,
        name: &str,
        subjects: Vec<&str>,
    ) -> Result<(), async_nats::Error> {
        let jetstream = async_nats::jetstream::new(client.clone());
        let _stream = jetstream
            .get_or_create_stream(stream::Config {
                name: name.to_string(),
                subjects: subjects.into_iter().map(String::from).collect(),
                max_age: Duration::from_secs(7 * 24 * 60 * 60),
                storage: stream::StorageType::File,
                ..Default::default()
            })
            .await?;
        Ok(())
    }

    pub async fn ensure_default_streams(
        client: &async_nats::Client,
    ) -> Result<(), async_nats::Error> {
        ensure_stream(
            client,
            super::STREAM_ORION_COMMANDS,
            vec![
                super::SUBJECT_COMMAND_COMPUTE,
                super::SUBJECT_COMMAND_NETWORK,
                super::SUBJECT_COMMAND_VOLUME,
                super::SUBJECT_COMMAND_IMAGE,
                super::SUBJECT_COMMAND_PLACEMENT,
            ],
        )
        .await?;

        ensure_stream(
            client,
            super::STREAM_ORION_EVENTS,
            vec![
                super::SUBJECT_EVENT_COMPUTE,
                super::SUBJECT_EVENT_NETWORK,
                super::SUBJECT_EVENT_VOLUME,
                super::SUBJECT_EVENT_IMAGE,
                super::SUBJECT_EVENT_PLACEMENT,
            ],
        )
        .await?;

        ensure_stream(
            client,
            super::STREAM_ORION_DESIRED_STATE,
            vec![
                super::SUBJECT_DESIRED_COMPUTE,
                super::SUBJECT_DESIRED_VOLUME,
            ],
        )
        .await?;

        Ok(())
    }
}

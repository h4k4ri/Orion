CREATE TABLE IF NOT EXISTS orion_volume.project_quotas (
    project_id    TEXT PRIMARY KEY,
    volumes_limit INT NOT NULL DEFAULT 1000,
    volume_gb_limit INT NOT NULL DEFAULT 1048576,
    volumes_used  INT NOT NULL DEFAULT 0,
    volume_gb_used INT NOT NULL DEFAULT 0,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

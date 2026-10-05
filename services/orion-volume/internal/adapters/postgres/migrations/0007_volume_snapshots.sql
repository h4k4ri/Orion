CREATE TABLE IF NOT EXISTS orion_volume.volume_snapshots (
    id         TEXT PRIMARY KEY,
    volume_id  TEXT NOT NULL REFERENCES orion_volume.volumes(id) ON DELETE CASCADE,
    project_id TEXT NOT NULL,
    name       TEXT NOT NULL,
    size_gb    INT NOT NULL,
    status     TEXT NOT NULL DEFAULT 'available',
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS volume_snapshots_volume_idx ON orion_volume.volume_snapshots(volume_id);
CREATE INDEX IF NOT EXISTS volume_snapshots_project_idx ON orion_volume.volume_snapshots(project_id);

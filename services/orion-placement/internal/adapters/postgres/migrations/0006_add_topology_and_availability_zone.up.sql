-- 0006_add_topology_and_availability_zone.up.sql
-- Add availability_zone and topology fields to placement_hosts

ALTER TABLE placement_hosts
    ADD COLUMN IF NOT EXISTS availability_zone TEXT DEFAULT '',
    ADD COLUMN IF NOT EXISTS datacenter TEXT DEFAULT '',
    ADD COLUMN IF NOT EXISTS rack TEXT DEFAULT '',
    ADD COLUMN IF NOT EXISTS host_aggregate TEXT DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_placement_hosts_availability_zone
    ON placement_hosts(availability_zone) WHERE availability_zone != '';

CREATE INDEX IF NOT EXISTS idx_placement_hosts_host_aggregate
    ON placement_hosts(host_aggregate) WHERE host_aggregate != '';

-- 0007_reservations.up.sql
-- Reservations table with TTL and fencing tokens

CREATE TABLE IF NOT EXISTS placement_reservations (
    id              UUID PRIMARY KEY,
    host_id         UUID NOT NULL,
    project_id      UUID NOT NULL,
    server_id       UUID,
    vcpus           INT NOT NULL DEFAULT 0,
    memory_mb       INT NOT NULL DEFAULT 0,
    disk_gb         INT NOT NULL DEFAULT 0,
    fencing_token   BIGINT NOT NULL DEFAULT 0,
    expires_at      TIMESTAMPTZ NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_placement_reservations_host_id
    ON placement_reservations(host_id);
CREATE INDEX IF NOT EXISTS idx_placement_reservations_project_id
    ON placement_reservations(project_id);
CREATE INDEX IF NOT EXISTS idx_placement_reservations_expires_at
    ON placement_reservations(expires_at);
CREATE INDEX IF NOT EXISTS idx_placement_reservations_host_project
    ON placement_reservations(host_id, project_id);

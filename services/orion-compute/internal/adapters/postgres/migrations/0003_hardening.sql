CREATE TABLE IF NOT EXISTS orion_compute.project_quotas (
    project_id       TEXT PRIMARY KEY,
    instances_limit  INT NOT NULL DEFAULT 100,
    instances_used   INT NOT NULL DEFAULT 0,
    vcpus_limit      INT NOT NULL DEFAULT 1000,
    vcpus_used       INT NOT NULL DEFAULT 0,
    ram_mb_limit     INT NOT NULL DEFAULT 1048576,
    ram_mb_used      INT NOT NULL DEFAULT 0,
    volumes_limit    INT NOT NULL DEFAULT 1000,
    volumes_used     INT NOT NULL DEFAULT 0,
    networks_limit   INT NOT NULL DEFAULT 1000,
    networks_used    INT NOT NULL DEFAULT 0,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS orion_compute.resource_finalizers (
    resource_type TEXT NOT NULL,
    resource_id   TEXT NOT NULL,
    finalizer     TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (resource_type, resource_id, finalizer)
);

CREATE INDEX IF NOT EXISTS resource_finalizers_resource_idx
    ON orion_compute.resource_finalizers(resource_type, resource_id);

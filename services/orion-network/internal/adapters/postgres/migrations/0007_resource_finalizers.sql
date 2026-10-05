CREATE TABLE IF NOT EXISTS orion_network.resource_finalizers (
    resource_type TEXT NOT NULL,
    resource_id   TEXT NOT NULL,
    finalizer     TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (resource_type, resource_id, finalizer)
);

CREATE INDEX IF NOT EXISTS resource_finalizers_resource_idx
    ON orion_network.resource_finalizers(resource_type, resource_id);

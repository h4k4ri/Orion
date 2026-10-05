CREATE TABLE IF NOT EXISTS orion_placement.resource_providers (
    uuid                TEXT PRIMARY KEY,
    name                TEXT NOT NULL,
    parent_provider_id  TEXT REFERENCES orion_placement.resource_providers(uuid) ON DELETE RESTRICT,
    root_provider_id    TEXT NOT NULL,
    generation          BIGINT NOT NULL DEFAULT 1,
    traits              TEXT[] NOT NULL DEFAULT '{}',
    inventories         JSONB NOT NULL DEFAULT '{}'
);

CREATE INDEX IF NOT EXISTS resource_providers_root_idx
    ON orion_placement.resource_providers(root_provider_id);
CREATE INDEX IF NOT EXISTS resource_providers_parent_idx
    ON orion_placement.resource_providers(parent_provider_id);

CREATE TABLE IF NOT EXISTS orion_network.security_groups (
    id          TEXT PRIMARY KEY,
    project_id  TEXT NOT NULL,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS orion_network.security_group_rules (
    id               TEXT PRIMARY KEY,
    security_group_id TEXT NOT NULL REFERENCES orion_network.security_groups(id) ON DELETE CASCADE,
    direction        TEXT NOT NULL,
    ether_type       TEXT NOT NULL,
    protocol         TEXT NOT NULL DEFAULT '',
    port_min         INT NOT NULL DEFAULT 0,
    port_max         INT NOT NULL DEFAULT 0,
    remote_cidr      TEXT NOT NULL DEFAULT '',
    remote_group_id  TEXT NOT NULL DEFAULT '',
    description      TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL
);

ALTER TABLE orion_network.ports ADD COLUMN IF NOT EXISTS security_group_ids JSONB NOT NULL DEFAULT '[]';
CREATE INDEX IF NOT EXISTS security_groups_project_idx ON orion_network.security_groups(project_id);
CREATE INDEX IF NOT EXISTS security_group_rules_group_idx ON orion_network.security_group_rules(security_group_id);

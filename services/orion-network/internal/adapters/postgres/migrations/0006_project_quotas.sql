CREATE TABLE IF NOT EXISTS orion_network.project_quotas (
    project_id     TEXT PRIMARY KEY,
    networks_limit INT NOT NULL DEFAULT 1000,
    ports_limit    INT NOT NULL DEFAULT 10000,
    networks_used  INT NOT NULL DEFAULT 0,
    ports_used     INT NOT NULL DEFAULT 0,
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

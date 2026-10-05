CREATE TABLE IF NOT EXISTS orion_network.routers (
    id                  TEXT PRIMARY KEY,
    project_id          TEXT NOT NULL,
    name                TEXT NOT NULL,
    external_network_id TEXT NOT NULL DEFAULT '',
    enable_snat         BOOLEAN NOT NULL DEFAULT FALSE,
    snat_external_ip    TEXT NOT NULL DEFAULT '',
    status              TEXT NOT NULL DEFAULT 'active',
    created_at          TIMESTAMPTZ NOT NULL,
    updated_at          TIMESTAMPTZ NOT NULL
);

ALTER TABLE orion_network.routers
    ADD COLUMN IF NOT EXISTS snat_external_ip TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS orion_network.router_interfaces (
    id         TEXT PRIMARY KEY,
    router_id  TEXT NOT NULL REFERENCES orion_network.routers(id) ON DELETE CASCADE,
    subnet_id  TEXT NOT NULL,
    port_id    TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS orion_network.floating_ips (
    id                  TEXT PRIMARY KEY,
    project_id          TEXT NOT NULL,
    floating_network_id TEXT NOT NULL,
    floating_ip         TEXT NOT NULL,
    port_id             TEXT NOT NULL DEFAULT '',
    fixed_ip            TEXT NOT NULL DEFAULT '',
    router_id           TEXT NOT NULL,
    status              TEXT NOT NULL DEFAULT 'active',
    created_at          TIMESTAMPTZ NOT NULL,
    updated_at          TIMESTAMPTZ NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS floating_ips_network_ip_idx
    ON orion_network.floating_ips(floating_network_id, floating_ip);
CREATE INDEX IF NOT EXISTS router_interfaces_router_idx
    ON orion_network.router_interfaces(router_id);

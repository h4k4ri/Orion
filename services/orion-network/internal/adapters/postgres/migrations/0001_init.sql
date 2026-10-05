CREATE TABLE IF NOT EXISTS networks (
	id         TEXT PRIMARY KEY,
	project_id TEXT NOT NULL DEFAULT '',
	name       TEXT NOT NULL,
	status     TEXT NOT NULL DEFAULT 'active',
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS subnets (
	id                TEXT PRIMARY KEY,
	project_id        TEXT NOT NULL DEFAULT '',
	network_id        TEXT NOT NULL,
	name              TEXT NOT NULL,
	cidr              TEXT NOT NULL,
	gateway_ip        TEXT NOT NULL DEFAULT '',
	enable_dhcp       BOOLEAN NOT NULL DEFAULT TRUE,
	dhcp_options_uuid TEXT NOT NULL DEFAULT '',
	created_at        TIMESTAMPTZ NOT NULL,
	updated_at        TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS ports (
	id              TEXT PRIMARY KEY,
	project_id      TEXT NOT NULL DEFAULT '',
	network_id      TEXT NOT NULL,
	device_id       TEXT NOT NULL DEFAULT '',
	device_owner    TEXT NOT NULL DEFAULT '',
	binding_host_id TEXT NOT NULL DEFAULT '',
	mac_address     TEXT NOT NULL DEFAULT '',
	fixed_ips       JSONB NOT NULL DEFAULT '[]',
	status          TEXT NOT NULL DEFAULT 'down',
	vif_type        TEXT NOT NULL DEFAULT 'ovs',
	vnic_type       TEXT NOT NULL DEFAULT 'normal',
	created_at      TIMESTAMPTZ NOT NULL,
	updated_at      TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS ports_network_id ON ports(network_id);

CREATE TABLE IF NOT EXISTS volumes (
	id                 TEXT PRIMARY KEY,
	project_id         TEXT NOT NULL DEFAULT '',
	name               TEXT NOT NULL,
	size_gb            INT NOT NULL DEFAULT 0,
	cell_id            TEXT NOT NULL DEFAULT '',
	host_id            TEXT NOT NULL DEFAULT '',
	status             TEXT NOT NULL DEFAULT 'available',
	backend_type       TEXT NOT NULL DEFAULT 'lvm',
	device_path        TEXT NOT NULL DEFAULT '',
	attached_server_id TEXT NOT NULL DEFAULT '',
	created_at         TIMESTAMPTZ NOT NULL,
	updated_at         TIMESTAMPTZ NOT NULL
);

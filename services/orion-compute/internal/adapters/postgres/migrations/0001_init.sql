CREATE TABLE IF NOT EXISTS compute_servers (
	id         TEXT PRIMARY KEY,
	project_id TEXT NOT NULL,
	name       TEXT NOT NULL,
	image_id   TEXT NOT NULL,
	flavor     TEXT NOT NULL,
	vcpus      INT NOT NULL DEFAULT 0,
	memory_mb  INT NOT NULL DEFAULT 0,
	disk_gb    INT NOT NULL DEFAULT 0,
	cell_id    TEXT NOT NULL DEFAULT '',
	host_id    TEXT NOT NULL DEFAULT '',
	port_ids   TEXT[] NOT NULL DEFAULT '{}',
	volume_ids TEXT[] NOT NULL DEFAULT '{}',
	status     TEXT NOT NULL DEFAULT 'building',
	task_id    TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS compute_tasks (
	id            TEXT PRIMARY KEY,
	kind          TEXT NOT NULL,
	scope         TEXT NOT NULL,
	target_ref    TEXT NOT NULL,
	status        TEXT NOT NULL,
	requested_by  TEXT NOT NULL,
	request_id    TEXT NOT NULL,
	cell_id       TEXT NOT NULL DEFAULT '',
	host_id       TEXT NOT NULL DEFAULT '',
	error_code    TEXT NOT NULL DEFAULT '',
	error_message TEXT NOT NULL DEFAULT '',
	created_at    TIMESTAMPTZ NOT NULL,
	updated_at    TIMESTAMPTZ NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS compute_tasks_kind_request_id_idx
	ON compute_tasks (kind, request_id);

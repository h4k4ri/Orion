CREATE TABLE IF NOT EXISTS placement_hosts (
	host_id             TEXT PRIMARY KEY,
	cell_id             TEXT NOT NULL DEFAULT '',
	group_name          TEXT NOT NULL DEFAULT '',
	enabled             BOOLEAN NOT NULL DEFAULT TRUE,
	drained             BOOLEAN NOT NULL DEFAULT FALSE,
	traits              TEXT[] NOT NULL DEFAULT '{}',
	vcpus_total         INT NOT NULL DEFAULT 0,
	vcpus_allocated     INT NOT NULL DEFAULT 0,
	memory_mb_total     INT NOT NULL DEFAULT 0,
	memory_mb_allocated INT NOT NULL DEFAULT 0,
	disk_gb_total       INT NOT NULL DEFAULT 0,
	disk_gb_allocated   INT NOT NULL DEFAULT 0,
	generation          BIGINT NOT NULL DEFAULT 0
);

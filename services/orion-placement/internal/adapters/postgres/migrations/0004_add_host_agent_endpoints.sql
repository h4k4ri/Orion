ALTER TABLE placement_hosts
	ADD COLUMN IF NOT EXISTS node_agent_url TEXT NOT NULL DEFAULT '',
	ADD COLUMN IF NOT EXISTS volume_host_agent_url TEXT NOT NULL DEFAULT '',
	ADD COLUMN IF NOT EXISTS network_host_agent_url TEXT NOT NULL DEFAULT '';

UPDATE placement_hosts
SET
	node_agent_url = CASE
		WHEN node_agent_url = '' THEN 'http://127.0.0.1:8084'
		ELSE node_agent_url
	END,
	volume_host_agent_url = CASE
		WHEN volume_host_agent_url = '' THEN 'grpc://127.0.0.1:50055'
		ELSE volume_host_agent_url
	END,
	network_host_agent_url = CASE
		WHEN network_host_agent_url = '' THEN 'grpc://127.0.0.1:50054'
		ELSE network_host_agent_url
	END
WHERE host_id = 'host_local';

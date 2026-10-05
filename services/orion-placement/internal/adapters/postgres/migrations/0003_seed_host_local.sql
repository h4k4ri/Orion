INSERT INTO placement_hosts
	(host_id, cell_id, group_name, enabled, drained, traits,
	 vcpus_total, vcpus_allocated, memory_mb_total, memory_mb_allocated,
	 disk_gb_total, disk_gb_allocated, numa, gpus, generation)
VALUES
	('host_local', 'cell_local', 'general', TRUE, FALSE,
	 ARRAY['general','kvm'], 8, 0, 16384, 0, 500, 0,
	 '[{"id":0,"vcpus":[0,1,2,3],"memory_mb_total":8192,"memory_mb_allocated":0},{"id":1,"vcpus":[4,5,6,7],"memory_mb_total":8192,"memory_mb_allocated":0}]'::jsonb,
	 '[]'::jsonb,
	 1)
ON CONFLICT (host_id) DO NOTHING;

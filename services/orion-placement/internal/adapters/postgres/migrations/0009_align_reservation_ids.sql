ALTER TABLE IF EXISTS orion_placement.placement_reservations
    ALTER COLUMN id TYPE TEXT USING id::TEXT,
    ALTER COLUMN host_id TYPE TEXT USING host_id::TEXT,
    ALTER COLUMN project_id TYPE TEXT USING project_id::TEXT,
    ALTER COLUMN server_id TYPE TEXT USING server_id::TEXT;

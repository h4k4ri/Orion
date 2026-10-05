ALTER TABLE IF EXISTS operation_operations
    ALTER COLUMN id TYPE TEXT USING id::TEXT,
    ALTER COLUMN resource_id TYPE TEXT USING resource_id::TEXT,
    ALTER COLUMN project_id TYPE TEXT USING project_id::TEXT,
    ALTER COLUMN request_id TYPE TEXT USING request_id::TEXT;

ALTER TABLE IF EXISTS operation_operation_events
    ALTER COLUMN id TYPE TEXT USING id::TEXT,
    ALTER COLUMN operation_id TYPE TEXT USING operation_id::TEXT;

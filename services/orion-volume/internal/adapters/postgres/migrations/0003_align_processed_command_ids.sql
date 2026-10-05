ALTER TABLE IF EXISTS processed_commands
    ALTER COLUMN message_id TYPE TEXT USING message_id::TEXT,
    ALTER COLUMN operation_id TYPE TEXT USING operation_id::TEXT;

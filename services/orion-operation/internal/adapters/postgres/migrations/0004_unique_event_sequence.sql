CREATE UNIQUE INDEX IF NOT EXISTS uq_operation_operation_events_sequence
    ON operation_operation_events(operation_id, sequence);

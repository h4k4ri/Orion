-- 0002_operation_events.up.sql
-- Operation events table: append-only historical log

CREATE TABLE IF NOT EXISTS operation_operation_events (
    id           UUID PRIMARY KEY,
    operation_id UUID NOT NULL,
    sequence     BIGINT NOT NULL DEFAULT 0,
    event_type   TEXT NOT NULL,
    from_state   TEXT,
    to_state     TEXT,
    step         TEXT,
    payload      JSONB,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_operation_operation_events_operation_id
    ON operation_operation_events(operation_id);
CREATE INDEX IF NOT EXISTS idx_operation_operation_events_sequence
    ON operation_operation_events(operation_id, sequence);

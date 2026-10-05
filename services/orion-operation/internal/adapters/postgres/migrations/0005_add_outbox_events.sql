CREATE TABLE IF NOT EXISTS orion_operation.outbox_events (
    id              UUID PRIMARY KEY,
    aggregate_type  TEXT NOT NULL,
    aggregate_id    TEXT NOT NULL,
    subject         TEXT NOT NULL,
    payload         JSONB NOT NULL,
    trace_id        TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    published_at    TIMESTAMPTZ,
    attempts        INT NOT NULL DEFAULT 0
);

CREATE INDEX idx_outbox_events_aggregate ON orion_operation.outbox_events(aggregate_type, aggregate_id);
CREATE INDEX idx_outbox_events_unpublished ON orion_operation.outbox_events(published_at) WHERE published_at IS NULL;

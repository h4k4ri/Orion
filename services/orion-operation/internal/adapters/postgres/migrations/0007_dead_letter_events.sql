CREATE TABLE IF NOT EXISTS orion_operation.outbox_dead_letters (
    id              UUID PRIMARY KEY,
    aggregate_type  TEXT NOT NULL,
    aggregate_id    TEXT NOT NULL,
    subject         TEXT NOT NULL,
    payload         JSONB NOT NULL,
    error           TEXT NOT NULL,
    attempts        INT NOT NULL,
    first_seen_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

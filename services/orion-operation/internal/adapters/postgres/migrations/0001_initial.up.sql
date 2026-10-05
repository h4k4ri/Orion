-- 0001_initial.up.sql
-- Operations table: snapshot of current operation state

CREATE TABLE IF NOT EXISTS operation_operations (
    id              UUID PRIMARY KEY,
    resource_type   TEXT NOT NULL,
    resource_id     UUID NOT NULL,
    project_id      UUID NOT NULL,
    request_id      UUID,
    operation_type  TEXT NOT NULL,
    state           TEXT NOT NULL DEFAULT 'PENDING',
    current_step    TEXT NOT NULL DEFAULT '',
    attempt         INT NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at      TIMESTAMPTZ,
    finished_at     TIMESTAMPTZ,
    error_code      TEXT,
    error_message   TEXT
);

CREATE INDEX IF NOT EXISTS idx_operation_operations_resource
    ON operation_operations(resource_type, resource_id);
CREATE INDEX IF NOT EXISTS idx_operation_operations_project
    ON operation_operations(project_id);
CREATE INDEX IF NOT EXISTS idx_operation_operations_state
    ON operation_operations(state);
CREATE INDEX IF NOT EXISTS idx_operation_operations_request
    ON operation_operations(request_id);

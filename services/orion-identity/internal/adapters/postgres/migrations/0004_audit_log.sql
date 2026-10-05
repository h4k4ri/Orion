CREATE TABLE IF NOT EXISTS orion_identity.audit_log (
    id              TEXT PRIMARY KEY,
    actor_id        TEXT,
    project_id      TEXT,
    action          TEXT NOT NULL,
    resource_type   TEXT,
    resource_id     TEXT,
    request_id      TEXT,
    metadata        JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_audit_log_created_at
    ON orion_identity.audit_log (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_log_actor_id
    ON orion_identity.audit_log (actor_id);
CREATE INDEX IF NOT EXISTS idx_audit_log_resource
    ON orion_identity.audit_log (resource_type, resource_id);

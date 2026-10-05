ALTER TABLE orion_identity.audit_log
    ADD COLUMN IF NOT EXISTS operation_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS source_ip TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS result TEXT NOT NULL DEFAULT 'accepted',
    ADD COLUMN IF NOT EXISTS before_state JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS after_state JSONB NOT NULL DEFAULT '{}'::jsonb;

CREATE OR REPLACE FUNCTION orion_identity.reject_audit_mutation()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'audit_log is append-only';
END;
$$;

DROP TRIGGER IF EXISTS audit_log_immutable ON orion_identity.audit_log;
CREATE TRIGGER audit_log_immutable
    BEFORE UPDATE OR DELETE ON orion_identity.audit_log
    FOR EACH ROW EXECUTE FUNCTION orion_identity.reject_audit_mutation();

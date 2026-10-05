-- Orion Plugin Protocol v1.0 State Store
-- PostgreSQL Schema

-- Enable UUID extension
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- ============================================================
-- RESOURCES
-- ============================================================

CREATE TABLE resources (
    id TEXT PRIMARY KEY,                    -- Orion global ID (e.g., orion-volume-01HZX...)
    tenant_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    kind TEXT NOT NULL,                    -- e.g., orion.io/storage.volume
    version TEXT NOT NULL,                  -- e.g., v1
    provider_id TEXT NOT NULL,
    external_id TEXT,                       -- provider-specific ID (opaque to Orion)
    state TEXT NOT NULL DEFAULT 'PENDING',

    desired_spec JSONB,
    actual_state JSONB,

    generation BIGINT NOT NULL DEFAULT 0,
    observed_generation BIGINT NOT NULL DEFAULT 0,
    observed_at TIMESTAMPTZ,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ,

    CONSTRAINT valid_state CHECK (state IN (
        'PENDING', 'PROVISIONING', 'AVAILABLE', 'UPDATING',
        'DELETING', 'DELETED', 'ERROR', 'IMPORTING'
    ))
);

CREATE INDEX idx_resources_tenant ON resources(tenant_id);
CREATE INDEX idx_resources_project ON resources(project_id);
CREATE INDEX idx_resources_kind ON resources(kind);
CREATE INDEX idx_resources_provider ON resources(provider_id);
CREATE INDEX idx_resources_state ON resources(state);

-- ============================================================
-- PROVIDERS
-- ============================================================

CREATE TABLE providers (
    provider_id TEXT PRIMARY KEY,
    plugin_id TEXT NOT NULL,
    name TEXT NOT NULL,
    endpoint TEXT NOT NULL,

    config JSONB NOT NULL DEFAULT '{}',
    effective_capabilities JSONB NOT NULL DEFAULT '{}',

    administrative_state TEXT NOT NULL DEFAULT 'ENABLED',
    health_state TEXT NOT NULL DEFAULT 'UNKNOWN',

    generation BIGINT NOT NULL DEFAULT 0,
    observed_generation BIGINT NOT NULL DEFAULT 0,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT valid_admin_state CHECK (administrative_state IN ('ENABLED', 'DISABLED', 'DRAINING')),
    CONSTRAINT valid_health_state CHECK (health_state IN ('HEALTHY', 'DEGRADED', 'UNHEALTHY', 'UNKNOWN'))
);

CREATE INDEX idx_providers_plugin ON providers(plugin_id);
CREATE INDEX idx_providers_endpoint ON providers(endpoint);

-- ============================================================
-- PLUGINS
-- ============================================================

CREATE TABLE plugins (
    plugin_id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    version TEXT NOT NULL,
    vendor TEXT,
    description TEXT,

    manifest JSONB NOT NULL,
    api_key_hash TEXT,

    registered_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_heartbeat_at TIMESTAMPTZ
);

CREATE INDEX idx_plugins_name ON plugins(name);
CREATE INDEX idx_plugins_vendor ON plugins(vendor);

-- ============================================================
-- OPERATIONS
-- ============================================================

CREATE TABLE operations (
    operation_id TEXT PRIMARY KEY,
    request_id TEXT,
    idempotency_key TEXT,

    tenant_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    provider_id TEXT NOT NULL,

    resource TEXT NOT NULL,
    resource_version TEXT NOT NULL,
    operation_name TEXT NOT NULL,

    state TEXT NOT NULL DEFAULT 'PENDING',
    attempt INT NOT NULL DEFAULT 1,
    max_attempts INT NOT NULL DEFAULT 3,

    error_code TEXT,
    error_message TEXT,

    started_at TIMESTAMPTZ,
    deadline_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT valid_operation_state CHECK (state IN (
        'PENDING', 'RUNNING', 'SUCCEEDED', 'FAILED', 'CANCELLING', 'CANCELLED'
    ))
);

CREATE INDEX idx_operations_tenant ON operations(tenant_id);
CREATE INDEX idx_operations_resource ON operations(resource);
CREATE INDEX idx_operations_provider ON operations(provider_id);
CREATE INDEX idx_operations_state ON operations(state);
CREATE UNIQUE INDEX idx_operations_idempotency ON operations(idempotency_key) WHERE idempotency_key IS NOT NULL;

-- ============================================================
-- RESOURCE RELATIONSHIPS
-- ============================================================

CREATE TABLE resource_relationships (
    id TEXT PRIMARY KEY,
    relationship_kind TEXT NOT NULL,         -- e.g., orion.io/storage.attachment
    tenant_id TEXT NOT NULL,
    project_id TEXT NOT NULL,

    source_resource_id TEXT NOT NULL,
    target_resource_id TEXT NOT NULL,

    state TEXT NOT NULL DEFAULT 'PENDING',
    config JSONB NOT NULL DEFAULT '{}',

    generation BIGINT NOT NULL DEFAULT 0,
    observed_generation BIGINT NOT NULL DEFAULT 0,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT valid_relationship_state CHECK (state IN (
        'PENDING', 'PREPARING', 'ATTACHING', 'ACTIVE',
        'DETACHING', 'COMPENSATING', 'DELETED', 'ERROR'
    ))
);

CREATE INDEX idx_relationships_kind ON resource_relationships(relationship_kind);
CREATE INDEX idx_relationships_source ON resource_relationships(source_resource_id);
CREATE INDEX idx_relationships_target ON resource_relationships(target_resource_id);
CREATE INDEX idx_relationships_state ON resource_relationships(state);

-- ============================================================
-- RELATIONSHIP EXECUTIONS (Saga)
-- ============================================================

CREATE TABLE relationship_executions (
    execution_id TEXT PRIMARY KEY,
    relationship_id TEXT NOT NULL REFERENCES resource_relationships(id),

    operation TEXT NOT NULL,               -- attach or detach
    state TEXT NOT NULL DEFAULT 'PENDING',

    error_code TEXT,
    error_message TEXT,

    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ,

    CONSTRAINT valid_execution_state CHECK (state IN (
        'PENDING', 'RUNNING', 'SUCCEEDED', 'FAILED', 'CANCELLING', 'CANCELLED'
    ))
);

CREATE INDEX idx_executions_relationship ON relationship_executions(relationship_id);
CREATE INDEX idx_executions_state ON relationship_executions(state);

-- ============================================================
-- SAGA STEPS
-- ============================================================

CREATE TABLE saga_steps (
    step_id TEXT PRIMARY KEY,
    execution_id TEXT NOT NULL REFERENCES relationship_executions(execution_id),

    step_index INT NOT NULL,
    role TEXT NOT NULL,                   -- source or target
    operation TEXT NOT NULL,             -- prepare, attach, detach, release

    input JSONB,
    output JSONB,

    state TEXT NOT NULL DEFAULT 'PENDING',
    compensation_required BOOLEAN NOT NULL DEFAULT FALSE,
    compensation_input JSONB,

    error_code TEXT,
    error_message TEXT,

    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,

    CONSTRAINT valid_step_state CHECK (state IN (
        'PENDING', 'RUNNING', 'SUCCEEDED', 'FAILED',
        'COMPENSATING', 'COMPENSATED', 'COMPENSATION_FAILED'
    ))
);

CREATE INDEX idx_steps_execution ON saga_steps(execution_id);
CREATE INDEX idx_steps_state ON saga_steps(state);

-- ============================================================
-- UPDATED AT TRIGGER
-- ============================================================

CREATE OR REPLACE FUNCTION update_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER resources_updated_at
    BEFORE UPDATE ON resources
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

CREATE TRIGGER providers_updated_at
    BEFORE UPDATE ON providers
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

CREATE TRIGGER relationships_updated_at
    BEFORE UPDATE ON resource_relationships
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

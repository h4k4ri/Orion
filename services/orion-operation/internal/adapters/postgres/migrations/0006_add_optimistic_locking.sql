ALTER TABLE orion_operation.operation_operations ADD COLUMN IF NOT EXISTS version BIGINT NOT NULL DEFAULT 1;

CREATE INDEX IF NOT EXISTS idx_operation_operations_version ON orion_operation.operation_operations(version);

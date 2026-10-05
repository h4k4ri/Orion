CREATE TABLE IF NOT EXISTS processed_commands (
	message_id    UUID PRIMARY KEY,
	operation_id UUID,
	handler      TEXT NOT NULL,
	processed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	result       JSONB,
	expires_at   TIMESTAMPTZ NOT NULL DEFAULT NOW() + INTERVAL '7 days'
);

CREATE INDEX IF NOT EXISTS idx_processed_commands_operation_id ON processed_commands(operation_id);
CREATE INDEX IF NOT EXISTS idx_processed_commands_expires_at ON processed_commands(expires_at);

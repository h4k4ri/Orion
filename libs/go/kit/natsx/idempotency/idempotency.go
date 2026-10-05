package idempotency

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	db    *pgxpool.Pool
	ttl   time.Duration
	table string
}

type StoreOption func(*Store)

func tableSchema(table string) string {
	if idx := strings.IndexByte(table, '.'); idx > 0 {
		return table[:idx]
	}
	return "public"
}

func WithTTL(ttl time.Duration) StoreOption {
	return func(s *Store) {
		s.ttl = ttl
	}
}

func New(db *pgxpool.Pool, opts ...StoreOption) *Store {
	s := &Store{db: db, ttl: 7 * 24 * time.Hour, table: "processed_commands"}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// NewForSchema keeps each service's idempotency records in its owned schema.
// The schema is service configuration, not user input, and is validated before
// being interpolated into SQL identifiers.
func NewForSchema(db *pgxpool.Pool, schema string, opts ...StoreOption) (*Store, error) {
	if schema == "" {
		return nil, fmt.Errorf("schema is required")
	}
	for _, char := range schema {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '_' {
			return nil, fmt.Errorf("invalid schema %q", schema)
		}
	}
	s := &Store{db: db, ttl: 7 * 24 * time.Hour, table: schema + ".processed_commands"}
	for _, opt := range opts {
		opt(s)
	}
	return s, nil
}

func (s *Store) Migrate(ctx context.Context) error {
	schema := fmt.Sprintf(`
	CREATE SCHEMA IF NOT EXISTS %s;
	CREATE TABLE IF NOT EXISTS %s (
		message_id    TEXT PRIMARY KEY,
		operation_id TEXT,
		handler      TEXT NOT NULL,
		processed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		result       JSONB,
		expires_at   TIMESTAMPTZ NOT NULL DEFAULT NOW() + INTERVAL '7 days'
	);

	CREATE INDEX IF NOT EXISTS idx_processed_commands_operation_id ON %s(operation_id);
	CREATE INDEX IF NOT EXISTS idx_processed_commands_expires_at ON %s(expires_at);
	`, tableSchema(s.table), s.table, s.table, s.table)
	_, err := s.db.Exec(ctx, schema)
	return err
}

// Claim atomically reserves a message for processing. It returns false when an
// unexpired claim already exists, including when another consumer won the race.
func (s *Store) Claim(ctx context.Context, messageID, operationID, handler string) (bool, error) {
	var claimed string
	err := s.db.QueryRow(ctx, `
	INSERT INTO `+s.table+` (message_id, operation_id, handler, result, expires_at)
		VALUES ($1, $2, $3, NULL, NOW() + $4::interval)
		ON CONFLICT (message_id) DO UPDATE SET
			operation_id = EXCLUDED.operation_id,
			handler = EXCLUDED.handler,
			processed_at = NOW(),
			expires_at = EXCLUDED.expires_at
		WHERE `+s.table+`.expires_at <= NOW()
		RETURNING message_id
	`, messageID, operationID, handler, s.ttl.String()).Scan(&claimed)
	if err == pgx.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("claim processed: %w", err)
	}
	return claimed != "", nil
}

type ProcessedCommand struct {
	MessageID   string
	OperationID string
	Handler     string
	ProcessedAt time.Time
	Result      []byte
}

func (s *Store) IsProcessed(ctx context.Context, messageID string) (bool, error) {
	var exists bool
	err := s.db.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM "+s.table+" WHERE message_id = $1 AND expires_at > NOW())",
		messageID,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check processed: %w", err)
	}
	return exists, nil
}

func (s *Store) MarkProcessed(ctx context.Context, messageID, operationID, handler string, result []byte) error {
	_, err := s.db.Exec(ctx, `
		INSERT INTO `+s.table+` (message_id, operation_id, handler, result, expires_at)
		VALUES ($1, $2, $3, $4, NOW() + $5::interval)
		ON CONFLICT (message_id) DO UPDATE SET
			processed_at = NOW(),
			result = EXCLUDED.result
	`, messageID, operationID, handler, result, s.ttl.String())
	if err != nil {
		return fmt.Errorf("mark processed: %w", err)
	}
	return nil
}

func (s *Store) Get(ctx context.Context, messageID string) (*ProcessedCommand, error) {
	var cmd ProcessedCommand
	err := s.db.QueryRow(ctx, `
		SELECT message_id, operation_id, handler, processed_at, result
		FROM `+s.table+`
		WHERE message_id = $1 AND expires_at > NOW()
	`, messageID).Scan(
		&cmd.MessageID, &cmd.OperationID, &cmd.Handler, &cmd.ProcessedAt, &cmd.Result,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get processed: %w", err)
	}
	return &cmd, nil
}

func (s *Store) Cleanup(ctx context.Context) (int64, error) {
	result, err := s.db.Exec(ctx, "DELETE FROM "+s.table+" WHERE expires_at <= NOW()")
	if err != nil {
		return 0, fmt.Errorf("cleanup: %w", err)
	}
	return result.RowsAffected(), nil
}

type Manager struct {
	store *Store
}

func NewManager(db *pgxpool.Pool) *Manager {
	return &Manager{store: New(db)}
}

func NewManagerForSchema(db *pgxpool.Pool, schema string) (*Manager, error) {
	store, err := NewForSchema(db, schema)
	if err != nil {
		return nil, err
	}
	return &Manager{store: store}, nil
}

func (m *Manager) Store() *Store {
	return m.store
}

func (m *Manager) InitSchema(ctx context.Context) error {
	return m.store.Migrate(ctx)
}

func (m *Manager) CheckAndMark(ctx context.Context, messageID, operationID, handler string) (bool, error) {
	if messageID == "" {
		return false, fmt.Errorf("message id is required")
	}
	claimed, err := m.store.Claim(ctx, messageID, operationID, handler)
	if err != nil {
		return false, err
	}
	return !claimed, nil
}

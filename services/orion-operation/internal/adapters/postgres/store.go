package postgres

import (
	"context"
	"embed"
	"errors"
	"time"

	kitpg "github.com/horizon/orion/libs/go/kit/postgres"
	"github.com/horizon/orion/services/orion-operation/internal/domain"
	"github.com/horizon/orion/services/orion-operation/internal/ports"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

var errOpNotFound = errors.New("operation not found")

//go:embed migrations/*.sql
var migrationsFS embed.FS

type Store struct {
	pool   *pgxpool.Pool
	schema string
}

func (s *Store) q(t string) string {
	return s.schema + "." + t
}

func New(ctx context.Context, pool *pgxpool.Pool) (*Store, error) {
	if err := kitpg.ApplyMigrations(ctx, pool, "orion-operation", migrationsFS, "migrations"); err != nil {
		return nil, err
	}
	return &Store{pool: pool, schema: "orion_operation"}, nil
}

func (s *Store) CreateOperation(ctx context.Context, op domain.Operation) (bool, error) {
	result, err := s.pool.Exec(ctx, `
		INSERT INTO orion_operation.operation_operations
		    (id, resource_type, resource_id, project_id, request_id,
		     operation_type, state, current_step, attempt, version,
		     created_at, started_at, finished_at, error_code, error_message)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		ON CONFLICT (id) DO NOTHING`,
		op.ID, op.ResourceType, op.ResourceID, op.ProjectID, op.RequestID,
		op.OperationType, op.State, op.CurrentStep, op.Attempt, 1,
		op.CreatedAt, op.StartedAt, op.FinishedAt, op.ErrorCode, op.ErrorMessage,
	)
	return result.RowsAffected() == 1, err
}

func (s *Store) GetOperation(ctx context.Context, id string) (domain.Operation, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, resource_type, resource_id, project_id, request_id,
		       operation_type, state, current_step, attempt, version,
		       created_at, started_at, finished_at, error_code, error_message
		FROM orion_operation.operation_operations WHERE id = $1`, id)

	var op domain.Operation
	var startedAt, finishedAt pgtype.Timestamptz
	var errorCode, errorMessage *string
	err := row.Scan(
		&op.ID, &op.ResourceType, &op.ResourceID, &op.ProjectID, &op.RequestID,
		&op.OperationType, &op.State, &op.CurrentStep, &op.Attempt, &op.Version,
		&op.CreatedAt, &startedAt, &finishedAt, &errorCode, &errorMessage,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Operation{}, ports.ErrOperationNotFound
	}
	if err != nil {
		return domain.Operation{}, err
	}
	if startedAt.Valid {
		op.StartedAt = &startedAt.Time
	}
	if finishedAt.Valid {
		op.FinishedAt = &finishedAt.Time
	}
	if errorCode != nil {
		op.ErrorCode = *errorCode
	}
	if errorMessage != nil {
		op.ErrorMessage = *errorMessage
	}
	return op, nil
}

func (s *Store) UpdateOperation(ctx context.Context, op domain.Operation) error {
	result, err := s.pool.Exec(ctx, `
		UPDATE orion_operation.operation_operations SET
		    state          = $2,
		    current_step   = $3,
		    attempt        = $4,
		    started_at     = $5,
		    finished_at    = $6,
		    error_code     = $7,
		    error_message  = $8,
		    version        = version + 1
		WHERE id = $1 AND version = $9`,
		op.ID, op.State, op.CurrentStep, op.Attempt,
		op.StartedAt, op.FinishedAt, op.ErrorCode, op.ErrorMessage,
		op.Version,
	)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ports.ErrOptimisticLockConflict
	}
	return nil
}

func (s *Store) ListOperations(ctx context.Context, projectID string) ([]domain.Operation, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, resource_type, resource_id, project_id, request_id,
		       operation_type, state, current_step, attempt, version,
		       created_at, started_at, finished_at, error_code, error_message
		FROM orion_operation.operation_operations
		WHERE project_id = $1
		ORDER BY created_at DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ops []domain.Operation
	for rows.Next() {
		var op domain.Operation
		var startedAt, finishedAt pgtype.Timestamptz
		var errorCode, errorMessage *string
		err := rows.Scan(
			&op.ID, &op.ResourceType, &op.ResourceID, &op.ProjectID, &op.RequestID,
			&op.OperationType, &op.State, &op.CurrentStep, &op.Attempt, &op.Version,
			&op.CreatedAt, &startedAt, &finishedAt, &errorCode, &errorMessage,
		)
		if err != nil {
			return nil, err
		}
		if startedAt.Valid {
			op.StartedAt = &startedAt.Time
		}
		if finishedAt.Valid {
			op.FinishedAt = &finishedAt.Time
		}
		if errorCode != nil {
			op.ErrorCode = *errorCode
		}
		if errorMessage != nil {
			op.ErrorMessage = *errorMessage
		}
		ops = append(ops, op)
	}
	return ops, rows.Err()
}

func (s *Store) AppendEvent(ctx context.Context, ev domain.OperationEvent) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, ev.OperationID); err != nil {
		return err
	}
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(MAX(sequence), 0) + 1
		FROM orion_operation.operation_operation_events
		WHERE operation_id = $1`, ev.OperationID).Scan(&ev.Sequence); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO orion_operation.operation_operation_events
		    (id, operation_id, sequence, event_type, from_state, to_state, step, payload, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		ev.ID, ev.OperationID, ev.Sequence, ev.EventType,
		ev.FromState, ev.ToState, ev.Step, ev.Payload, ev.CreatedAt,
	); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) GetNextSequence(ctx context.Context, operationID string) (int64, error) {
	var seq int64
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(MAX(sequence), 0) + 1
		FROM orion_operation.operation_operation_events
		WHERE operation_id = $1`, operationID,
	).Scan(&seq)
	return seq, err
}

func (s *Store) GetEvents(ctx context.Context, operationID string) ([]domain.OperationEvent, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, operation_id, sequence, event_type, from_state, to_state, step, payload, created_at
		FROM orion_operation.operation_operation_events
		WHERE operation_id = $1
		ORDER BY sequence ASC`, operationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []domain.OperationEvent
	for rows.Next() {
		var ev domain.OperationEvent
		var fromState, toState, step *string
		if err := rows.Scan(&ev.ID, &ev.OperationID, &ev.Sequence, &ev.EventType, &fromState, &toState, &step, &ev.Payload, &ev.CreatedAt); err != nil {
			return nil, err
		}
		if fromState != nil {
			ev.FromState = *fromState
		}
		if toState != nil {
			ev.ToState = *toState
		}
		if step != nil {
			ev.Step = *step
		}
		events = append(events, ev)
	}
	return events, rows.Err()
}

func (s *Store) InsertOutboxEvent(ctx context.Context, ev ports.OutboxEvent) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO orion_operation.outbox_events
		    (id, aggregate_type, aggregate_id, subject, payload, trace_id)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		ev.ID, ev.AggregateType, ev.AggregateID, ev.Subject, ev.Payload, ev.TraceID,
	)
	return err
}

func (s *Store) TransitionWithOutbox(ctx context.Context, op domain.Operation, ev domain.OperationEvent, outbox ports.OutboxEvent) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, op.ID); err != nil {
		return err
	}

	var nextSeq int64
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(MAX(sequence), 0) + 1
		FROM orion_operation.operation_operation_events
		WHERE operation_id = $1`, op.ID).Scan(&nextSeq); err != nil {
		return err
	}
	ev.Sequence = nextSeq

	if _, err := tx.Exec(ctx, `
		INSERT INTO orion_operation.operation_operation_events
		    (id, operation_id, sequence, event_type, from_state, to_state, step, payload, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		ev.ID, ev.OperationID, ev.Sequence, ev.EventType,
		ev.FromState, ev.ToState, ev.Step, ev.Payload, ev.CreatedAt,
	); err != nil {
		return err
	}

	result, err := tx.Exec(ctx, `
		UPDATE orion_operation.operation_operations SET
		    state          = $2,
		    current_step   = $3,
		    attempt        = $4,
		    started_at     = $5,
		    finished_at    = $6,
		    error_code     = $7,
		    error_message  = $8,
		    version        = version + 1
		WHERE id = $1 AND version = $9`,
		op.ID, op.State, op.CurrentStep, op.Attempt,
		op.StartedAt, op.FinishedAt, op.ErrorCode, op.ErrorMessage,
		op.Version,
	)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ports.ErrOptimisticLockConflict
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO orion_operation.outbox_events
		    (id, aggregate_type, aggregate_id, subject, payload, trace_id)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		outbox.ID, outbox.AggregateType, outbox.AggregateID, outbox.Subject, outbox.Payload, outbox.TraceID,
	); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (s *Store) ListUnpublishedOutbox(ctx context.Context, limit int) ([]ports.OutboxEventRecord, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, aggregate_type, aggregate_id, subject, payload, COALESCE(trace_id, ''), attempts
		FROM orion_operation.outbox_events
		WHERE published_at IS NULL
		ORDER BY created_at ASC
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []ports.OutboxEventRecord
	for rows.Next() {
		var event ports.OutboxEventRecord
		if err := rows.Scan(&event.ID, &event.AggregateType, &event.AggregateID, &event.Subject, &event.Payload, &event.TraceID, &event.Attempts); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (s *Store) MarkOutboxPublished(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `UPDATE orion_operation.outbox_events SET published_at = $2, attempts = attempts + 1 WHERE id = $1 AND published_at IS NULL`, id, time.Now().UTC())
	return err
}

func (s *Store) MarkOutboxFailed(ctx context.Context, event ports.OutboxEventRecord, reason string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if event.Attempts+1 < 10 {
		if _, err := tx.Exec(ctx, `UPDATE orion_operation.outbox_events SET attempts = attempts + 1 WHERE id = $1 AND published_at IS NULL`, event.ID); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO orion_operation.outbox_dead_letters
		(id, aggregate_type, aggregate_id, subject, payload, error, attempts)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (id) DO UPDATE SET error = EXCLUDED.error, attempts = EXCLUDED.attempts, last_seen_at = NOW()`,
		event.ID, event.AggregateType, event.AggregateID, event.Subject, event.Payload, reason, event.Attempts+1); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE orion_operation.outbox_events SET attempts = attempts + 1, published_at = NOW() WHERE id = $1 AND published_at IS NULL`, event.ID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) TryAcquireOutboxLeader(ctx context.Context) (ports.LeaderRelease, bool, error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, false, err
	}
	var acquired bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtextextended('orion-operation-outbox-dispatcher', 0))`).Scan(&acquired); err != nil {
		conn.Release()
		return nil, false, err
	}
	if !acquired {
		conn.Release()
		return nil, false, nil
	}
	return func() {
		_, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock(hashtextextended('orion-operation-outbox-dispatcher', 0))`)
		conn.Release()
	}, true, nil
}

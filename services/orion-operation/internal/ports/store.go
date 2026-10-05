package ports

import (
	"context"
	"errors"

	"github.com/horizon/orion/services/orion-operation/internal/domain"
)

var (
	ErrOperationNotFound      = errors.New("operation not found")
	ErrOptimisticLockConflict = errors.New("optimistic lock conflict: operation was modified by another writer")
)

type OutboxEvent struct {
	ID            string
	AggregateType string
	AggregateID   string
	Subject       string
	Payload       []byte
	TraceID       string
}

type OutboxEventRecord struct {
	OutboxEvent
	Attempts int
}

type LeaderRelease func()

type OperationStore interface {
	CreateOperation(ctx context.Context, op domain.Operation) (bool, error)
	GetOperation(ctx context.Context, id string) (domain.Operation, error)
	UpdateOperation(ctx context.Context, op domain.Operation) error
	ListOperations(ctx context.Context, projectID string) ([]domain.Operation, error)
	AppendEvent(ctx context.Context, ev domain.OperationEvent) error
	GetNextSequence(ctx context.Context, operationID string) (int64, error)
	GetEvents(ctx context.Context, operationID string) ([]domain.OperationEvent, error)
	InsertOutboxEvent(ctx context.Context, ev OutboxEvent) error
	TransitionWithOutbox(ctx context.Context, op domain.Operation, ev domain.OperationEvent, outbox OutboxEvent) error
	ListUnpublishedOutbox(ctx context.Context, limit int) ([]OutboxEventRecord, error)
	MarkOutboxPublished(ctx context.Context, id string) error
	MarkOutboxFailed(ctx context.Context, event OutboxEventRecord, reason string) error
	TryAcquireOutboxLeader(ctx context.Context) (LeaderRelease, bool, error)
}

package ports

import (
	"context"

	"github.com/horizon/orion/core/domain"
)

type ResourceRepository interface {
	Create(ctx context.Context, r *domain.Resource) error
	Get(ctx context.Context, id string) (*domain.Resource, error)
	Update(ctx context.Context, r *domain.Resource) error
	Delete(ctx context.Context, id string) error
	List(ctx context.Context, tenantID, projectID string) ([]*domain.Resource, error)
	ListByKind(ctx context.Context, kind, version string) ([]*domain.Resource, error)
	ListAll(ctx context.Context) ([]*domain.Resource, error)
}

type OperationRepository interface {
	Create(ctx context.Context, op *domain.Operation) error
	Get(ctx context.Context, id string) (*domain.Operation, error)
	Update(ctx context.Context, op *domain.Operation) error
	ListByResource(ctx context.Context, resource string) ([]*domain.Operation, error)
	GetByIdempotencyKey(ctx context.Context, key string) (*domain.Operation, error)
}

type ProviderRepository interface {
	Create(ctx context.Context, p *domain.Provider) error
	Get(ctx context.Context, id string) (*domain.Provider, error)
	Update(ctx context.Context, p *domain.Provider) error
	Delete(ctx context.Context, id string) error
	List(ctx context.Context) ([]*domain.Provider, error)
	ListByPlugin(ctx context.Context, pluginID string) ([]*domain.Provider, error)
}

type RelationshipRepository interface {
	Create(ctx context.Context, r *domain.ResourceRelationship) error
	Get(ctx context.Context, id string) (*domain.ResourceRelationship, error)
	Update(ctx context.Context, r *domain.ResourceRelationship) error
	Delete(ctx context.Context, id string) error
	List(ctx context.Context, tenantID, projectID string) ([]*domain.ResourceRelationship, error)
}

type SagaExecutionRepository interface {
	CreateExecution(ctx context.Context, e *domain.RelationshipExecution) error
	GetExecution(ctx context.Context, id string) (*domain.RelationshipExecution, error)
	UpdateExecution(ctx context.Context, e *domain.RelationshipExecution) error

	CreateStep(ctx context.Context, s *domain.SagaStepExecution) error
	UpdateStep(ctx context.Context, s *domain.SagaStepExecution) error
	ListStepsByExecution(ctx context.Context, executionID string) ([]*domain.SagaStepExecution, error)
}

package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/horizon/orion/core/domain"
	"github.com/horizon/orion/core/ports"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(connString string) (*Store, error) {
	pool, err := pgxpool.New(context.Background(), connString)
	if err != nil {
		return nil, fmt.Errorf("failed to create pool: %w", err)
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() {
	s.pool.Close()
}

func (s *Store) ResourceRepository() ports.ResourceRepository {
	return &resourceRepository{pool: s.pool}
}

func (s *Store) OperationRepository() ports.OperationRepository {
	return &operationRepository{pool: s.pool}
}

func (s *Store) ProviderRepository() ports.ProviderRepository {
	return &providerRepository{pool: s.pool}
}

func (s *Store) RelationshipRepository() ports.RelationshipRepository {
	return &relationshipRepository{pool: s.pool}
}

func (s *Store) SagaExecutionRepository() ports.SagaExecutionRepository {
	return &sagaExecutionRepository{pool: s.pool}
}

type resourceRepository struct {
	pool *pgxpool.Pool
}

func (r *resourceRepository) Create(ctx context.Context, res *domain.Resource) error {
	query := `
		INSERT INTO resources (id, tenant_id, project_id, kind, version, provider_id, external_id, state,
			desired_spec, actual_state, generation, observed_generation, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	`
	_, err := r.pool.Exec(ctx, query,
		res.ID, res.TenantID, res.ProjectID, res.Kind, res.Version, res.ProviderID, res.ExternalID, res.State,
		res.DesiredSpec, res.ActualState, res.Generation, res.ObservedGeneration, res.CreatedAt, res.UpdatedAt,
	)
	return err
}

func (r *resourceRepository) Get(ctx context.Context, id string) (*domain.Resource, error) {
	query := `
		SELECT id, tenant_id, project_id, kind, version, provider_id, external_id, state,
			desired_spec, actual_state, generation, observed_generation, created_at, updated_at, observed_at, deleted_at
		FROM resources WHERE id = $1 AND deleted_at IS NULL
	`
	row := r.pool.QueryRow(ctx, query, id)
	return r.scanResource(row)
}

func (r *resourceRepository) Update(ctx context.Context, res *domain.Resource) error {
	query := `
		UPDATE resources SET
			state = $2, external_id = $3, desired_spec = $4, actual_state = $5,
			generation = $6, observed_generation = $7, observed_at = $8, updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL
	`
	_, err := r.pool.Exec(ctx, query,
		res.ID, res.State, res.ExternalID, res.DesiredSpec, res.ActualState,
		res.Generation, res.ObservedGeneration, res.ObservedAt,
	)
	return err
}

func (r *resourceRepository) Delete(ctx context.Context, id string) error {
	query := `UPDATE resources SET deleted_at = NOW(), state = 'DELETED' WHERE id = $1`
	_, err := r.pool.Exec(ctx, query, id)
	return err
}

func (r *resourceRepository) List(ctx context.Context, tenantID, projectID string) ([]*domain.Resource, error) {
	query := `
		SELECT id, tenant_id, project_id, kind, version, provider_id, external_id, state,
			desired_spec, actual_state, generation, observed_generation, created_at, updated_at, observed_at, deleted_at
		FROM resources
		WHERE tenant_id = $1 AND project_id = $2 AND deleted_at IS NULL
	`
	rows, err := r.pool.Query(ctx, query, tenantID, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return r.scanResources(rows)
}

func (r *resourceRepository) ListByKind(ctx context.Context, kind, version string) ([]*domain.Resource, error) {
	query := `
		SELECT id, tenant_id, project_id, kind, version, provider_id, external_id, state,
			desired_spec, actual_state, generation, observed_generation, created_at, updated_at, observed_at, deleted_at
		FROM resources
		WHERE kind = $1 AND version = $2 AND deleted_at IS NULL
	`
	rows, err := r.pool.Query(ctx, query, kind, version)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return r.scanResources(rows)
}

func (r *resourceRepository) ListAll(ctx context.Context) ([]*domain.Resource, error) {
	query := `
		SELECT id, tenant_id, project_id, kind, version, provider_id, external_id, state,
			desired_spec, actual_state, generation, observed_generation, created_at, updated_at, observed_at, deleted_at
		FROM resources
		WHERE deleted_at IS NULL
	`
	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return r.scanResources(rows)
}

func (r *resourceRepository) scanResource(row pgx.Row) (*domain.Resource, error) {
	var res domain.Resource
	var desiredSpec, actualState json.RawMessage
	err := row.Scan(
		&res.ID, &res.TenantID, &res.ProjectID, &res.Kind, &res.Version, &res.ProviderID,
		&res.ExternalID, &res.State, &desiredSpec, &actualState, &res.Generation,
		&res.ObservedGeneration, &res.CreatedAt, &res.UpdatedAt, &res.ObservedAt, &res.DeletedAt,
	)
	if err != nil {
		return nil, err
	}
	res.DesiredSpec = desiredSpec
	res.ActualState = actualState
	return &res, nil
}

func (r *resourceRepository) scanResources(rows pgx.Rows) ([]*domain.Resource, error) {
	var resources []*domain.Resource
	for rows.Next() {
		var res domain.Resource
		var desiredSpec, actualState json.RawMessage
		err := rows.Scan(
			&res.ID, &res.TenantID, &res.ProjectID, &res.Kind, &res.Version, &res.ProviderID,
			&res.ExternalID, &res.State, &desiredSpec, &actualState, &res.Generation,
			&res.ObservedGeneration, &res.CreatedAt, &res.UpdatedAt, &res.ObservedAt, &res.DeletedAt,
		)
		if err != nil {
			return nil, err
		}
		res.DesiredSpec = desiredSpec
		res.ActualState = actualState
		resources = append(resources, &res)
	}
	return resources, nil
}

type operationRepository struct {
	pool *pgxpool.Pool
}

func (o *operationRepository) Create(ctx context.Context, op *domain.Operation) error {
	query := `
		INSERT INTO operations (operation_id, request_id, idempotency_key, tenant_id, project_id, provider_id,
			resource, resource_version, operation_name, state, attempt, max_attempts, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`
	_, err := o.pool.Exec(ctx, query,
		op.OperationID, op.RequestID, op.IdempotencyKey, op.TenantID, op.ProjectID, op.ProviderID,
		op.Resource, op.ResourceVersion, op.OperationName, op.State, op.Attempt, op.MaxAttempts, op.CreatedAt,
	)
	return err
}

func (o *operationRepository) Get(ctx context.Context, id string) (*domain.Operation, error) {
	query := `
		SELECT operation_id, request_id, idempotency_key, tenant_id, project_id, provider_id,
			resource, resource_version, operation_name, state, attempt, max_attempts,
			error_code, error_message, started_at, deadline_at, completed_at, created_at
		FROM operations WHERE operation_id = $1
	`
	row := o.pool.QueryRow(ctx, query, id)
	var op domain.Operation
	err := row.Scan(
		&op.OperationID, &op.RequestID, &op.IdempotencyKey, &op.TenantID, &op.ProjectID, &op.ProviderID,
		&op.Resource, &op.ResourceVersion, &op.OperationName, &op.State, &op.Attempt, &op.MaxAttempts,
		&op.ErrorCode, &op.ErrorMessage, &op.StartedAt, &op.DeadlineAt, &op.CompletedAt, &op.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &op, nil
}

func (o *operationRepository) Update(ctx context.Context, op *domain.Operation) error {
	query := `
		UPDATE operations SET
			state = $2, attempt = $3, error_code = $4, error_message = $5,
			started_at = $6, completed_at = $7
		WHERE operation_id = $1
	`
	_, err := o.pool.Exec(ctx, query,
		op.OperationID, op.State, op.Attempt, op.ErrorCode, op.ErrorMessage, op.StartedAt, op.CompletedAt,
	)
	return err
}

func (o *operationRepository) ListByResource(ctx context.Context, resource string) ([]*domain.Operation, error) {
	query := `
		SELECT operation_id, request_id, idempotency_key, tenant_id, project_id, provider_id,
			resource, resource_version, operation_name, state, attempt, max_attempts,
			error_code, error_message, started_at, deadline_at, completed_at, created_at
		FROM operations WHERE resource = $1
	`
	rows, err := o.pool.Query(ctx, query, resource)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ops []*domain.Operation
	for rows.Next() {
		var op domain.Operation
		err := rows.Scan(
			&op.OperationID, &op.RequestID, &op.IdempotencyKey, &op.TenantID, &op.ProjectID, &op.ProviderID,
			&op.Resource, &op.ResourceVersion, &op.OperationName, &op.State, &op.Attempt, &op.MaxAttempts,
			&op.ErrorCode, &op.ErrorMessage, &op.StartedAt, &op.DeadlineAt, &op.CompletedAt, &op.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		ops = append(ops, &op)
	}
	return ops, nil
}

func (o *operationRepository) GetByIdempotencyKey(ctx context.Context, key string) (*domain.Operation, error) {
	query := `
		SELECT operation_id, request_id, idempotency_key, tenant_id, project_id, provider_id,
			resource, resource_version, operation_name, state, attempt, max_attempts,
			error_code, error_message, started_at, deadline_at, completed_at, created_at
		FROM operations WHERE idempotency_key = $1
	`
	row := o.pool.QueryRow(ctx, query, key)
	var op domain.Operation
	err := row.Scan(
		&op.OperationID, &op.RequestID, &op.IdempotencyKey, &op.TenantID, &op.ProjectID, &op.ProviderID,
		&op.Resource, &op.ResourceVersion, &op.OperationName, &op.State, &op.Attempt, &op.MaxAttempts,
		&op.ErrorCode, &op.ErrorMessage, &op.StartedAt, &op.DeadlineAt, &op.CompletedAt, &op.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &op, nil
}

type providerRepository struct {
	pool *pgxpool.Pool
}

func (p *providerRepository) Create(ctx context.Context, prov *domain.Provider) error {
	query := `
		INSERT INTO providers (provider_id, plugin_id, name, endpoint, config, effective_capabilities,
			administrative_state, health_state, generation, observed_generation, created_at, last_seen_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`
	_, err := p.pool.Exec(ctx, query,
		prov.ProviderID, prov.PluginID, prov.Name, prov.Endpoint, prov.Config, prov.EffectiveCapabilities,
		prov.AdministrativeState, prov.HealthState, prov.Generation, prov.ObservedGeneration, prov.CreatedAt, prov.LastSeenAt,
	)
	return err
}

func (p *providerRepository) Get(ctx context.Context, id string) (*domain.Provider, error) {
	query := `
		SELECT provider_id, plugin_id, name, endpoint, config, effective_capabilities,
			administrative_state, health_state, generation, observed_generation, created_at, last_seen_at
		FROM providers WHERE provider_id = $1
	`
	row := p.pool.QueryRow(ctx, query, id)
	var prov domain.Provider
	err := row.Scan(
		&prov.ProviderID, &prov.PluginID, &prov.Name, &prov.Endpoint, &prov.Config, &prov.EffectiveCapabilities,
		&prov.AdministrativeState, &prov.HealthState, &prov.Generation, &prov.ObservedGeneration, &prov.CreatedAt, &prov.LastSeenAt,
	)
	if err != nil {
		return nil, err
	}
	return &prov, nil
}

func (p *providerRepository) Update(ctx context.Context, prov *domain.Provider) error {
	query := `
		UPDATE providers SET
			name = $2, endpoint = $3, config = $4, effective_capabilities = $5,
			administrative_state = $6, health_state = $7, generation = $8,
			observed_generation = $9, last_seen_at = $10
		WHERE provider_id = $1
	`
	_, err := p.pool.Exec(ctx, query,
		prov.ProviderID, prov.Name, prov.Endpoint, prov.Config, prov.EffectiveCapabilities,
		prov.AdministrativeState, prov.HealthState, prov.Generation, prov.ObservedGeneration, prov.LastSeenAt,
	)
	return err
}

func (p *providerRepository) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM providers WHERE provider_id = $1`
	_, err := p.pool.Exec(ctx, query, id)
	return err
}

func (p *providerRepository) List(ctx context.Context) ([]*domain.Provider, error) {
	query := `
		SELECT provider_id, plugin_id, name, endpoint, config, effective_capabilities,
			administrative_state, health_state, generation, observed_generation, created_at, last_seen_at
		FROM providers
	`
	rows, err := p.pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var providers []*domain.Provider
	for rows.Next() {
		var prov domain.Provider
		err := rows.Scan(
			&prov.ProviderID, &prov.PluginID, &prov.Name, &prov.Endpoint, &prov.Config, &prov.EffectiveCapabilities,
			&prov.AdministrativeState, &prov.HealthState, &prov.Generation, &prov.ObservedGeneration, &prov.CreatedAt, &prov.LastSeenAt,
		)
		if err != nil {
			return nil, err
		}
		providers = append(providers, &prov)
	}
	return providers, nil
}

func (p *providerRepository) ListByPlugin(ctx context.Context, pluginID string) ([]*domain.Provider, error) {
	query := `
		SELECT provider_id, plugin_id, name, endpoint, config, effective_capabilities,
			administrative_state, health_state, generation, observed_generation, created_at, last_seen_at
		FROM providers WHERE plugin_id = $1
	`
	rows, err := p.pool.Query(ctx, query, pluginID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var providers []*domain.Provider
	for rows.Next() {
		var prov domain.Provider
		err := rows.Scan(
			&prov.ProviderID, &prov.PluginID, &prov.Name, &prov.Endpoint, &prov.Config, &prov.EffectiveCapabilities,
			&prov.AdministrativeState, &prov.HealthState, &prov.Generation, &prov.ObservedGeneration, &prov.CreatedAt, &prov.LastSeenAt,
		)
		if err != nil {
			return nil, err
		}
		providers = append(providers, &prov)
	}
	return providers, nil
}

type relationshipRepository struct {
	pool *pgxpool.Pool
}

func (r *relationshipRepository) Create(ctx context.Context, rel *domain.ResourceRelationship) error {
	query := `
		INSERT INTO resource_relationships (id, relationship_kind, tenant_id, project_id,
			source_resource_id, target_resource_id, state, config, generation, observed_generation, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`
	_, err := r.pool.Exec(ctx, query,
		rel.ID, rel.RelationshipKind, rel.TenantID, rel.ProjectID,
		rel.SourceResourceID, rel.TargetResourceID, rel.State, rel.Config,
		rel.Generation, rel.ObservedGeneration, rel.CreatedAt, rel.UpdatedAt,
	)
	return err
}

func (r *relationshipRepository) Get(ctx context.Context, id string) (*domain.ResourceRelationship, error) {
	query := `
		SELECT id, relationship_kind, tenant_id, project_id, source_resource_id, target_resource_id,
			state, config, generation, observed_generation, created_at, updated_at
		FROM resource_relationships WHERE id = $1
	`
	row := r.pool.QueryRow(ctx, query, id)
	var rel domain.ResourceRelationship
	err := row.Scan(
		&rel.ID, &rel.RelationshipKind, &rel.TenantID, &rel.ProjectID,
		&rel.SourceResourceID, &rel.TargetResourceID, &rel.State, &rel.Config,
		&rel.Generation, &rel.ObservedGeneration, &rel.CreatedAt, &rel.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &rel, nil
}

func (r *relationshipRepository) Update(ctx context.Context, rel *domain.ResourceRelationship) error {
	query := `
		UPDATE resource_relationships SET
			state = $2, config = $3, generation = $4, observed_generation = $5, updated_at = NOW()
		WHERE id = $1
	`
	_, err := r.pool.Exec(ctx, query, rel.ID, rel.State, rel.Config, rel.Generation, rel.ObservedGeneration)
	return err
}

func (r *relationshipRepository) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM resource_relationships WHERE id = $1`
	_, err := r.pool.Exec(ctx, query, id)
	return err
}

func (r *relationshipRepository) List(ctx context.Context, tenantID, projectID string) ([]*domain.ResourceRelationship, error) {
	query := `
		SELECT id, relationship_kind, tenant_id, project_id, source_resource_id, target_resource_id,
			state, config, generation, observed_generation, created_at, updated_at
		FROM resource_relationships WHERE tenant_id = $1 AND project_id = $2
	`
	rows, err := r.pool.Query(ctx, query, tenantID, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rels []*domain.ResourceRelationship
	for rows.Next() {
		var rel domain.ResourceRelationship
		err := rows.Scan(
			&rel.ID, &rel.RelationshipKind, &rel.TenantID, &rel.ProjectID,
			&rel.SourceResourceID, &rel.TargetResourceID, &rel.State, &rel.Config,
			&rel.Generation, &rel.ObservedGeneration, &rel.CreatedAt, &rel.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		rels = append(rels, &rel)
	}
	return rels, nil
}

type sagaExecutionRepository struct {
	pool *pgxpool.Pool
}

func (s *sagaExecutionRepository) CreateExecution(ctx context.Context, e *domain.RelationshipExecution) error {
	query := `
		INSERT INTO relationship_executions (execution_id, relationship_id, operation, state, started_at)
		VALUES ($1, $2, $3, $4, $5)
	`
	_, err := s.pool.Exec(ctx, query, e.ExecutionID, e.RelationshipID, e.Operation, e.State, e.StartedAt)
	return err
}

func (s *sagaExecutionRepository) GetExecution(ctx context.Context, id string) (*domain.RelationshipExecution, error) {
	query := `
		SELECT execution_id, relationship_id, operation, state, error_code, error_message, started_at, completed_at
		FROM relationship_executions WHERE execution_id = $1
	`
	row := s.pool.QueryRow(ctx, query, id)
	var e domain.RelationshipExecution
	err := row.Scan(&e.ExecutionID, &e.RelationshipID, &e.Operation, &e.State, &e.ErrorCode, &e.ErrorMessage, &e.StartedAt, &e.CompletedAt)
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func (s *sagaExecutionRepository) UpdateExecution(ctx context.Context, e *domain.RelationshipExecution) error {
	query := `
		UPDATE relationship_executions SET
			state = $2, error_code = $3, error_message = $4, completed_at = $5
		WHERE execution_id = $1
	`
	_, err := s.pool.Exec(ctx, query, e.ExecutionID, e.State, e.ErrorCode, e.ErrorMessage, e.CompletedAt)
	return err
}

func (s *sagaExecutionRepository) CreateStep(ctx context.Context, step *domain.SagaStepExecution) error {
	query := `
		INSERT INTO saga_steps (step_id, execution_id, step_index, role, operation, input, output, state,
			compensation_required, compensation_input, started_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`
	_, err := s.pool.Exec(ctx, query,
		step.StepID, step.ExecutionID, step.StepIndex, step.Role, step.Operation,
		step.Input, step.Output, step.State, step.CompensationRequired, step.CompensationInput, step.StartedAt,
	)
	return err
}

func (s *sagaExecutionRepository) UpdateStep(ctx context.Context, step *domain.SagaStepExecution) error {
	query := `
		UPDATE saga_steps SET
			output = $2, state = $3, compensation_required = $4, compensation_input = $5,
			error_code = $6, error_message = $7, completed_at = $8
		WHERE step_id = $1
	`
	_, err := s.pool.Exec(ctx, query,
		step.StepID, step.Output, step.State, step.CompensationRequired,
		step.CompensationInput, step.ErrorCode, step.ErrorMessage, step.CompletedAt,
	)
	return err
}

func (s *sagaExecutionRepository) ListStepsByExecution(ctx context.Context, executionID string) ([]*domain.SagaStepExecution, error) {
	query := `
		SELECT step_id, execution_id, step_index, role, operation, input, output, state,
			compensation_required, compensation_input, error_code, error_message, started_at, completed_at
		FROM saga_steps WHERE execution_id = $1 ORDER BY step_index
	`
	rows, err := s.pool.Query(ctx, query, executionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var steps []*domain.SagaStepExecution
	for rows.Next() {
		var step domain.SagaStepExecution
		err := rows.Scan(
			&step.StepID, &step.ExecutionID, &step.StepIndex, &step.Role, &step.Operation,
			&step.Input, &step.Output, &step.State, &step.CompensationRequired,
			&step.CompensationInput, &step.ErrorCode, &step.ErrorMessage, &step.StartedAt, &step.CompletedAt,
		)
		if err != nil {
			return nil, err
		}
		steps = append(steps, &step)
	}
	return steps, nil
}

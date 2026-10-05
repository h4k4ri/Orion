package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/horizon/orion/core/domain"
	"github.com/horizon/orion/core/ports"
)

type RelationshipService struct {
	relationshipRepo ports.RelationshipRepository
	resourceRepo     ports.ResourceRepository
	sagaRepo         ports.SagaExecutionRepository
	sagaExecutor     SagaExecutor
}

type SagaExecutor interface {
	AttachResources(ctx context.Context, relationshipID string) (*domain.RelationshipExecution, error)
}

func NewRelationshipService(
	relationshipRepo ports.RelationshipRepository,
	resourceRepo ports.ResourceRepository,
	sagaRepo ports.SagaExecutionRepository,
	sagaExecutor ...SagaExecutor,
) *RelationshipService {
	var executor SagaExecutor
	if len(sagaExecutor) > 0 {
		executor = sagaExecutor[0]
	}
	return &RelationshipService{
		relationshipRepo: relationshipRepo,
		resourceRepo:     resourceRepo,
		sagaRepo:         sagaRepo,
		sagaExecutor:     executor,
	}
}

type CreateRelationshipRequest struct {
	TenantID         string
	ProjectID        string
	RelationshipKind string
	SourceResourceID string
	TargetResourceID string
	Config           json.RawMessage
}

type RelationshipResponse struct {
	ID               string          `json:"id"`
	RelationshipKind string          `json:"relationshipKind"`
	TenantID         string          `json:"tenantId"`
	ProjectID        string          `json:"projectId"`
	SourceResourceID string          `json:"sourceResourceId"`
	TargetResourceID string          `json:"targetResourceId"`
	State            string          `json:"state"`
	Config           json.RawMessage `json:"config,omitempty"`
	Generation       int64           `json:"generation"`
	CreatedAt        time.Time       `json:"createdAt"`
	UpdatedAt        time.Time       `json:"updatedAt"`
}

func (s *RelationshipService) CreateRelationship(ctx context.Context, req *CreateRelationshipRequest) (*RelationshipResponse, error) {
	source, err := s.resourceRepo.Get(ctx, req.SourceResourceID)
	if err != nil {
		return nil, fmt.Errorf("source resource not found: %w", err)
	}

	target, err := s.resourceRepo.Get(ctx, req.TargetResourceID)
	if err != nil {
		return nil, fmt.Errorf("target resource not found: %w", err)
	}

	if source.State != domain.ResourceStateAvailable {
		return nil, fmt.Errorf("source resource must be AVAILABLE, got: %s", source.State)
	}
	if target.State != domain.ResourceStateAvailable {
		return nil, fmt.Errorf("target resource must be AVAILABLE, got: %s", target.State)
	}

	relationship := &domain.ResourceRelationship{
		ID:               fmt.Sprintf("rel-%s", uuid.New().String()[:8]),
		RelationshipKind: req.RelationshipKind,
		TenantID:         req.TenantID,
		ProjectID:        req.ProjectID,
		SourceResourceID: req.SourceResourceID,
		TargetResourceID: req.TargetResourceID,
		State:            domain.RelationshipStatePending,
		Config:           req.Config,
		Generation:       1,
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}

	if err := s.relationshipRepo.Create(ctx, relationship); err != nil {
		return nil, fmt.Errorf("failed to create relationship: %w", err)
	}

	return toRelationshipResponse(relationship), nil
}

func (s *RelationshipService) GetRelationship(ctx context.Context, id string) (*RelationshipResponse, error) {
	rel, err := s.relationshipRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return toRelationshipResponse(rel), nil
}

func (s *RelationshipService) Attach(ctx context.Context, relationshipID string) (*domain.RelationshipExecution, error) {
	if s.sagaExecutor == nil {
		return nil, fmt.Errorf("saga executor is not configured")
	}
	return s.sagaExecutor.AttachResources(ctx, relationshipID)

	/*
		rel, err := s.relationshipRepo.Get(ctx, relationshipID)
		if err != nil {
			return nil, fmt.Errorf("relationship not found: %w", err)
		}

		if rel.State != domain.RelationshipStatePending && rel.State != domain.RelationshipStateDetaching {
			return nil, fmt.Errorf("relationship must be PENDING or DETACHING to attach, got: %s", rel.State)
		}

		source, err := s.resourceRepo.Get(ctx, rel.SourceResourceID)
		if err != nil {
			return nil, fmt.Errorf("source resource not found: %w", err)
		}

		target, err := s.resourceRepo.Get(ctx, rel.TargetResourceID)
		if err != nil {
			return nil, fmt.Errorf("target resource not found: %w", err)
		}

		rel.State = domain.RelationshipStatePreparing
		rel.Generation++
		if err := s.relationshipRepo.Update(ctx, rel); err != nil {
			return nil, fmt.Errorf("failed to update relationship state: %w", err)
		}

		execution := &domain.RelationshipExecution{
			ExecutionID:   uuid.New().String(),
			RelationshipID: rel.ID,
			Operation:     "attach",
			State:         domain.RelationshipStatePreparing,
			StartedAt:     time.Now(),
		}

		if err := s.sagaRepo.CreateExecution(ctx, execution); err != nil {
			return nil, fmt.Errorf("failed to create execution: %w", err)
		}

		steps := []struct {
			idx    int
			role   string
			op     string
			input  json.RawMessage
		}{
			{0, "source", "prepare", toJSON(map[string]string{"resource_id": source.ExternalID})},
			{1, "target", "attach", toJSON(map[string]string{"resource_id": target.ExternalID})},
			{2, "source", "release", toJSON(map[string]string{"resource_id": source.ExternalID})},
		}

		for _, st := range steps {
			step := &domain.SagaStepExecution{
				StepID:               uuid.New().String(),
				ExecutionID:         execution.ExecutionID,
				StepIndex:           st.idx,
				Role:                st.role,
				Operation:           st.op,
				Input:               st.input,
				State:               domain.StepStatePending,
				CompensationRequired: st.op != "release",
			}
			if err := s.sagaRepo.CreateStep(ctx, step); err != nil {
				return nil, fmt.Errorf("failed to create step: %w", err)
			}
		}

		go s.runAttachmentSaga(context.Background(), execution, source, target)

		return execution, nil
	*/
}

func (s *RelationshipService) Detach(ctx context.Context, relationshipID string) (*domain.RelationshipExecution, error) {
	return nil, fmt.Errorf("detach saga executor is not configured")
	/*
		rel, err := s.relationshipRepo.Get(ctx, relationshipID)
		if err != nil {
			return nil, fmt.Errorf("relationship not found: %w", err)
		}

		if rel.State != domain.RelationshipStateActive {
			return nil, fmt.Errorf("relationship must be ACTIVE to detach, got: %s", rel.State)
		}

		source, err := s.resourceRepo.Get(ctx, rel.SourceResourceID)
		if err != nil {
			return nil, fmt.Errorf("source resource not found: %w", err)
		}

		target, err := s.resourceRepo.Get(ctx, rel.TargetResourceID)
		if err != nil {
			return nil, fmt.Errorf("target resource not found: %w", err)
		}

		rel.State = domain.RelationshipStateDetaching
		rel.Generation++
		if err := s.relationshipRepo.Update(ctx, rel); err != nil {
			return nil, fmt.Errorf("failed to update relationship state: %w", err)
		}

		execution := &domain.RelationshipExecution{
			ExecutionID:   uuid.New().String(),
			RelationshipID: rel.ID,
			Operation:     "detach",
			State:         domain.RelationshipStateDetaching,
			StartedAt:     time.Now(),
		}

		if err := s.sagaRepo.CreateExecution(ctx, execution); err != nil {
			return nil, fmt.Errorf("failed to create execution: %w", err)
		}

		steps := []struct {
			idx    int
			role   string
			op     string
			input  json.RawMessage
		}{
			{0, "source", "prepare", toJSON(map[string]string{"resource_id": source.ExternalID})},
			{1, "target", "detach", toJSON(map[string]string{"resource_id": target.ExternalID})},
			{2, "source", "release", toJSON(map[string]string{"resource_id": source.ExternalID})},
		}

		for _, st := range steps {
			step := &domain.SagaStepExecution{
				StepID:               uuid.New().String(),
				ExecutionID:         execution.ExecutionID,
				StepIndex:           st.idx,
				Role:                st.role,
				Operation:           st.op,
				Input:               st.input,
				State:               domain.StepStatePending,
				CompensationRequired: st.op != "release",
			}
			if err := s.sagaRepo.CreateStep(ctx, step); err != nil {
				return nil, fmt.Errorf("failed to create step: %w", err)
			}
		}

		go s.runDetachmentSaga(context.Background(), execution, source, target)

		return execution, nil
	*/
}

func (s *RelationshipService) runAttachmentSaga(ctx context.Context, exec *domain.RelationshipExecution, source, target *domain.Resource) {
	s.executeSagaSteps(ctx, exec, source, target)
}

func (s *RelationshipService) runDetachmentSaga(ctx context.Context, exec *domain.RelationshipExecution, source, target *domain.Resource) {
	s.executeSagaSteps(ctx, exec, source, target)
}

func (s *RelationshipService) executeSagaSteps(ctx context.Context, exec *domain.RelationshipExecution, source, target *domain.Resource) {
	steps, err := s.sagaRepo.ListStepsByExecution(ctx, exec.ExecutionID)
	if err != nil {
		log.Printf("Failed to list steps: %v", err)
		return
	}

	for _, step := range steps {
		step.State = domain.StepStateRunning
		started := time.Now()
		step.StartedAt = &started
		s.sagaRepo.UpdateStep(ctx, step)

		result := s.executeStep(ctx, step, source, target)
		if !result.Success {
			s.handleStepFailure(ctx, step, exec, result)
			return
		}

		step.State = domain.StepStateSucceeded
		step.Output = result.Output
		completed := time.Now()
		step.CompletedAt = &completed
		s.sagaRepo.UpdateStep(ctx, step)
	}

	exec.State = domain.RelationshipStateActive
	completed := time.Now()
	exec.CompletedAt = &completed
	s.sagaRepo.UpdateExecution(ctx, exec)

	rel, _ := s.relationshipRepo.Get(ctx, exec.RelationshipID)
	if rel != nil {
		rel.State = domain.RelationshipStateActive
		s.relationshipRepo.Update(ctx, rel)
	}
}

func (s *RelationshipService) executeStep(ctx context.Context, step *domain.SagaStepExecution, source, target *domain.Resource) *StepResult {
	return &StepResult{
		Success:   false,
		ErrorCode: "EXECUTOR_NOT_CONFIGURED",
		ErrorMsg:  "relationship service has no plugin executor configured",
	}
}

type StepResult struct {
	Success   bool
	Output    json.RawMessage
	ErrorCode string
	ErrorMsg  string
}

func (s *RelationshipService) handleStepFailure(ctx context.Context, step *domain.SagaStepExecution, exec *domain.RelationshipExecution, result *StepResult) {
	step.ErrorCode = result.ErrorCode
	step.ErrorMessage = result.ErrorMsg
	step.State = domain.StepStateFailed
	completed := time.Now()
	step.CompletedAt = &completed
	s.sagaRepo.UpdateStep(ctx, step)

	if step.CompensationRequired {
		s.runCompensation(ctx, step, exec)
		return
	}

	exec.State = domain.RelationshipStateError
	exec.ErrorCode = step.ErrorCode
	exec.ErrorMessage = step.ErrorMessage
	s.sagaRepo.UpdateExecution(ctx, exec)

	rel, _ := s.relationshipRepo.Get(ctx, exec.RelationshipID)
	if rel != nil {
		rel.State = domain.RelationshipStateError
		s.relationshipRepo.Update(ctx, rel)
	}
}

func (s *RelationshipService) runCompensation(ctx context.Context, failedStep *domain.SagaStepExecution, exec *domain.RelationshipExecution) {
	steps, err := s.sagaRepo.ListStepsByExecution(ctx, exec.ExecutionID)
	if err != nil {
		log.Printf("Failed to list steps for compensation: %v", err)
		return
	}

	for i := len(steps) - 1; i >= 0; i-- {
		step := steps[i]
		if step.StepIndex >= failedStep.StepIndex {
			continue
		}
		if !step.CompensationRequired || step.State != domain.StepStateSucceeded {
			continue
		}

		step.State = domain.StepStateCompensating
		s.sagaRepo.UpdateStep(ctx, step)

		compResult := s.executeCompensation(ctx, step)
		if !compResult.Success {
			step.State = domain.StepStateCompensationFailed
			step.ErrorCode = compResult.ErrorCode
			step.ErrorMessage = compResult.ErrorMsg
		} else {
			step.State = domain.StepStateCompensated
		}
		s.sagaRepo.UpdateStep(ctx, step)
	}

	exec.State = domain.RelationshipStateError
	s.sagaRepo.UpdateExecution(ctx, exec)

	rel, _ := s.relationshipRepo.Get(ctx, exec.RelationshipID)
	if rel != nil {
		rel.State = domain.RelationshipStateError
		s.relationshipRepo.Update(ctx, rel)
	}
}

func (s *RelationshipService) executeCompensation(ctx context.Context, step *domain.SagaStepExecution) *StepResult {
	return &StepResult{Success: false, ErrorCode: "EXECUTOR_NOT_CONFIGURED", ErrorMsg: "relationship service has no plugin executor configured"}
}

func (s *RelationshipService) ListRelationships(ctx context.Context, tenantID, projectID string) ([]*RelationshipResponse, error) {
	rels, err := s.relationshipRepo.List(ctx, tenantID, projectID)
	if err != nil {
		return nil, err
	}

	responses := make([]*RelationshipResponse, len(rels))
	for i, r := range rels {
		responses[i] = toRelationshipResponse(r)
	}
	return responses, nil
}

func toRelationshipResponse(r *domain.ResourceRelationship) *RelationshipResponse {
	return &RelationshipResponse{
		ID:               r.ID,
		RelationshipKind: r.RelationshipKind,
		TenantID:         r.TenantID,
		ProjectID:        r.ProjectID,
		SourceResourceID: r.SourceResourceID,
		TargetResourceID: r.TargetResourceID,
		State:            string(r.State),
		Config:           r.Config,
		Generation:       r.Generation,
		CreatedAt:        r.CreatedAt,
		UpdatedAt:        r.UpdatedAt,
	}
}

func toJSON(v interface{}) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

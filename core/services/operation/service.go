package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/horizon/orion/core/domain"
	"github.com/horizon/orion/core/ports"
)

type OperationService struct {
	operationRepo ports.OperationRepository
	resourceRepo ports.ResourceRepository
}

func NewOperationService(operationRepo ports.OperationRepository, resourceRepo ports.ResourceRepository) *OperationService {
	return &OperationService{
		operationRepo: operationRepo,
		resourceRepo:  resourceRepo,
	}
}

type CreateOperationRequest struct {
	RequestID     string
	IdempotencyKey string
	TenantID     string
	ProjectID    string
	ResourceID   string
	ProviderID   string
	OperationName string
	MaxAttempts  int
	Payload      []byte
}

func (s *OperationService) CreateOperation(ctx context.Context, req *CreateOperationRequest) (*domain.Operation, error) {
	if req.IdempotencyKey != "" {
		existing, err := s.operationRepo.GetByIdempotencyKey(ctx, req.IdempotencyKey)
		if err == nil && existing != nil {
			return existing, nil
		}
	}

	resource, err := s.resourceRepo.Get(ctx, req.ResourceID)
	if err != nil {
		return nil, fmt.Errorf("resource not found: %w", err)
	}

	op := &domain.Operation{
		OperationID:    uuid.New().String(),
		RequestID:     req.RequestID,
		IdempotencyKey: req.IdempotencyKey,
		TenantID:      req.TenantID,
		ProjectID:     req.ProjectID,
		Resource:      req.ResourceID,
		ResourceVersion: resource.Version,
		ProviderID:    req.ProviderID,
		OperationName: req.OperationName,
		State:         domain.OperationStatePending,
		Attempt:       1,
		MaxAttempts:   req.MaxAttempts,
		CreatedAt:     time.Now(),
	}

	if err := s.operationRepo.Create(ctx, op); err != nil {
		return nil, fmt.Errorf("failed to create operation: %w", err)
	}

	return op, nil
}

func (s *OperationService) GetOperation(ctx context.Context, operationID string) (*domain.Operation, error) {
	return s.operationRepo.Get(ctx, operationID)
}

func (s *OperationService) ListByResource(ctx context.Context, resourceID string) ([]*domain.Operation, error) {
	return s.operationRepo.ListByResource(ctx, resourceID)
}

func (s *OperationService) StartOperation(ctx context.Context, operationID string) (*domain.Operation, error) {
	op, err := s.operationRepo.Get(ctx, operationID)
	if err != nil {
		return nil, err
	}

	if op.State != domain.OperationStatePending {
		return nil, fmt.Errorf("operation must be PENDING, got: %s", op.State)
	}

	op.State = domain.OperationStateRunning
	started := time.Now()
	op.StartedAt = &started

	if err := s.operationRepo.Update(ctx, op); err != nil {
		return nil, fmt.Errorf("failed to update operation: %w", err)
	}

	return op, nil
}

func (s *OperationService) SucceedOperation(ctx context.Context, operationID string, result []byte) (*domain.Operation, error) {
	op, err := s.operationRepo.Get(ctx, operationID)
	if err != nil {
		return nil, err
	}

	op.State = domain.OperationStateSucceeded
	completed := time.Now()
	op.CompletedAt = &completed

	if err := s.operationRepo.Update(ctx, op); err != nil {
		return nil, fmt.Errorf("failed to update operation: %w", err)
	}

	return op, nil
}

func (s *OperationService) FailOperation(ctx context.Context, operationID, errorCode, errorMsg string) (*domain.Operation, error) {
	op, err := s.operationRepo.Get(ctx, operationID)
	if err != nil {
		return nil, err
	}

	op.Attempt++
	if op.Attempt > op.MaxAttempts {
		op.State = domain.OperationStateFailed
	} else {
		op.State = domain.OperationStatePending
		op.StartedAt = nil
	}
	op.ErrorCode = errorCode
	op.ErrorMessage = errorMsg
	completed := time.Now()
	op.CompletedAt = &completed

	if err := s.operationRepo.Update(ctx, op); err != nil {
		return nil, fmt.Errorf("failed to update operation: %w", err)
	}

	return op, nil
}

func (s *OperationService) CancelOperation(ctx context.Context, operationID string) (*domain.Operation, error) {
	op, err := s.operationRepo.Get(ctx, operationID)
	if err != nil {
		return nil, err
	}

	if op.State == domain.OperationStateSucceeded || op.State == domain.OperationStateFailed {
		return nil, fmt.Errorf("cannot cancel completed operation: %s", op.State)
	}

	op.State = domain.OperationStateCancelling
	if err := s.operationRepo.Update(ctx, op); err != nil {
		return nil, fmt.Errorf("failed to update operation: %w", err)
	}

	return op, nil
}

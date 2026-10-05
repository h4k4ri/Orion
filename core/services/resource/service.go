package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/horizon/orion/core/domain"
	"github.com/horizon/orion/core/ports"
	"github.com/horizon/orion/core/runtime"
)

type ResourceService struct {
	resourceRepo   ports.ResourceRepository
	providerRepo   ports.ProviderRepository
	runtime        *runtime.ResourceRuntime
}

func NewResourceService(
	resourceRepo ports.ResourceRepository,
	providerRepo ports.ProviderRepository,
	rt *runtime.ResourceRuntime,
) *ResourceService {
	return &ResourceService{
		resourceRepo: resourceRepo,
		providerRepo: providerRepo,
		runtime:     rt,
	}
}

type CreateResourceRequest struct {
	TenantID    string
	ProjectID   string
	Kind        string
	Version     string
	ProviderID  string
	DesiredSpec json.RawMessage
}

type ResourceResponse struct {
	ID           string          `json:"id"`
	TenantID     string          `json:"tenantId"`
	ProjectID    string          `json:"projectId"`
	Kind         string          `json:"kind"`
	Version      string          `json:"version"`
	ProviderID   string          `json:"providerId"`
	State        string          `json:"state"`
	DesiredSpec  json.RawMessage `json:"desiredSpec,omitempty"`
	ActualState  json.RawMessage `json:"actualState,omitempty"`
	Generation   int64           `json:"generation"`
	CreatedAt    time.Time       `json:"createdAt"`
	UpdatedAt    time.Time       `json:"updatedAt"`
}

func (s *ResourceService) CreateResource(ctx context.Context, req *CreateResourceRequest) (*ResourceResponse, error) {
	_, err := s.providerRepo.Get(ctx, req.ProviderID)
	if err != nil {
		return nil, fmt.Errorf("provider not found: %w", err)
	}

	resource := &domain.Resource{
		ID:           fmt.Sprintf("orion-%s", uuid.New().String()[:8]),
		TenantID:     req.TenantID,
		ProjectID:    req.ProjectID,
		Kind:         req.Kind,
		Version:      req.Version,
		ProviderID:   req.ProviderID,
		State:        domain.ResourceStatePending,
		DesiredSpec:  req.DesiredSpec,
		Generation:   1,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	if err := s.resourceRepo.Create(ctx, resource); err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	if err := s.runtime.StartResource(ctx, resource.ID, "create", req.DesiredSpec); err != nil {
		return nil, fmt.Errorf("failed to start resource operation: %w", err)
	}

	return toResourceResponse(resource), nil
}

func (s *ResourceService) GetResource(ctx context.Context, id string) (*ResourceResponse, error) {
	resource, err := s.resourceRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return toResourceResponse(resource), nil
}

func (s *ResourceService) DeleteResource(ctx context.Context, id string) error {
	resource, err := s.resourceRepo.Get(ctx, id)
	if err != nil {
		return err
	}

	if resource.State == domain.ResourceStateDeleting || resource.State == domain.ResourceStateDeleted {
		return fmt.Errorf("resource already being deleted or deleted")
	}

	resource.State = domain.ResourceStateDeleting
	resource.Generation++
	if err := s.resourceRepo.Update(ctx, resource); err != nil {
		return fmt.Errorf("failed to update resource state: %w", err)
	}

	return s.runtime.StartResource(ctx, resource.ID, "delete", nil)
}

func (s *ResourceService) ListResources(ctx context.Context, tenantID, projectID string) ([]*ResourceResponse, error) {
	resources, err := s.resourceRepo.List(ctx, tenantID, projectID)
	if err != nil {
		return nil, err
	}

	responses := make([]*ResourceResponse, len(resources))
	for i, r := range resources {
		responses[i] = toResourceResponse(r)
	}
	return responses, nil
}

func (s *ResourceService) UpdateResource(ctx context.Context, id string, desiredSpec json.RawMessage) (*ResourceResponse, error) {
	resource, err := s.resourceRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	if resource.State != domain.ResourceStateAvailable {
		return nil, fmt.Errorf("resource must be in AVAILABLE state to update, current: %s", resource.State)
	}

	resource.DesiredSpec = desiredSpec
	resource.State = domain.ResourceStateUpdating
	resource.Generation++

	if err := s.resourceRepo.Update(ctx, resource); err != nil {
		return nil, fmt.Errorf("failed to update resource: %w", err)
	}

	if err := s.runtime.StartResource(ctx, resource.ID, "update", desiredSpec); err != nil {
		return nil, fmt.Errorf("failed to start update operation: %w", err)
	}

	return toResourceResponse(resource), nil
}

func toResourceResponse(r *domain.Resource) *ResourceResponse {
	return &ResourceResponse{
		ID:          r.ID,
		TenantID:    r.TenantID,
		ProjectID:   r.ProjectID,
		Kind:        r.Kind,
		Version:     r.Version,
		ProviderID:  r.ProviderID,
		State:       string(r.State),
		DesiredSpec: r.DesiredSpec,
		ActualState: r.ActualState,
		Generation:  r.Generation,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
}

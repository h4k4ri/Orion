package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/horizon/orion/core/domain"
)

func (r *ResourceRuntime) ObserveResource(ctx context.Context, resourceID string) (*domain.Resource, error) {
	res, err := r.resourceRepo.Get(ctx, resourceID)
	if err != nil {
		return nil, fmt.Errorf("resource not found: %w", err)
	}

	result, err := r.executor.InvokeResource(ctx, res.ProviderID, "observe", json.RawMessage(`{"external_id": "`+res.ExternalID+`"}`))
	if err != nil {
		return nil, fmt.Errorf("observe failed: %w", err)
	}

	if !result.Success {
		return nil, fmt.Errorf("observe returned error: %s - %s", result.ErrorCode, result.ErrorMsg)
	}

	res.ActualState = result.Output
	now := time.Now()
	res.ObservedAt = &now
	res.ObservedGeneration = res.Generation

	if err := r.resourceRepo.Update(ctx, res); err != nil {
		return nil, fmt.Errorf("failed to update resource: %w", err)
	}

	return res, nil
}

type DiscoveredResource struct {
	ExternalID   string            `json:"external_id"`
	Kind         string            `json:"kind"`
	Version      string            `json:"version"`
	State        string            `json:"state"`
	ActualState  json.RawMessage   `json:"actual_state,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

func (r *ResourceRuntime) DiscoverUnmanaged(ctx context.Context, providerID string, kind, version string) ([]*DiscoveredResource, error) {
	result, err := r.executor.InvokeResource(ctx, providerID, "discover", json.RawMessage(fmt.Sprintf(`{"kind": "%s", "version": "%s"}`, kind, version)))
	if err != nil {
		return nil, fmt.Errorf("discover failed: %w", err)
	}

	if !result.Success {
		return nil, fmt.Errorf("discover returned error: %s - %s", result.ErrorCode, result.ErrorMsg)
	}

	var discovered []*DiscoveredResource
	if err := json.Unmarshal(result.Output, &discovered); err != nil {
		return nil, fmt.Errorf("failed to unmarshal discovered resources: %w", err)
	}

	return discovered, nil
}

func (r *ResourceRuntime) ImportResource(ctx context.Context, providerID, kind, version, externalID string, spec json.RawMessage) (*domain.Resource, error) {
	res := &domain.Resource{
		ID:           fmt.Sprintf("orion-%s", uuidNew()[:8]),
		ProviderID:   providerID,
		Kind:         kind,
		Version:      version,
		ExternalID:   externalID,
		State:        domain.ResourceStateImporting,
		DesiredSpec:  spec,
		Generation:   1,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	if err := r.resourceRepo.Create(ctx, res); err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	observeResult, err := r.executor.InvokeResource(ctx, providerID, "observe", json.RawMessage(fmt.Sprintf(`{"external_id": "%s"}`, externalID)))
	if err != nil {
		res.State = domain.ResourceStateError
		r.resourceRepo.Update(ctx, res)
		return nil, fmt.Errorf("observe failed: %w", err)
	}

	if !observeResult.Success {
		res.State = domain.ResourceStateError
		r.resourceRepo.Update(ctx, res)
		return nil, fmt.Errorf("observe returned error: %s - %s", observeResult.ErrorCode, observeResult.ErrorMsg)
	}

	res.State = domain.ResourceStateAvailable
	res.ActualState = observeResult.Output
	res.ObservedGeneration = res.Generation
	now := time.Now()
	res.ObservedAt = &now

	if err := r.resourceRepo.Update(ctx, res); err != nil {
		return nil, fmt.Errorf("failed to update imported resource: %w", err)
	}

	return res, nil
}

func uuidNew() string {
	return uuid.New().String()[:8]
}

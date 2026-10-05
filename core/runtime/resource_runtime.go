package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/horizon/orion/core/domain"
	"github.com/horizon/orion/core/ports"
)

type ResourceRuntime struct {
	mu              sync.RWMutex
	running         map[string]*RunningResource
	resourceRepo    ports.ResourceRepository
	executor        ResourceOperator
	pollingInterval time.Duration
}

type RunningResource struct {
	ResourceID string
	Operation  string
	Input      json.RawMessage
	StartedAt  time.Time
}

type ResourceOperator interface {
	ExecuteResourceOperation(ctx context.Context, resourceID, operationName string, input json.RawMessage) (*domain.Operation, error)
	InvokeResource(ctx context.Context, providerID, operation string, input json.RawMessage) (*InvokeResult, error)
}

type ResourceAwareOperator interface {
	InvokeResourceForResource(ctx context.Context, resource *domain.Resource, operation string, input json.RawMessage) (*InvokeResult, error)
}

type InvokeResult struct {
	Success   bool
	Output    json.RawMessage
	ErrorCode string
	ErrorMsg  string
}

func NewResourceRuntime(resourceRepo ports.ResourceRepository, executor ResourceOperator) *ResourceRuntime {
	return &ResourceRuntime{
		running:         make(map[string]*RunningResource),
		resourceRepo:    resourceRepo,
		executor:        executor,
		pollingInterval: 5 * time.Second,
	}
}

func (r *ResourceRuntime) StartResource(ctx context.Context, resourceID, operation string, input json.RawMessage) error {
	r.mu.Lock()
	if _, exists := r.running[resourceID]; exists {
		r.mu.Unlock()
		return fmt.Errorf("resource %s already has running operation", resourceID)
	}

	r.running[resourceID] = &RunningResource{
		ResourceID: resourceID,
		Operation:  operation,
		Input:      input,
		StartedAt:  time.Now(),
	}
	r.mu.Unlock()

	go r.runResourceOperation(ctx, resourceID, operation, input)

	return nil
}

func (r *ResourceRuntime) runResourceOperation(ctx context.Context, resourceID, operation string, input json.RawMessage) {
	defer func() {
		r.mu.Lock()
		delete(r.running, resourceID)
		r.mu.Unlock()
	}()

	res, err := r.resourceRepo.Get(ctx, resourceID)
	if err != nil {
		log.Printf("Resource %s not found: %v", resourceID, err)
		return
	}

	res.State = domain.ResourceStateProvisioning
	res.Generation++
	if err := r.resourceRepo.Update(ctx, res); err != nil {
		log.Printf("Failed to update resource state: %v", err)
		return
	}

	var result *InvokeResult
	if aware, ok := r.executor.(ResourceAwareOperator); ok {
		result, err = aware.InvokeResourceForResource(ctx, res, operation, input)
	} else {
		result, err = r.executor.InvokeResource(ctx, res.ProviderID, operation, input)
	}
	if err != nil {
		r.failResource(ctx, res, "INTERNAL_ERROR", err.Error())
		return
	}

	if !result.Success {
		r.failResource(ctx, res, result.ErrorCode, result.ErrorMsg)
		return
	}

	if operation == "delete" {
		res.State = domain.ResourceStateDeleted
		now := time.Now()
		res.DeletedAt = &now
	} else {
		res.State = domain.ResourceStateAvailable
		res.ActualState = result.Output
		res.ObservedGeneration = res.Generation
		now := time.Now()
		res.ObservedAt = &now
	}

	if err := r.resourceRepo.Update(ctx, res); err != nil {
		log.Printf("Failed to update resource: %v", err)
	}

	log.Printf("Resource %s operation %s completed successfully", resourceID, operation)
}

func (r *ResourceRuntime) failResource(ctx context.Context, res *domain.Resource, errorCode, errorMsg string) {
	res.State = domain.ResourceStateError
	res.Generation++

	if err := r.resourceRepo.Update(ctx, res); err != nil {
		log.Printf("Failed to update resource error state: %v", err)
	}

	log.Printf("Resource %s failed: %s - %s", res.ID, errorCode, errorMsg)
}

func (r *ResourceRuntime) StopResource(ctx context.Context, resourceID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	rr, exists := r.running[resourceID]
	if !exists {
		return fmt.Errorf("no running operation for resource %s", resourceID)
	}

	delete(r.running, resourceID)
	log.Printf("Stopped operation %s for resource %s (started at %s)", rr.Operation, resourceID, rr.StartedAt)
	return nil
}

func (r *ResourceRuntime) GetRunning(ctx context.Context, resourceID string) (*RunningResource, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	rr, exists := r.running[resourceID]
	return rr, exists
}

func (r *ResourceRuntime) ListRunning(ctx context.Context) []*RunningResource {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*RunningResource, 0, len(r.running))
	for _, rr := range r.running {
		result = append(result, rr)
	}
	return result
}

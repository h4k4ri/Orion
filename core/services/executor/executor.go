package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/horizon/orion/core/domain"
	"github.com/horizon/orion/core/ports"
	registry "github.com/horizon/orion/core/services/plugin_registry"
	"github.com/horizon/orion/core/services/provider"
)

type PluginExecutor struct {
	pluginRegistry   *registry.PluginRegistry
	providerRegistry *provider.Registry
	resourceRepo     ports.ResourceRepository
	operationRepo    ports.OperationRepository
	relationshipRepo ports.RelationshipRepository
	sagaRepo         ports.SagaExecutionRepository
}

func NewPluginExecutor(
	pr *registry.PluginRegistry,
	provReg *provider.Registry,
	resourceRepo ports.ResourceRepository,
	operationRepo ports.OperationRepository,
	relationshipRepo ports.RelationshipRepository,
	sagaRepo ports.SagaExecutionRepository,
) *PluginExecutor {
	return &PluginExecutor{
		pluginRegistry:   pr,
		providerRegistry: provReg,
		resourceRepo:     resourceRepo,
		operationRepo:    operationRepo,
		relationshipRepo: relationshipRepo,
		sagaRepo:         sagaRepo,
	}
}

type InvokeResult struct {
	Success   bool
	Output    json.RawMessage
	ErrorCode string
	ErrorMsg  string
}

func (e *PluginExecutor) InvokeResource(ctx context.Context, providerID, operation string, input json.RawMessage) (*InvokeResult, error) {
	return e.invokeResource(ctx, providerID, "", "v1", operation, input)
}

// InvokeResourceForResource preserves the resource kind from the control-plane
// object. Provider names are configuration labels and must never be used as
// protocol resource kinds.
func (e *PluginExecutor) InvokeResourceForResource(ctx context.Context, resource *domain.Resource, operation string, input json.RawMessage) (*InvokeResult, error) {
	return e.invokeResource(ctx, resource.ProviderID, resource.Kind, resource.Version, operation, input)
}

func (e *PluginExecutor) invokeResource(ctx context.Context, providerID, kind, version, operation string, input json.RawMessage) (*InvokeResult, error) {
	prov, err := e.providerRegistry.Get(ctx, providerID)
	if err != nil {
		return nil, fmt.Errorf("provider not found: %w", err)
	}

	plugin, err := e.pluginRegistry.GetPlugin(ctx, prov.PluginID)
	if err != nil {
		return nil, fmt.Errorf("plugin not found: %w", err)
	}

	if kind == "" {
		// Legacy callers (reconciliation adapters) do not yet pass the
		// resource object. Keep the path usable, while all ResourceRuntime
		// calls use InvokeResourceForResource above.
		kind = prov.Name
	}
	if version == "" {
		version = "v1"
	}

	req := &registry.InvokeRequest{
		Kind:       kind,
		Version:    version,
		ProviderID: providerID,
		Operation:  operation,
		Input:      input,
	}

	resp, err := plugin.Client.Invoke(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("invoke failed: %w", err)
	}

	return &InvokeResult{
		Success:   resp.Success,
		Output:    resp.Output,
		ErrorCode: getErrorCode(resp.Error),
		ErrorMsg:  getErrorMessage(resp.Error),
	}, nil
}

func (e *PluginExecutor) ExecuteResourceOperation(ctx context.Context, resourceID, operationName string, input json.RawMessage) (*domain.Operation, error) {
	resource, err := e.resourceRepo.Get(ctx, resourceID)
	if err != nil {
		return nil, fmt.Errorf("resource not found: %w", err)
	}

	existing, err := e.operationRepo.GetByIdempotencyKey(ctx, fmt.Sprintf("%s:%s", resourceID, operationName))
	if err == nil && existing != nil {
		return existing, nil
	}

	op := &domain.Operation{
		OperationID:     uuid.New().String(),
		Resource:        resource.ID,
		ResourceVersion: resource.Version,
		ProviderID:      resource.ProviderID,
		OperationName:   operationName,
		State:           domain.OperationStatePending,
		Attempt:         1,
		MaxAttempts:     3,
		CreatedAt:       time.Now(),
	}

	if err := e.operationRepo.Create(ctx, op); err != nil {
		return nil, fmt.Errorf("failed to create operation: %w", err)
	}

	go e.runResourceOperation(context.Background(), op, input)

	return op, nil
}

func (e *PluginExecutor) runResourceOperation(ctx context.Context, op *domain.Operation, input json.RawMessage) {
	op.State = domain.OperationStateRunning
	started := time.Now()
	op.StartedAt = &started
	e.operationRepo.Update(ctx, op)

	result, err := e.InvokeResource(ctx, op.ProviderID, op.OperationName, input)
	if err != nil {
		op.State = domain.OperationStateFailed
		op.ErrorCode = "INTERNAL_ERROR"
		op.ErrorMessage = err.Error()
	} else if !result.Success {
		op.State = domain.OperationStateFailed
		op.ErrorCode = result.ErrorCode
		op.ErrorMessage = result.ErrorMsg
	} else {
		op.State = domain.OperationStateSucceeded
	}

	completed := time.Now()
	op.CompletedAt = &completed
	e.operationRepo.Update(ctx, op)

	log.Printf("Operation %s completed with state %s", op.OperationID, op.State)
}

func (e *PluginExecutor) AttachResources(ctx context.Context, relationshipID string) (*domain.RelationshipExecution, error) {
	rel, err := e.relationshipRepo.Get(ctx, relationshipID)
	if err != nil {
		return nil, fmt.Errorf("relationship not found: %w", err)
	}

	execution := &domain.RelationshipExecution{
		ExecutionID:    uuid.New().String(),
		RelationshipID: rel.ID,
		Operation:      "attach",
		State:          domain.RelationshipStatePending,
		StartedAt:      time.Now(),
	}

	if err := e.sagaRepo.CreateExecution(ctx, execution); err != nil {
		return nil, fmt.Errorf("failed to create execution: %w", err)
	}

	source, err := e.resourceRepo.Get(ctx, rel.SourceResourceID)
	if err != nil {
		return nil, fmt.Errorf("source resource not found: %w", err)
	}
	target, err := e.resourceRepo.Get(ctx, rel.TargetResourceID)
	if err != nil {
		return nil, fmt.Errorf("target resource not found: %w", err)
	}

	steps := []struct {
		idx   int
		role  string
		op    string
		input json.RawMessage
	}{
		{0, "source", "prepare", toJSON(map[string]string{"resource_id": source.ExternalID})},
		{1, "target", "attach", toJSON(map[string]string{"resource_id": target.ExternalID})},
	}

	for _, s := range steps {
		step := &domain.SagaStepExecution{
			StepID:               uuid.New().String(),
			ExecutionID:          execution.ExecutionID,
			StepIndex:            s.idx,
			Role:                 s.role,
			Operation:            s.op,
			Input:                s.input,
			State:                domain.StepStatePending,
			CompensationRequired: true,
			CompensationInput:    s.input,
		}
		e.sagaRepo.CreateStep(ctx, step)
	}

	go e.runAttachmentSaga(context.Background(), execution, source, target)

	return execution, nil
}

func (e *PluginExecutor) runAttachmentSaga(ctx context.Context, exec *domain.RelationshipExecution, source, target *domain.Resource) {
	steps, _ := e.sagaRepo.ListStepsByExecution(ctx, exec.ExecutionID)

	for _, step := range steps {
		step.State = domain.StepStateRunning
		started := time.Now()
		step.StartedAt = &started
		e.sagaRepo.UpdateStep(ctx, step)

		result, err := e.InvokeRelationshipStep(ctx, step, source, target)
		if err != nil || result == nil || !result.Success {
			e.handleStepFailure(ctx, step, exec, result, err, source, target)
			return
		}

		step.State = domain.StepStateSucceeded
		step.Output = result.Output
		completed := time.Now()
		step.CompletedAt = &completed
		e.sagaRepo.UpdateStep(ctx, step)
	}

	exec.State = domain.RelationshipStateActive
	completed := time.Now()
	exec.CompletedAt = &completed
	e.sagaRepo.UpdateExecution(ctx, exec)

	rel, _ := e.relationshipRepo.Get(ctx, exec.RelationshipID)
	if rel != nil {
		rel.State = domain.RelationshipStateActive
		e.relationshipRepo.Update(ctx, rel)
	}
}

func (e *PluginExecutor) InvokeRelationshipStep(ctx context.Context, step *domain.SagaStepExecution, source, target *domain.Resource) (*InvokeResult, error) {
	var providerID string
	if step.Role == "source" {
		providerID = source.ProviderID
	} else {
		providerID = target.ProviderID
	}

	prov, err := e.providerRegistry.Get(ctx, providerID)
	if err != nil {
		return nil, err
	}

	plugin, err := e.pluginRegistry.GetPlugin(ctx, prov.PluginID)
	if err != nil {
		return nil, err
	}

	req := &registry.InvokeRequest{
		Kind:       "orion.io/storage.attachment",
		Version:    "v1",
		ProviderID: providerID,
		Operation:  fmt.Sprintf("%s:%s", step.Role, step.Operation),
		Input:      step.Input,
		Metadata: map[string]string{
			"source_resource_id": source.ExternalID,
			"target_resource_id": target.ExternalID,
		},
	}
	// The plugin wire protocol carries relationship participant IDs in
	// metadata while the payload remains provider-contract data.
	// InvokeRequest is extended at the adapter boundary where available.

	resp, err := plugin.Client.Invoke(ctx, req)
	if err != nil {
		return nil, err
	}

	return &InvokeResult{
		Success:   resp.Success,
		Output:    resp.Output,
		ErrorCode: getErrorCode(resp.Error),
		ErrorMsg:  getErrorMessage(resp.Error),
	}, nil
}

func (e *PluginExecutor) handleStepFailure(ctx context.Context, step *domain.SagaStepExecution, exec *domain.RelationshipExecution, result *InvokeResult, err error, source, target *domain.Resource) {
	if err != nil {
		step.ErrorMessage = err.Error()
	} else if result != nil {
		step.ErrorCode = result.ErrorCode
		step.ErrorMessage = result.ErrorMsg
	} else {
		step.ErrorCode = "UNKNOWN"
		step.ErrorMessage = "relationship step failed without a result"
	}
	step.State = domain.StepStateFailed
	completed := time.Now()
	step.CompletedAt = &completed
	e.sagaRepo.UpdateStep(ctx, step)

	if step.StepIndex > 0 {
		e.compensate(ctx, exec, step.StepIndex, source, target)
		return
	}

	exec.State = domain.RelationshipStateError
	exec.ErrorCode = step.ErrorCode
	exec.ErrorMessage = step.ErrorMessage
	e.sagaRepo.UpdateExecution(ctx, exec)

	rel, _ := e.relationshipRepo.Get(ctx, exec.RelationshipID)
	if rel != nil {
		rel.State = domain.RelationshipStateError
		e.relationshipRepo.Update(ctx, rel)
	}
}

func (e *PluginExecutor) compensate(ctx context.Context, exec *domain.RelationshipExecution, failedIndex int, source, target *domain.Resource) {
	exec.State = domain.RelationshipStateCompensating
	e.sagaRepo.UpdateExecution(ctx, exec)
	steps, err := e.sagaRepo.ListStepsByExecution(ctx, exec.ExecutionID)
	if err != nil {
		exec.State = domain.RelationshipStateError
		exec.ErrorCode = "COMPENSATION_LOAD_FAILED"
		exec.ErrorMessage = err.Error()
		e.sagaRepo.UpdateExecution(ctx, exec)
		return
	}
	for i := len(steps) - 1; i >= 0; i-- {
		step := steps[i]
		if step.StepIndex >= failedIndex || step.State != domain.StepStateSucceeded || !step.CompensationRequired {
			continue
		}
		step.State = domain.StepStateCompensating
		e.sagaRepo.UpdateStep(ctx, step)
		comp := &domain.SagaStepExecution{Role: step.Role, Operation: compensationOperation(step.Operation), Input: step.Output}
		result, callErr := e.InvokeRelationshipStep(ctx, comp, source, target)
		if callErr != nil || result == nil || !result.Success {
			step.State = domain.StepStateCompensationFailed
			if callErr != nil {
				step.ErrorMessage = callErr.Error()
			} else if result != nil {
				step.ErrorCode, step.ErrorMessage = result.ErrorCode, result.ErrorMsg
			}
		} else {
			step.State = domain.StepStateCompensated
		}
		e.sagaRepo.UpdateStep(ctx, step)
	}
	exec.State = domain.RelationshipStateError
	e.sagaRepo.UpdateExecution(ctx, exec)
	if rel, _ := e.relationshipRepo.Get(ctx, exec.RelationshipID); rel != nil {
		rel.State = domain.RelationshipStateError
		e.relationshipRepo.Update(ctx, rel)
	}
}

func compensationOperation(operation string) string {
	switch operation {
	case "prepare":
		return "release"
	case "attach":
		return "detach"
	case "release":
		return "prepare"
	case "detach":
		return "attach"
	default:
		return operation
	}
}

func getErrorCode(e *registry.OrionError) string {
	if e == nil {
		return ""
	}
	return e.Code
}

func getErrorMessage(e *registry.OrionError) string {
	if e == nil {
		return ""
	}
	return e.Message
}

func toJSON(v interface{}) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

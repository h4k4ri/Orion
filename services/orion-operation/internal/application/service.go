package application

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/horizon/orion/libs/go/kit/ids"
	"github.com/horizon/orion/services/orion-operation/internal/domain"
	"github.com/horizon/orion/services/orion-operation/internal/ports"
)

var (
	ErrOperationNotFound = errors.New("operation not found")
	ErrInvalidTransition = errors.New("invalid state transition")
	ErrAlreadyTerminal   = errors.New("operation is already in a terminal state")
)

type Service struct {
	store ports.OperationStore
}

var tracer = otel.Tracer("github.com/horizon/orion/services/orion-operation")

func NewService(store ports.OperationStore) *Service {
	return &Service{store: store}
}

func (s *Service) CreateOperation(ctx context.Context, req domain.CreateOperationRequest) (domain.Operation, error) {
	ctx, span := tracer.Start(ctx, "operation.Create")
	defer span.End()

	now := time.Now().UTC()
	op := domain.Operation{
		ID:            req.ID,
		ResourceType:  req.ResourceType,
		ResourceID:    req.ResourceID,
		ProjectID:     req.ProjectID,
		RequestID:     req.RequestID,
		OperationType: req.OperationType,
		State:         domain.StatePending,
		CurrentStep:   "",
		Attempt:       0,
		CreatedAt:     now,
	}

	created, err := s.store.CreateOperation(ctx, op)
	if err != nil {
		return domain.Operation{}, err
	}
	if !created {
		return s.GetOperation(ctx, op.ID)
	}

	if err := s.appendEvent(ctx, op.ID, domain.EventTypeStateTransition, "", string(domain.StatePending), "", nil); err != nil {
		return domain.Operation{}, err
	}

	span.SetAttributes(
		attribute.String("orion.operation_id", op.ID),
		attribute.String("orion.operation_type", op.OperationType),
		attribute.String("orion.resource_type", op.ResourceType),
		attribute.String("orion.resource_id", op.ResourceID),
	)

	return op, nil
}

func (s *Service) TransitionTo(ctx context.Context, operationID string, nextState domain.OperationState, step domain.OperationStep, payload []byte) error {
	ctx, span := tracer.Start(ctx, "operation.TransitionTo")
	defer span.End()

	op, err := s.store.GetOperation(ctx, operationID)
	if errors.Is(err, ports.ErrOperationNotFound) {
		return ErrOperationNotFound
	}
	if err != nil {
		return err
	}

	if op.State.IsTerminal() {
		return ErrAlreadyTerminal
	}

	if !op.State.CanTransitionTo(nextState) {
		return ErrInvalidTransition
	}

	fromState := op.State
	op.State = nextState
	op.CurrentStep = step

	now := time.Now().UTC()
	if op.StartedAt == nil && nextState != domain.StatePending {
		op.StartedAt = &now
	}
	if nextState.IsTerminal() {
		op.FinishedAt = &now
	}

	ev := domain.OperationEvent{
		ID:          ids.New("opevt"),
		OperationID: op.ID,
		EventType:   string(domain.EventTypeStateTransition),
		FromState:   string(fromState),
		ToState:     string(nextState),
		Step:        string(step),
		Payload:     payload,
		CreatedAt:   now,
	}

	outbox := s.newOutboxEvent(ctx, op.ID, "operation.state_changed", map[string]any{
		"operation_id": op.ID,
		"from_state":   fromState,
		"to_state":     nextState,
		"step":         step,
	})

	if err := s.store.TransitionWithOutbox(ctx, op, ev, outbox); err != nil {
		return err
	}

	span.SetAttributes(
		attribute.String("orion.operation_id", operationID),
		attribute.String("orion.from_state", string(fromState)),
		attribute.String("orion.to_state", string(nextState)),
	)
	return nil
}

func (s *Service) Fail(ctx context.Context, operationID string, errorCode string, errorMessage string) error {
	ctx, span := tracer.Start(ctx, "operation.Fail")
	defer span.End()

	op, err := s.store.GetOperation(ctx, operationID)
	if errors.Is(err, ports.ErrOperationNotFound) {
		return ErrOperationNotFound
	}
	if err != nil {
		return err
	}

	if op.State.IsTerminal() {
		return ErrAlreadyTerminal
	}

	fromState := op.State
	op.State = domain.StateFailed
	op.ErrorCode = errorCode
	op.ErrorMessage = errorMessage
	now := time.Now().UTC()
	op.FinishedAt = &now

	payload, _ := json.Marshal(errorPayload{Code: errorCode, Message: errorMessage})

	ev := domain.OperationEvent{
		ID:          ids.New("opevt"),
		OperationID: op.ID,
		EventType:   string(domain.EventTypeStateTransition),
		FromState:   string(fromState),
		ToState:     string(domain.StateFailed),
		Payload:     payload,
		CreatedAt:   now,
	}

	outbox := s.newOutboxEvent(ctx, op.ID, "operation.failed", map[string]any{
		"operation_id": op.ID,
		"from_state":   fromState,
		"error_code":   errorCode,
		"error_message": errorMessage,
	})

	return s.store.TransitionWithOutbox(ctx, op, ev, outbox)
}

func (s *Service) Succeed(ctx context.Context, operationID string) error {
	ctx, span := tracer.Start(ctx, "operation.Succeed")
	defer span.End()

	op, err := s.store.GetOperation(ctx, operationID)
	if errors.Is(err, ports.ErrOperationNotFound) {
		return ErrOperationNotFound
	}
	if err != nil {
		return err
	}

	if op.State.IsTerminal() {
		return ErrAlreadyTerminal
	}

	fromState := op.State
	op.State = domain.StateSucceeded
	now := time.Now().UTC()
	op.FinishedAt = &now

	ev := domain.OperationEvent{
		ID:          ids.New("opevt"),
		OperationID: op.ID,
		EventType:   string(domain.EventTypeStateTransition),
		FromState:   string(fromState),
		ToState:     string(domain.StateSucceeded),
		CreatedAt:   now,
	}

	outbox := s.newOutboxEvent(ctx, op.ID, "operation.succeeded", map[string]any{
		"operation_id": op.ID,
		"from_state":   fromState,
	})

	return s.store.TransitionWithOutbox(ctx, op, ev, outbox)
}

func (s *Service) StartCompensation(ctx context.Context, operationID string) error {
	ctx, span := tracer.Start(ctx, "operation.StartCompensation")
	defer span.End()

	op, err := s.store.GetOperation(ctx, operationID)
	if errors.Is(err, ports.ErrOperationNotFound) {
		return ErrOperationNotFound
	}
	if err != nil {
		return err
	}

	if op.State.IsTerminal() {
		return ErrAlreadyTerminal
	}

	fromState := op.State
	op.State = domain.StateCompensating
	op.CurrentStep = domain.StepCompensateNetwork

	now := time.Now().UTC()
	ev := domain.OperationEvent{
		ID:          ids.New("opevt"),
		OperationID: op.ID,
		EventType:   string(domain.EventTypeStateTransition),
		FromState:   string(fromState),
		ToState:     string(domain.StateCompensating),
		Step:        string(domain.StepCompensateNetwork),
		CreatedAt:   now,
	}

	outbox := s.newOutboxEvent(ctx, op.ID, "operation.compensating", map[string]any{
		"operation_id": op.ID,
		"from_state":   fromState,
	})

	return s.store.TransitionWithOutbox(ctx, op, ev, outbox)
}

func (s *Service) GetOperation(ctx context.Context, id string) (domain.Operation, error) {
	op, err := s.store.GetOperation(ctx, id)
	if errors.Is(err, ports.ErrOperationNotFound) {
		return domain.Operation{}, ErrOperationNotFound
	}
	if err == nil {
		op.Events, err = s.store.GetEvents(ctx, id)
	}
	return op, err
}

func (s *Service) ListOperations(ctx context.Context, projectID string) ([]domain.Operation, error) {
	return s.store.ListOperations(ctx, projectID)
}

func (s *Service) appendEvent(ctx context.Context, operationID string, eventType domain.OperationEventType, fromState string, toState string, step string, payload []byte) error {
	ev := domain.OperationEvent{
		ID:          ids.New("opevt"),
		OperationID: operationID,
		EventType:   string(eventType),
		FromState:   fromState,
		ToState:     toState,
		Step:        step,
		Payload:     payload,
		CreatedAt:   time.Now().UTC(),
	}
	return s.store.AppendEvent(ctx, ev)
}

func (s *Service) newOutboxEvent(ctx context.Context, aggregateID string, subject string, payload map[string]any) ports.OutboxEvent {
	payloadBytes, _ := json.Marshal(payload)
	traceID := ""
	if span := trace.SpanFromContext(ctx); span.SpanContext().HasTraceID() {
		traceID = span.SpanContext().TraceID().String()
	}
	return ports.OutboxEvent{
		ID:            ids.New("obox"),
		AggregateType: "operation",
		AggregateID:   aggregateID,
		Subject:       subject,
		Payload:       payloadBytes,
		TraceID:      traceID,
	}
}

type errorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

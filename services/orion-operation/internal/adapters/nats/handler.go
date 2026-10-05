package nats

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/horizon/orion/libs/go/kit/events"
	"github.com/horizon/orion/libs/go/kit/natsx"
	"github.com/horizon/orion/services/orion-operation/internal/application"
	"github.com/horizon/orion/services/orion-operation/internal/domain"
)

var tracer = otel.Tracer("github.com/horizon/orion/services/orion-operation")

type EventHandler struct {
	opSvc *application.Service
	sub   *natsx.Subscriber
}

func NewEventHandler(opSvc *application.Service, client *natsx.Client) *EventHandler {
	return &EventHandler{
		opSvc: opSvc,
		sub:   natsx.NewSubscriber(client, 4),
	}
}

func (h *EventHandler) HandleEvent(msg *nats.Msg) error {
	ctx := natsx.ExtractContext(context.Background(), msg.Header)
	var taskEv events.TaskEvent
	if err := json.Unmarshal(msg.Data, &taskEv); err == nil && taskEv.Task.ID != "" {
		return h.handleTaskEvent(ctx, taskEv)
	}

	var resourceEv events.ResourceEvent
	if err := json.Unmarshal(msg.Data, &resourceEv); err == nil && resourceEv.ResourceID != "" {
		return h.handleResourceEvent(ctx, resourceEv)
	}

	return nil
}

func (h *EventHandler) handleTaskEvent(ctx context.Context, ev events.TaskEvent) error {
	ctx, span := tracer.Start(ctx, "operation.HandleTaskEvent")
	defer span.End()

	task := ev.Task
	parts := strings.Split(task.Kind, ".")
	action := "create"
	if len(parts) >= 2 {
		action = parts[1]
	}
	operationID := ev.OperationID
	if operationID == "" {
		operationID = task.RequestID
	}

	if operationID == "" {
		return nil
	}

	if ev.EventType == events.TaskFailed {
		if err := h.opSvc.Fail(ctx, operationID, task.ErrorCode, task.ErrorMessage); err != nil {
			log.Printf("failed to fail operation %s: %v", operationID, err)
		}
		return nil
	}
	if ev.EventType == events.TaskSucceeded && (action == "create" || action == "build") {
		op, err := h.opSvc.GetOperation(ctx, operationID)
		if err != nil {
			return nil
		}
		if op.State == domain.StateSpawning {
			if err := h.opSvc.TransitionTo(ctx, operationID, domain.StateVerifying, domain.StepVerifyInstance, nil); err != nil {
				return err
			}
		}
		if ev.ObservedState == "running" || ev.ObservedState == "active" {
			if err := h.opSvc.Succeed(ctx, operationID); err != nil && !errors.Is(err, application.ErrAlreadyTerminal) {
				log.Printf("failed to succeed operation %s: %v", operationID, err)
			}
		}
	}
	return nil
}

func (h *EventHandler) handleResourceEvent(ctx context.Context, ev events.ResourceEvent) error {
	ctx, span := tracer.Start(ctx, "operation.HandleResourceEvent")
	defer span.End()

	span.SetAttributes(
		attribute.String("orion.resource_type", ev.ResourceType),
		attribute.String("orion.resource_id", ev.ResourceID),
		attribute.String("orion.event_type", ev.EventType),
	)
	return nil
}

func (h *EventHandler) Start(ctx context.Context) error {
	if err := h.sub.Consume(ctx, "orion.event.>", h.HandleEvent,
		natsx.WithStream("ORION_EVENTS"),
		natsx.WithQueueGroup("orion-operation"),
		natsx.WithDurable("orion-operation-events"),
	); err != nil {
		return fmt.Errorf("consume orion.events: %w", err)
	}
	return nil
}

package grpcadapter

import (
	"context"

	commonv1 "github.com/horizon/orion/gen/go/common/v1"
	operationv1 "github.com/horizon/orion/gen/go/operation/v1"
	"github.com/horizon/orion/services/orion-operation/internal/application"
	"github.com/horizon/orion/services/orion-operation/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Server struct {
	operationv1.UnimplementedOperationServiceServer
	service *application.Service
}

func NewServer(service *application.Service) *Server {
	return &Server{service: service}
}

func (s *Server) CreateOperation(ctx context.Context, req *operationv1.CreateOperationRequest) (*operationv1.Operation, error) {
	op, err := s.service.CreateOperation(ctx, domain.CreateOperationRequest{
		ID:            req.GetId(),
		ResourceType:  req.GetResourceType(),
		ResourceID:    req.GetResourceId(),
		ProjectID:     req.GetProjectId(),
		RequestID:     req.GetRequestId(),
		OperationType: req.GetOperationType(),
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return toProto(op), nil
}

func (s *Server) GetOperation(ctx context.Context, req *operationv1.GetOperationRequest) (*operationv1.Operation, error) {
	op, err := s.service.GetOperation(ctx, req.GetId())
	if err == application.ErrOperationNotFound {
		return nil, status.Error(codes.NotFound, err.Error())
	}
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return toProto(op), nil
}

func (s *Server) TransitionOperation(ctx context.Context, req *operationv1.TransitionOperationRequest) (*emptypb.Empty, error) {
	err := s.service.TransitionTo(ctx, req.GetOperationId(), domain.OperationState(req.GetState()), domain.OperationStep(req.GetStep()), req.GetPayload())
	switch err {
	case nil:
		return &emptypb.Empty{}, nil
	case application.ErrOperationNotFound:
		return nil, status.Error(codes.NotFound, err.Error())
	case application.ErrInvalidTransition:
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	case application.ErrAlreadyTerminal:
		return nil, status.Error(codes.Aborted, err.Error())
	default:
		return nil, status.Error(codes.Internal, err.Error())
	}
}

func toProto(op domain.Operation) *operationv1.Operation {
	result := &operationv1.Operation{
		Id:            &commonv1.OperationID{Value: op.ID},
		ResourceType:  op.ResourceType,
		ResourceId:    &commonv1.ResourceID{Value: op.ResourceID},
		ProjectId:     &commonv1.ProjectID{Value: op.ProjectID},
		RequestId:     &commonv1.RequestID{Value: op.RequestID},
		OperationType: op.OperationType,
		State:         toProtoState(op.State),
		CurrentStep:   string(op.CurrentStep),
		Attempt:       int32(op.Attempt),
		CreatedAt:     timestamppb.New(op.CreatedAt),
		ErrorCode:     op.ErrorCode,
		ErrorMessage:  op.ErrorMessage,
	}
	if op.StartedAt != nil {
		result.StartedAt = timestamppb.New(*op.StartedAt)
	}
	if op.FinishedAt != nil {
		result.FinishedAt = timestamppb.New(*op.FinishedAt)
	}
	for _, event := range op.Events {
		result.Events = append(result.Events, eventToProto(event))
	}
	return result
}

func eventToProto(event domain.OperationEvent) *operationv1.OperationEvent {
	result := &operationv1.OperationEvent{
		Id:          &commonv1.ResourceID{Value: event.ID},
		OperationId: &commonv1.OperationID{Value: event.OperationID},
		Sequence:    event.Sequence,
		EventType:   toProtoEventType(event.EventType),
		FromState:   event.FromState,
		ToState:     event.ToState,
		Step:        event.Step,
		CreatedAt:   timestamppb.New(event.CreatedAt),
	}
	if len(event.Payload) > 0 {
		result.Payload = &anypb.Any{Value: event.Payload}
	}
	return result
}

func toProtoEventType(eventType string) operationv1.EventType {
	switch domain.OperationEventType(eventType) {
	case domain.EventTypeStateTransition:
		return operationv1.EventType_EVENT_TYPE_STATE_TRANSITION
	case domain.EventTypeCommandSent:
		return operationv1.EventType_EVENT_TYPE_COMMAND_SENT
	case domain.EventTypeEventReceived:
		return operationv1.EventType_EVENT_TYPE_EVENT_RECEIVED
	case domain.EventTypeStepStarted:
		return operationv1.EventType_EVENT_TYPE_STEP_STARTED
	case domain.EventTypeStepFinished:
		return operationv1.EventType_EVENT_TYPE_STEP_FINISHED
	default:
		return operationv1.EventType_EVENT_TYPE_ERROR
	}
}

func toProtoState(state domain.OperationState) operationv1.OperationState {
	switch state {
	case domain.StatePending:
		return operationv1.OperationState_OPERATION_STATE_PENDING
	case domain.StateSucceeded:
		return operationv1.OperationState_OPERATION_STATE_SUCCEEDED
	case domain.StateFailed:
		return operationv1.OperationState_OPERATION_STATE_FAILED
	case domain.StateCancelled:
		return operationv1.OperationState_OPERATION_STATE_CANCELLED
	case domain.StateTimedOut:
		return operationv1.OperationState_OPERATION_STATE_TIMED_OUT
	default:
		return operationv1.OperationState_OPERATION_STATE_RUNNING
	}
}

package operationclient

import (
	"context"
	"fmt"

	operationv1 "github.com/horizon/orion/gen/go/operation/v1"
	"github.com/horizon/orion/libs/go/kit/retry"
	"github.com/horizon/orion/libs/go/kit/tlsconfig"
	"github.com/horizon/orion/services/orion-api/internal/domain"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"time"
)

type Client struct {
	conn   *grpc.ClientConn
	client operationv1.OperationServiceClient
}

func New(addr string) *Client {
	transport := insecure.NewCredentials()
	if configured, tlsErr := tlsconfig.ClientCredentialsFromEnv(); tlsErr != nil {
		panic(tlsErr)
	} else if configured != nil {
		transport = configured
	}
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(transport), grpc.WithStatsHandler(otelgrpc.NewClientHandler()))
	if err != nil {
		panic(fmt.Sprintf("operation gRPC client: %v", err))
	}
	return &Client{conn: conn, client: operationv1.NewOperationServiceClient(conn)}
}

func (c *Client) Close() error { return c.conn.Close() }

func (c *Client) GetOperation(ctx context.Context, operationID string) (domain.Operation, error) {
	var op *operationv1.Operation
	err := retryRPC(ctx, func(callCtx context.Context) error {
		var callErr error
		op, callErr = c.client.GetOperation(callCtx, &operationv1.GetOperationRequest{Id: operationID})
		return callErr
	})
	if err != nil {
		return domain.Operation{}, fmt.Errorf("get operation: %w", err)
	}
	return fromProto(op), nil
}

func retryRPC(ctx context.Context, operation func(context.Context) error) error {
	policy := retry.Policy{MaxAttempts: 3, Initial: 100 * time.Millisecond, MaxDelay: time.Second}
	return retry.Do(ctx, policy, operation, func(err error) bool {
		code := status.Code(err)
		return code == codes.Unavailable || code == codes.ResourceExhausted || code == codes.DeadlineExceeded
	})
}

func fromProto(op *operationv1.Operation) domain.Operation {
	result := domain.Operation{
		ResourceType:  op.GetResourceType(),
		OperationType: op.GetOperationType(),
		State:         stateName(op.GetState()),
		CurrentStep:   op.GetCurrentStep(),
		Attempt:       int(op.GetAttempt()),
	}
	if op.GetId() != nil {
		result.ID = op.GetId().GetValue()
	}
	if op.GetResourceId() != nil {
		result.ResourceID = op.GetResourceId().GetValue()
	}
	if op.GetProjectId() != nil {
		result.ProjectID = op.GetProjectId().GetValue()
	}
	if op.GetRequestId() != nil {
		result.RequestID = op.GetRequestId().GetValue()
	}
	if op.GetCreatedAt() != nil {
		result.CreatedAt = op.GetCreatedAt().AsTime()
	}
	if op.GetStartedAt() != nil {
		startedAt := op.GetStartedAt().AsTime()
		result.StartedAt = &startedAt
	}
	if op.GetFinishedAt() != nil {
		finishedAt := op.GetFinishedAt().AsTime()
		result.FinishedAt = &finishedAt
	}
	result.ErrorCode = op.GetErrorCode()
	result.ErrorMessage = op.GetErrorMessage()
	for _, event := range op.GetEvents() {
		item := domain.OperationEvent{
			EventType: event.GetEventType().String(),
			FromState: event.GetFromState(),
			ToState:   event.GetToState(),
			Step:      event.GetStep(),
			Sequence:  event.GetSequence(),
		}
		if event.GetId() != nil {
			item.ID = event.GetId().GetValue()
		}
		if event.GetOperationId() != nil {
			item.OperationID = event.GetOperationId().GetValue()
		}
		if event.GetCreatedAt() != nil {
			item.CreatedAt = event.GetCreatedAt().AsTime()
		}
		if event.GetPayload() != nil {
			item.Payload = event.GetPayload().GetValue()
		}
		result.Events = append(result.Events, item)
	}
	return result
}

func stateName(state operationv1.OperationState) string {
	switch state {
	case operationv1.OperationState_OPERATION_STATE_PENDING:
		return "PENDING"
	case operationv1.OperationState_OPERATION_STATE_SUCCEEDED:
		return "SUCCEEDED"
	case operationv1.OperationState_OPERATION_STATE_FAILED:
		return "FAILED"
	case operationv1.OperationState_OPERATION_STATE_CANCELLED:
		return "CANCELLED"
	case operationv1.OperationState_OPERATION_STATE_TIMED_OUT:
		return "TIMED_OUT"
	default:
		return "RUNNING"
	}
}

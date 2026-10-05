package operationclient

import (
	"context"
	"fmt"
	"time"

	operationv1 "github.com/horizon/orion/gen/go/operation/v1"
	"github.com/horizon/orion/libs/go/kit/retry"
	"github.com/horizon/orion/libs/go/kit/tlsconfig"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
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

type CreateOperationRequest struct {
	ID            string
	ResourceType  string
	ResourceID    string
	ProjectID     string
	RequestID     string
	OperationType string
}

type OperationResponse struct {
	ID            string
	ResourceType  string
	ResourceID    string
	ProjectID     string
	RequestID     string
	OperationType string
	State         string
	CurrentStep   string
	Attempt       int
	CreatedAt     time.Time
}

func (c *Client) CreateOperation(ctx context.Context, req CreateOperationRequest) (OperationResponse, error) {
	var op *operationv1.Operation
	err := retryRPC(ctx, func(callCtx context.Context) error {
		var callErr error
		op, callErr = c.client.CreateOperation(callCtx, &operationv1.CreateOperationRequest{
			Id:            req.ID,
			ResourceType:  req.ResourceType,
			ResourceId:    req.ResourceID,
			ProjectId:     req.ProjectID,
			RequestId:     req.RequestID,
			OperationType: req.OperationType,
		})
		return callErr
	})
	if err != nil {
		return OperationResponse{}, fmt.Errorf("create operation: %w", err)
	}
	return fromProto(op), nil
}

func (c *Client) GetOperation(ctx context.Context, id string) (OperationResponse, error) {
	var op *operationv1.Operation
	err := retryRPC(ctx, func(callCtx context.Context) error {
		var callErr error
		op, callErr = c.client.GetOperation(callCtx, &operationv1.GetOperationRequest{Id: id})
		return callErr
	})
	if err != nil {
		return OperationResponse{}, fmt.Errorf("get operation: %w", err)
	}
	return fromProto(op), nil
}

func (c *Client) TransitionOperation(ctx context.Context, id, state, step string, payload []byte) error {
	err := retryRPC(ctx, func(callCtx context.Context) error {
		_, callErr := c.client.TransitionOperation(callCtx, &operationv1.TransitionOperationRequest{
			OperationId: id,
			State:       state,
			Step:        step,
			Payload:     payload,
		})
		return callErr
	})
	if err != nil {
		return fmt.Errorf("transition operation: %w", err)
	}
	return nil
}

func retryRPC(ctx context.Context, operation func(context.Context) error) error {
	policy := retry.Policy{MaxAttempts: 3, Initial: 100 * time.Millisecond, MaxDelay: time.Second}
	return retry.Do(ctx, policy, operation, func(err error) bool {
		code := status.Code(err)
		return code == codes.Unavailable || code == codes.ResourceExhausted || code == codes.DeadlineExceeded
	})
}

func (c *Client) FailOperation(ctx context.Context, id, message string) error {
	return c.TransitionOperation(ctx, id, "FAILED", "", []byte(message))
}

func fromProto(op *operationv1.Operation) OperationResponse {
	result := OperationResponse{
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

package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/horizon/orion/sdk/go/health"
	"github.com/horizon/orion/sdk/go/middleware"
	v1 "github.com/horizon/orion/sdk/go/proto/plugin/v1"
	"github.com/horizon/orion/sdk/go/retry"
)

type ServerOption func(*Server)

func WithUnaryInterceptor(f grpc.UnaryServerInterceptor) ServerOption {
	return func(s *Server) { s.unaryInterceptors = append(s.unaryInterceptors, f) }
}

func WithStreamInterceptor(f grpc.StreamServerInterceptor) ServerOption {
	return func(s *Server) { s.streamInterceptors = append(s.streamInterceptors, f) }
}

func WithHealthServer(h *health.GRPCServer) ServerOption {
	return func(s *Server) { s.healthServer = h }
}

func WithLogger(logger *middleware.Logging) ServerOption {
	return func(s *Server) { s.logger = logger }
}

type Server struct {
	plugin             *Plugin
	endpoint           string
	grpcServer         *grpc.Server
	mu                 sync.RWMutex
	running            bool
	healthServer       *health.GRPCServer
	logger             *middleware.Logging
	unaryInterceptors  []grpc.UnaryServerInterceptor
	streamInterceptors []grpc.StreamServerInterceptor
	operations         map[string]*v1.OperationStatus
	operationCancels   map[string]context.CancelFunc
	timeout            time.Duration
	retryConfig        retry.Config
	retryEnabled       bool
}

func NewServer(p *Plugin, endpoint string, opts ...ServerOption) *Server {
	s := &Server{
		plugin:           p,
		endpoint:         endpoint,
		operations:       make(map[string]*v1.OperationStatus),
		operationCancels: make(map[string]context.CancelFunc),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *Server) Serve(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("server already running")
	}
	s.running = true
	s.mu.Unlock()

	lis, err := net.Listen("tcp", s.endpoint)
	if err != nil {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
		return fmt.Errorf("failed to listen: %w", err)
	}

	var opts []grpc.ServerOption
	if len(s.unaryInterceptors) > 0 {
		opts = append(opts, grpc.ChainUnaryInterceptor(s.unaryInterceptors...))
	}
	if len(s.streamInterceptors) > 0 {
		opts = append(opts, grpc.ChainStreamInterceptor(s.streamInterceptors...))
	}
	if s.logger != nil {
		opts = append(opts, grpc.ChainUnaryInterceptor(s.logger.UnaryServerInterceptor()))
	}
	grpcServer := grpc.NewServer(opts...)
	s.mu.Lock()
	s.grpcServer = grpcServer
	s.mu.Unlock()
	v1.RegisterPluginExecutorServer(grpcServer, &executorServer{plugin: s.plugin, parent: s})

	go func() {
		<-ctx.Done()
		grpcServer.GracefulStop()
	}()

	err = grpcServer.Serve(lis)
	s.mu.Lock()
	s.running = false
	s.mu.Unlock()
	return err
}

func (s *Server) Stop() {
	s.mu.Lock()
	server := s.grpcServer
	running := s.running
	s.mu.Unlock()
	if running && server != nil {
		server.GracefulStop()
	}
}

func (p *Plugin) Serve(ctx context.Context, endpoint string, opts ...ServerOption) error {
	if configuredEndpoint := os.Getenv("ORION_PLUGIN_ENDPOINT"); configuredEndpoint != "" {
		endpoint = configuredEndpoint
	}
	if manifestPath := os.Getenv("ORION_PLUGIN_MANIFEST"); manifestPath != "" {
		if err := p.WriteManifest(manifestPath); err != nil {
			return fmt.Errorf("write plugin manifest: %w", err)
		}
	}
	return NewServer(p, endpoint, opts...).Serve(ctx)
}

// executorServer is the protocol adapter. Plugin authors only implement SDK
// handlers; protobuf conversion and operation bookkeeping stay in the SDK.
type executorServer struct {
	v1.UnimplementedPluginExecutorServer
	plugin *Plugin
	parent *Server
}

func (e *executorServer) Invoke(ctx context.Context, req *v1.InvokeRequest) (*v1.InvokeResponse, error) {
	if req == nil {
		return &v1.InvokeResponse{Success: false, Error: protocolError("INVALID_REQUEST", "request is nil")}, nil
	}
	payload, err := structToJSON(req.Payload)
	if err != nil {
		return &v1.InvokeResponse{RequestId: req.RequestId, Error: protocolError("INVALID_PAYLOAD", err.Error())}, nil
	}
	if e.parent.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, e.parent.timeout)
		defer cancel()
	}

	if handler, err := e.plugin.GetResourceHandler(req.Resource); err == nil {
		if operation, ok := handler.Ops[req.Operation]; ok {
			request := &ResourceRequest{RequestID: req.RequestId, ProviderID: req.ProviderId, Operation: req.Operation, Payload: payload}
			var resp *ResourceResponse
			callErr := e.callWithRetry(ctx, func() error {
				var err error
				resp, err = operation(ctx, request)
				return err
			})
			return resourceResponse(req.RequestId, resp, callErr), nil
		}
	}

	role, operationName := splitRelationshipOperation(req.Operation)
	if handler, err := e.plugin.GetRelationshipHandler(req.Resource); err == nil && role != "" {
		if handler.Role != role {
			return &v1.InvokeResponse{RequestId: req.RequestId, Error: protocolError("ROLE_MISMATCH", "relationship role is not implemented by this plugin")}, nil
		}
		if operation, ok := handler.Ops[operationName]; ok {
			request := &RelationshipRequest{
				RequestID: req.RequestId, ProviderID: req.ProviderId, Operation: operationName,
				SourceResourceID: req.Metadata["source_resource_id"], TargetResourceID: req.Metadata["target_resource_id"], Payload: payload,
			}
			var resp *RelationshipResponse
			callErr := e.callWithRetry(ctx, func() error {
				var err error
				resp, err = operation(ctx, request)
				return err
			})
			return relationshipResponse(req.RequestId, resp, callErr), nil
		}
	}

	return &v1.InvokeResponse{RequestId: req.RequestId, Error: protocolError("NOT_SUPPORTED", fmt.Sprintf("operation %s is not supported for %s", req.Operation, req.Resource))}, nil
}

func (e *executorServer) InvokeAsync(ctx context.Context, req *v1.InvokeRequest) (*v1.OperationStatus, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	id := req.IdempotencyKey
	if id == "" {
		id = req.RequestId
	}
	if id == "" {
		id = fmt.Sprintf("operation-%d", time.Now().UnixNano())
	}

	operationCtx, cancel := context.WithCancel(context.Background())
	e.parent.mu.Lock()
	if existing, ok := e.parent.operations[id]; ok {
		e.parent.mu.Unlock()
		return proto.Clone(existing).(*v1.OperationStatus), nil
	}
	operation := &v1.OperationStatus{
		OperationId: id, State: v1.OperationState_OPERATION_STATE_RUNNING,
		Resource: req.Resource, ResourceVersion: req.ResourceVersion, OperationName: req.Operation,
		StartedAt: timestamppb.Now(), UpdatedAt: timestamppb.Now(),
	}
	e.parent.operations[id] = operation
	e.parent.operationCancels[id] = cancel
	e.parent.mu.Unlock()

	go func() {
		resp, invokeErr := e.Invoke(operationCtx, req)
		e.parent.mu.Lock()
		defer e.parent.mu.Unlock()
		delete(e.parent.operationCancels, id)
		operation.UpdatedAt = timestamppb.Now()
		operation.CompletedAt = operation.UpdatedAt
		operation.ProgressPercent = 100
		if operation.State == v1.OperationState_OPERATION_STATE_CANCELLING || operationCtx.Err() == context.Canceled {
			operation.State = v1.OperationState_OPERATION_STATE_CANCELLED
			operation.Error = protocolError("CANCELLED", "operation cancelled")
		} else if invokeErr != nil {
			operation.State = v1.OperationState_OPERATION_STATE_FAILED
			operation.Error = protocolError("INTERNAL", invokeErr.Error())
		} else if !resp.Success {
			operation.State = v1.OperationState_OPERATION_STATE_FAILED
			operation.Error = resp.Error
		} else {
			operation.State = v1.OperationState_OPERATION_STATE_SUCCEEDED
			operation.Result = resp.Result
		}
	}()
	return operation, nil
}

func (e *executorServer) GetOperationStatus(ctx context.Context, req *v1.GetOperationStatusRequest) (*v1.OperationStatus, error) {
	e.parent.mu.RLock()
	defer e.parent.mu.RUnlock()
	if operation, ok := e.parent.operations[req.OperationId]; ok {
		return proto.Clone(operation).(*v1.OperationStatus), nil
	}
	return &v1.OperationStatus{OperationId: req.OperationId, State: v1.OperationState_OPERATION_STATE_UNSPECIFIED, Error: protocolError("NOT_FOUND", "operation not found")}, nil
}

func (e *executorServer) CancelOperation(ctx context.Context, req *v1.CancelOperationRequest) (*v1.OperationStatus, error) {
	e.parent.mu.Lock()
	defer e.parent.mu.Unlock()
	operation, ok := e.parent.operations[req.OperationId]
	if !ok {
		return &v1.OperationStatus{OperationId: req.OperationId, Error: protocolError("NOT_FOUND", "operation not found")}, nil
	}
	if operation.State == v1.OperationState_OPERATION_STATE_RUNNING || operation.State == v1.OperationState_OPERATION_STATE_PENDING {
		operation.State = v1.OperationState_OPERATION_STATE_CANCELLING
		operation.UpdatedAt = timestamppb.Now()
		if cancel, ok := e.parent.operationCancels[req.OperationId]; ok {
			cancel()
		}
	}
	return proto.Clone(operation).(*v1.OperationStatus), nil
}

func (e *executorServer) Reconcile(ctx context.Context, req *v1.ReconcileRequest) (*v1.ReconcileResponse, error) {
	return &v1.ReconcileResponse{Success: false, Error: protocolError("NOT_SUPPORTED", "reconcile is owned by Orion core")}, nil
}

func (e *executorServer) HealthCheck(ctx context.Context, req *v1.HealthCheckRequest) (*v1.HealthCheckResponse, error) {
	if e.parent.healthServer != nil {
		statusValue, details, err := e.parent.healthServer.Check(ctx)
		if err != nil {
			return &v1.HealthCheckResponse{Healthy: false, Message: fmt.Sprintf("%s: %v", statusValue.String(), details)}, nil
		}
		return &v1.HealthCheckResponse{Healthy: statusValue == health.StatusHealthy, Message: statusValue.String()}, nil
	}
	return &v1.HealthCheckResponse{Healthy: true, Message: "ok"}, nil
}

func resourceResponse(requestID string, resp *ResourceResponse, callErr error) *v1.InvokeResponse {
	if callErr != nil {
		return &v1.InvokeResponse{RequestId: requestID, Error: protocolError("INTERNAL", callErr.Error())}
	}
	if resp == nil {
		return &v1.InvokeResponse{RequestId: requestID, Error: protocolError("INVALID_RESPONSE", "handler returned nil response")}
	}
	return &v1.InvokeResponse{RequestId: requestID, Success: resp.Success, Result: mustStruct(resp.Result), Error: toProtoError(resp.Error)}
}

func relationshipResponse(requestID string, resp *RelationshipResponse, callErr error) *v1.InvokeResponse {
	if callErr != nil {
		return &v1.InvokeResponse{RequestId: requestID, Error: protocolError("INTERNAL", callErr.Error())}
	}
	if resp == nil {
		return &v1.InvokeResponse{RequestId: requestID, Error: protocolError("INVALID_RESPONSE", "handler returned nil response")}
	}
	return &v1.InvokeResponse{RequestId: requestID, Success: resp.Success, Result: mustStruct(resp.Result), Error: toProtoError(resp.Error)}
}

func structToJSON(value *structpb.Struct) ([]byte, error) {
	if value == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(value.AsMap())
}

func mustStruct(payload []byte) *structpb.Struct {
	if len(payload) == 0 {
		return &structpb.Struct{Fields: map[string]*structpb.Value{}}
	}
	var value interface{}
	if json.Unmarshal(payload, &value) != nil {
		return &structpb.Struct{Fields: map[string]*structpb.Value{"value": structpb.NewStringValue(string(payload))}}
	}
	if object, ok := value.(map[string]interface{}); ok {
		if result, err := structpb.NewStruct(object); err == nil {
			return result
		}
	}
	result, _ := structpb.NewStruct(map[string]interface{}{"value": value})
	return result
}

func splitRelationshipOperation(operation string) (string, string) {
	parts := strings.SplitN(operation, ":", 2)
	if len(parts) != 2 {
		return "", operation
	}
	return parts[0], parts[1]
}

func protocolError(code, message string) *v1.OrionError {
	return &v1.OrionError{Code: code, Message: message}
}

func toProtoError(err *Error) *v1.OrionError {
	if err == nil {
		return nil
	}
	return protocolError(err.Code, err.Message)
}

func (e *executorServer) callWithRetry(ctx context.Context, fn func() error) error {
	if !e.parent.retryEnabled {
		return fn()
	}
	return retry.Do(ctx, e.parent.retryConfig, fn)
}

func WithRetry(cfg retry.Config) ServerOption {
	return func(s *Server) {
		if cfg.MaxAttempts <= 0 {
			cfg = retry.DefaultConfig
		}
		s.retryConfig = cfg
		s.retryEnabled = true
	}
}

func WithTimeout(timeout time.Duration) ServerOption {
	return func(s *Server) { s.timeout = timeout }
}

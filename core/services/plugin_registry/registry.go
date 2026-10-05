package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/horizon/orion/core/domain"
	"github.com/horizon/orion/core/ports"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/structpb"

	v1 "github.com/horizon/orion/sdk/go/proto/plugin/v1"
)

type PluginRegistry struct {
	mu           sync.RWMutex
	plugins      map[string]*RegisteredPlugin
	providerRepo ports.ProviderRepository
}

type RegisteredPlugin struct {
	PluginID      string
	Name          string
	Version       string
	Vendor        string
	Manifest      *domain.PluginManifest
	Conn          *grpc.ClientConn
	Client        PluginExecutorClient
	RegisteredAt  time.Time
	LastHeartbeat time.Time
}

type PluginExecutorClient interface {
	Invoke(ctx context.Context, req *InvokeRequest) (*InvokeResponse, error)
	HealthCheck(ctx context.Context, req *HealthCheckRequest) (*HealthCheckResponse, error)
}

type InvokeRequest struct {
	Kind       string
	Version    string
	ProviderID string
	Operation  string
	Input      json.RawMessage
	Metadata   map[string]string
}

type InvokeResponse struct {
	Success bool
	Output  json.RawMessage
	Error   *OrionError
}

type HealthCheckRequest struct{}
type HealthCheckResponse struct {
	Status  string
	Details map[string]string
}

type OrionError struct {
	Code    string
	Message string
	Details map[string]string
}

func NewPluginRegistry(providerRepo ports.ProviderRepository) *PluginRegistry {
	return &PluginRegistry{
		plugins:      make(map[string]*RegisteredPlugin),
		providerRepo: providerRepo,
	}
}

func (r *PluginRegistry) Register(ctx context.Context, name, version, vendor string, manifest *domain.PluginManifest, endpoint string) (string, error) {
	if manifest == nil {
		return "", fmt.Errorf("plugin manifest is required")
	}
	if name == "" || version == "" || endpoint == "" {
		return "", fmt.Errorf("plugin name, version and endpoint are required")
	}
	for _, resource := range manifest.Resources {
		if resource.Kind == "" || resource.Version == "" {
			return "", fmt.Errorf("plugin manifest contains an invalid resource declaration")
		}
	}
	for _, relationship := range manifest.Relationships {
		if relationship.Kind == "" || relationship.Version == "" {
			return "", fmt.Errorf("plugin manifest contains an invalid relationship declaration")
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	pluginID := uuid.New().String()

	conn, err := grpc.NewClient(endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return "", fmt.Errorf("failed to connect to plugin: %w", err)
	}

	r.plugins[pluginID] = &RegisteredPlugin{
		PluginID:      pluginID,
		Name:          name,
		Version:       version,
		Vendor:        vendor,
		Manifest:      manifest,
		Conn:          conn,
		Client:        &grpcClient{client: v1.NewPluginExecutorClient(conn)},
		RegisteredAt:  time.Now(),
		LastHeartbeat: time.Now(),
	}

	return pluginID, nil
}

type grpcClient struct{ client v1.PluginExecutorClient }

func (c *grpcClient) Invoke(ctx context.Context, req *InvokeRequest) (*InvokeResponse, error) {
	payload := &structpb.Struct{Fields: map[string]*structpb.Value{}}
	if len(req.Input) > 0 {
		var object map[string]interface{}
		if err := json.Unmarshal(req.Input, &object); err != nil {
			return nil, fmt.Errorf("invalid plugin input: %w", err)
		}
		payload, _ = structpb.NewStruct(object)
	}
	resp, err := c.client.Invoke(ctx, &v1.InvokeRequest{
		ProviderId: req.ProviderID, Resource: req.Kind, ResourceVersion: req.Version,
		Operation: req.Operation, Payload: payload, Metadata: req.Metadata,
	})
	if err != nil {
		return nil, err
	}
	output, _ := json.Marshal(map[string]interface{}{})
	if resp.Result != nil {
		output, _ = json.Marshal(resp.Result.AsMap())
	}
	var pluginErr *OrionError
	if resp.Error != nil {
		pluginErr = &OrionError{Code: resp.Error.Code, Message: resp.Error.Message}
	}
	return &InvokeResponse{Success: resp.Success, Output: output, Error: pluginErr}, nil
}

func (c *grpcClient) HealthCheck(ctx context.Context, req *HealthCheckRequest) (*HealthCheckResponse, error) {
	resp, err := c.client.HealthCheck(ctx, &v1.HealthCheckRequest{})
	if err != nil {
		return nil, err
	}
	state := "UNHEALTHY"
	if resp.Healthy {
		state = "HEALTHY"
	}
	return &HealthCheckResponse{Status: state, Details: map[string]string{"message": resp.Message}}, nil
}

func (r *PluginRegistry) Unregister(ctx context.Context, pluginID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	p, ok := r.plugins[pluginID]
	if !ok {
		return fmt.Errorf("plugin not found: %s", pluginID)
	}
	p.Conn.Close()
	delete(r.plugins, pluginID)
	return nil
}

func (r *PluginRegistry) GetPlugin(ctx context.Context, pluginID string) (*RegisteredPlugin, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	p, ok := r.plugins[pluginID]
	if !ok {
		return nil, fmt.Errorf("plugin not found: %s", pluginID)
	}
	return p, nil
}

func (r *PluginRegistry) ListPlugins(ctx context.Context) []*RegisteredPlugin {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*RegisteredPlugin, 0, len(r.plugins))
	for _, p := range r.plugins {
		result = append(result, p)
	}
	return result
}

func (r *PluginRegistry) UpdateHeartbeat(ctx context.Context, pluginID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	p, ok := r.plugins[pluginID]
	if !ok {
		return fmt.Errorf("plugin not found: %s", pluginID)
	}
	p.LastHeartbeat = time.Now()
	return nil
}

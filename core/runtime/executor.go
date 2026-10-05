package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"google.golang.org/protobuf/types/known/structpb"

	"github.com/horizon/orion/core/domain"
	"github.com/horizon/orion/core/node"
	"github.com/horizon/orion/sdk/go/plugin"
	v1 "github.com/horizon/orion/sdk/go/proto/plugin/v1"
)

type NodePluginExecutor interface {
	Execute(ctx context.Context, providerID string, op string, payload []byte) (*domain.Operation, error)
	ExecuteOnNode(ctx context.Context, nodeID, providerID string, op string, payload []byte) (*domain.Operation, error)
	AttachToNode(ctx context.Context, nodeID string, providerID string, endpoint string) error
	DetachFromNode(ctx context.Context, nodeID string, providerID string) error
	GetNodeForProvider(ctx context.Context, providerID string) (*node.Node, error)
}

type multiNodePluginExecutor struct {
	mu               sync.RWMutex
	nodeRegistry     node.NodeRegistry
	providerRegistry ProviderRegistry
	connections      map[string]map[string]*plugin.Client
}

type ProviderRegistry interface {
	Get(ctx context.Context, providerID string) (*domain.Provider, error)
	List(ctx context.Context) ([]*domain.Provider, error)
}

func NewMultiNodePluginExecutor(nodeRegistry node.NodeRegistry, providerRegistry ProviderRegistry) NodePluginExecutor {
	return &multiNodePluginExecutor{
		nodeRegistry:     nodeRegistry,
		providerRegistry: providerRegistry,
		connections:      make(map[string]map[string]*plugin.Client),
	}
}

func (e *multiNodePluginExecutor) Execute(ctx context.Context, providerID string, op string, payload []byte) (*domain.Operation, error) {
	provider, err := e.providerRegistry.Get(ctx, providerID)
	if err != nil {
		return nil, fmt.Errorf("provider not found: %w", err)
	}

	if provider.NodeID == "" {
		return nil, fmt.Errorf("provider %s is not assigned to any node", providerID)
	}

	return e.ExecuteOnNode(ctx, provider.NodeID, providerID, op, payload)
}

func (e *multiNodePluginExecutor) ExecuteOnNode(ctx context.Context, nodeID, providerID string, op string, payload []byte) (*domain.Operation, error) {
	e.mu.RLock()
	nodeConnections, ok := e.connections[nodeID]
	client, connected := nodeConnections[providerID]
	e.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("no plugins registered on node %s", nodeID)
	}
	if !connected {
		return nil, fmt.Errorf("provider %s not found on node %s", providerID, nodeID)
	}

	provider, err := e.providerRegistry.Get(ctx, providerID)
	if err != nil {
		return nil, fmt.Errorf("provider not found: %w", err)
	}

	kind := getResourceKind(provider.PluginID)
	var payloadMap map[string]interface{}
	if len(payload) == 0 {
		payloadMap = map[string]interface{}{}
	} else if err := json.Unmarshal(payload, &payloadMap); err != nil {
		return nil, fmt.Errorf("invalid plugin payload: %w", err)
	}
	payloadStruct, err := structpb.NewStruct(payloadMap)
	if err != nil {
		return nil, fmt.Errorf("invalid plugin payload: %w", err)
	}

	now := time.Now()
	resp, err := client.Invoke(ctx, &v1.InvokeRequest{
		RequestId:       fmt.Sprintf("req-%d", now.UnixNano()),
		ProviderId:      providerID,
		Resource:        kind,
		ResourceVersion: "v1",
		Operation:       op,
		Payload:         payloadStruct,
	})
	if err != nil {
		return &domain.Operation{
			OperationID:   fmt.Sprintf("op-%d", now.UnixNano()),
			ProviderID:    providerID,
			OperationName: op,
			State:         domain.OperationStateFailed,
			ErrorMessage:  err.Error(),
			CompletedAt:   &now,
		}, nil
	}
	if !resp.GetSuccess() {
		message := "plugin operation failed"
		if resp.GetError() != nil && resp.GetError().GetMessage() != "" {
			message = resp.GetError().GetMessage()
		}
		return &domain.Operation{
			OperationID:   fmt.Sprintf("op-%d", now.UnixNano()),
			ProviderID:    providerID,
			OperationName: op,
			State:         domain.OperationStateFailed,
			ErrorMessage:  message,
			CompletedAt:   &now,
		}, nil
	}

	return &domain.Operation{
		OperationID:   fmt.Sprintf("op-%d", now.UnixNano()),
		ProviderID:    providerID,
		OperationName: op,
		State:         domain.OperationStateSucceeded,
		CompletedAt:   &now,
	}, nil
}

func (e *multiNodePluginExecutor) AttachToNode(ctx context.Context, nodeID string, providerID string, endpoint string) error {
	n, err := e.nodeRegistry.Get(ctx, nodeID)
	if err != nil {
		return fmt.Errorf("node not found: %w", err)
	}

	pluginEndpoint := strings.TrimSpace(endpoint)
	if pluginEndpoint == "" {
		pluginEndpoint = fmt.Sprintf("%s:%d", n.Address, n.Port)
	}
	pluginEndpoint = strings.TrimPrefix(pluginEndpoint, "grpc://")
	client, err := plugin.NewClient(ctx, plugin.ClientConfig{
		Endpoint: pluginEndpoint,
	})
	if err != nil {
		return fmt.Errorf("failed to create plugin client: %w", err)
	}

	e.mu.Lock()
	if e.connections[nodeID] == nil {
		e.connections[nodeID] = make(map[string]*plugin.Client)
	}
	if previous := e.connections[nodeID][providerID]; previous != nil {
		_ = previous.Close()
	}
	e.connections[nodeID][providerID] = client
	e.mu.Unlock()

	return nil
}

func (e *multiNodePluginExecutor) DetachFromNode(ctx context.Context, nodeID string, providerID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if nodeConnections, ok := e.connections[nodeID]; ok {
		if client, ok := nodeConnections[providerID]; ok {
			client.Close()
			delete(nodeConnections, providerID)
		}
	}
	return nil
}

func (e *multiNodePluginExecutor) GetNodeForProvider(ctx context.Context, providerID string) (*node.Node, error) {
	provider, err := e.providerRegistry.Get(ctx, providerID)
	if err != nil {
		return nil, err
	}

	if provider.NodeID == "" {
		return nil, fmt.Errorf("provider %s has no node assigned", providerID)
	}

	return e.nodeRegistry.Get(ctx, provider.NodeID)
}

func getResourceKind(pluginID string) string {
	pluginKinds := map[string]string{
		"zfs-plugin":           "orion.io/storage.volume",
		"ceph-plugin":          "orion.io/storage.volume",
		"cephfs-plugin":        "orion.io/storage.volume",
		"glusterfs-plugin":     "orion.io/storage.volume",
		"nfs-plugin":           "orion.io/storage.volume",
		"iscsi-plugin":         "orion.io/storage.volume",
		"nvmeof-plugin":        "orion.io/storage.volume",
		"smb-plugin":           "orion.io/storage.share",
		"kvm-plugin":           "orion.io/compute.instance",
		"openbao-plugin":       "orion.io/secret.secret",
		"dns-plugin":           "orion.io/dns.zone",
		"s3-plugin":            "orion.io/storage.object",
		"image-plugin":         "orion.io/image.image",
		"backup-plugin":        "orion.io/backup.backup",
		"network-plugin":       "orion.io/network.network",
		"loadbalancer-plugin":  "orion.io/loadbalancer.lb",
		"dbaas-plugin":         "orion.io/database.instance",
		"openbao-keys-plugin":  "orion.io/secret.key",
		"containers-plugin":    "orion.io/containers.pod",
		"orchestration-plugin": "orion.io/orchestration.stack",
		"baremetal-plugin":     "orion.io/compute.baremetal",
		"messaging-plugin":     "orion.io/messaging.queue",
		"telemetry-plugin":     "orion.io/telemetry.metrics",
	}

	if kind, ok := pluginKinds[pluginID]; ok {
		return kind
	}
	return "orion.io/resource"
}

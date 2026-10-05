package node

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type NodeStatus string

const (
	NodeStatusUnknown   NodeStatus = "unknown"
	NodeStatusOnline    NodeStatus = "online"
	NodeStatusOffline   NodeStatus = "offline"
	NodeStatusDraining  NodeStatus = "draining"
)

type Node struct {
	ID           string
	Name         string
	Address      string
	Port         int
	Status       NodeStatus
	Capabilities map[string]interface{}
	Plugins      []string
	Resources    NodeResources
	LastSeen     time.Time
	Metadata     map[string]string
}

type NodeResources struct {
	CPU      int64
	Memory   int64
	Storage  int64
	Instances int
}

type NodeRegistry interface {
	Register(ctx context.Context, node *Node) error
	Deregister(ctx context.Context, nodeID string) error
	Get(ctx context.Context, nodeID string) (*Node, error)
	List(ctx context.Context) ([]*Node, error)
	ListByCapability(ctx context.Context, capability string) ([]*Node, error)
	UpdateStatus(ctx context.Context, nodeID string, status NodeStatus) error
	UpdatePlugins(ctx context.Context, nodeID string, plugins []string) error
	Heartbeat(ctx context.Context, nodeID string) error
}

type inMemoryNodeRegistry struct {
	mu    sync.RWMutex
	nodes map[string]*Node
}

func NewInMemoryRegistry() NodeRegistry {
	return &inMemoryNodeRegistry{
		nodes: make(map[string]*Node),
	}
}

func (r *inMemoryNodeRegistry) Register(ctx context.Context, node *Node) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	node.LastSeen = time.Now()
	r.nodes[node.ID] = node
	return nil
}

func (r *inMemoryNodeRegistry) Deregister(ctx context.Context, nodeID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.nodes, nodeID)
	return nil
}

func (r *inMemoryNodeRegistry) Get(ctx context.Context, nodeID string) (*Node, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	node, ok := r.nodes[nodeID]
	if !ok {
		return nil, fmt.Errorf("node not found: %s", nodeID)
	}
	return node, nil
}

func (r *inMemoryNodeRegistry) List(ctx context.Context) ([]*Node, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*Node, 0, len(r.nodes))
	for _, node := range r.nodes {
		result = append(result, node)
	}
	return result, nil
}

func (r *inMemoryNodeRegistry) ListByCapability(ctx context.Context, capability string) ([]*Node, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*Node, 0)
	for _, node := range r.nodes {
		if node.Status != NodeStatusOnline {
			continue
		}
		if _, ok := node.Capabilities[capability]; ok {
			result = append(result, node)
		}
	}
	return result, nil
}

func (r *inMemoryNodeRegistry) UpdateStatus(ctx context.Context, nodeID string, status NodeStatus) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	node, ok := r.nodes[nodeID]
	if !ok {
		return fmt.Errorf("node not found: %s", nodeID)
	}
	node.Status = status
	node.LastSeen = time.Now()
	return nil
}

func (r *inMemoryNodeRegistry) UpdatePlugins(ctx context.Context, nodeID string, plugins []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	node, ok := r.nodes[nodeID]
	if !ok {
		return fmt.Errorf("node not found: %s", nodeID)
	}
	node.Plugins = plugins
	node.LastSeen = time.Now()
	return nil
}

func (r *inMemoryNodeRegistry) Heartbeat(ctx context.Context, nodeID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	node, ok := r.nodes[nodeID]
	if !ok {
		return fmt.Errorf("node not found: %s", nodeID)
	}
	node.Status = NodeStatusOnline
	node.LastSeen = time.Now()
	return nil
}

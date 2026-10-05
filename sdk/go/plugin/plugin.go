package plugin

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

type Config struct {
	ID      string
	Name    string
	Version string
	Vendor  string
}

type Plugin struct {
	config        Config
	mu            sync.RWMutex
	resources     map[string]*ResourceHandler
	relationships map[string]*RelationshipHandler
}

type ResourceHandler struct {
	Kind         string
	Version      string
	Ops          map[string]ResourceOperationHandler
	Capabilities map[string]interface{}
}

type ResourceOperationHandler func(ctx context.Context, req *ResourceRequest) (*ResourceResponse, error)

type RelationshipHandler struct {
	Kind string
	Role string
	Ops  map[string]RelationshipOperationHandler
}

type RelationshipOperationHandler func(ctx context.Context, req *RelationshipRequest) (*RelationshipResponse, error)

type ResourceRequest struct {
	RequestID  string
	ProviderID string
	Operation  string
	Payload    []byte
}

type ResourceResponse struct {
	Success bool
	Result  []byte
	Error   *Error
}

type RelationshipRequest struct {
	RequestID        string
	ProviderID       string
	Operation        string
	SourceResourceID string
	TargetResourceID string
	Payload          []byte
}

type RelationshipResponse struct {
	Success bool
	Result  []byte
	Error   *Error
}

type Error struct {
	Code    string
	Message string
}

func New(cfg Config) *Plugin {
	return &Plugin{
		config:        cfg,
		resources:     make(map[string]*ResourceHandler),
		relationships: make(map[string]*RelationshipHandler),
	}
}

func (p *Plugin) Resource(kind, version string) *ResourceBuilder {
	return &ResourceBuilder{
		plugin: p,
		handler: &ResourceHandler{
			Kind:         kind,
			Version:      version,
			Ops:          make(map[string]ResourceOperationHandler),
			Capabilities: make(map[string]interface{}),
		},
	}
}

type ResourceBuilder struct {
	plugin  *Plugin
	handler *ResourceHandler
}

func (rb *ResourceBuilder) Handle(operation string, h ResourceOperationHandler) *ResourceBuilder {
	rb.handler.Ops[operation] = h
	return rb
}

func (rb *ResourceBuilder) AddCapability(name string, value interface{}) *ResourceBuilder {
	rb.handler.Capabilities[name] = value
	return rb
}

func (rb *ResourceBuilder) Register() {
	rb.plugin.mu.Lock()
	defer rb.plugin.mu.Unlock()
	rb.plugin.resources[rb.handler.Kind] = rb.handler
}

func (p *Plugin) Relationship(kind, role string) *RelationshipBuilder {
	return &RelationshipBuilder{
		plugin: p,
		handler: &RelationshipHandler{
			Kind: kind,
			Role: role,
			Ops:  make(map[string]RelationshipOperationHandler),
		},
	}
}

type RelationshipBuilder struct {
	plugin  *Plugin
	handler *RelationshipHandler
}

func (rb *RelationshipBuilder) Handle(operation string, h RelationshipOperationHandler) *RelationshipBuilder {
	rb.handler.Ops[operation] = h
	return rb
}

func (rb *RelationshipBuilder) Register() {
	rb.plugin.mu.Lock()
	defer rb.plugin.mu.Unlock()
	rb.plugin.relationships[rb.handler.Kind] = rb.handler
}

func (p *Plugin) GetResourceHandler(kind string) (*ResourceHandler, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	h, ok := p.resources[kind]
	if !ok {
		return nil, fmt.Errorf("no handler registered for resource kind: %s", kind)
	}
	return h, nil
}

func (p *Plugin) GetRelationshipHandler(kind string) (*RelationshipHandler, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	h, ok := p.relationships[kind]
	if !ok {
		return nil, fmt.Errorf("no handler registered for relationship kind: %s", kind)
	}
	return h, nil
}

func (p *Plugin) GetConfig() Config {
	return p.config
}

func (p *Plugin) ListResources() []*ResourceHandler {
	p.mu.RLock()
	defer p.mu.RUnlock()
	result := make([]*ResourceHandler, 0, len(p.resources))
	for _, h := range p.resources {
		result = append(result, h)
	}
	return result
}

func (p *Plugin) ListRelationships() []*RelationshipHandler {
	p.mu.RLock()
	defer p.mu.RUnlock()
	result := make([]*RelationshipHandler, 0, len(p.relationships))
	for _, h := range p.relationships {
		result = append(result, h)
	}
	return result
}

// Manifest returns a deterministic, machine-readable description of the
// plugin and all handlers registered with it. It is safe to call while the
// plugin is serving requests.
func (p *Plugin) Manifest() map[string]interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()

	manifest := NewManifest(p.config)
	resourceKinds := make([]string, 0, len(p.resources))
	for kind := range p.resources {
		resourceKinds = append(resourceKinds, kind)
	}
	sort.Strings(resourceKinds)
	for _, kind := range resourceKinds {
		h := p.resources[kind]
		ops := make([]string, 0, len(h.Ops))
		for op := range h.Ops {
			ops = append(ops, op)
		}
		sort.Strings(ops)
		capabilities := make(map[string]interface{}, len(h.Capabilities))
		for name, value := range h.Capabilities {
			capabilities[name] = value
		}
		manifest.AddResource(h.Kind, h.Version, ops, capabilities)
	}

	relationshipKinds := make([]string, 0, len(p.relationships))
	for kind := range p.relationships {
		relationshipKinds = append(relationshipKinds, kind)
	}
	sort.Strings(relationshipKinds)
	for _, kind := range relationshipKinds {
		h := p.relationships[kind]
		ops := make([]string, 0, len(h.Ops))
		for op := range h.Ops {
			ops = append(ops, op)
		}
		sort.Strings(ops)
		manifest.AddRelationship(h.Kind, "v1", h.Role, ops)
	}

	return manifest.Build()
}

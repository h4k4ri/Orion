package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/google/uuid"
	"github.com/horizon/orion/core/domain"
	"github.com/horizon/orion/core/ports"
)

type Registry struct {
	mu       sync.RWMutex
	providers map[string]*domain.Provider
	providerRepo ports.ProviderRepository
}

func NewRegistry(providerRepo ports.ProviderRepository) *Registry {
	return &Registry{
		providers:  make(map[string]*domain.Provider),
		providerRepo: providerRepo,
	}
}

func (r *Registry) Register(ctx context.Context, pluginID, name, endpoint string, config json.RawMessage) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	providerID := uuid.New().String()

	provider := &domain.Provider{
		ProviderID:    providerID,
		PluginID:      pluginID,
		Name:          name,
		Endpoint:      endpoint,
		Config:        config,
		AdministrativeState: domain.ProviderStateEnabled,
		HealthState:   domain.HealthStateUnknown,
		Generation:    1,
	}

	if err := r.providerRepo.Create(ctx, provider); err != nil {
		return "", fmt.Errorf("failed to persist provider: %w", err)
	}

	r.providers[providerID] = provider
	return providerID, nil
}

func (r *Registry) Unregister(ctx context.Context, providerID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	_, ok := r.providers[providerID]
	if !ok {
		return fmt.Errorf("provider not found: %s", providerID)
	}

	if err := r.providerRepo.Delete(ctx, providerID); err != nil {
		return fmt.Errorf("failed to delete provider: %w", err)
	}

	delete(r.providers, providerID)
	return nil
}

func (r *Registry) Get(ctx context.Context, providerID string) (*domain.Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	p, ok := r.providers[providerID]
	if !ok {
		return nil, fmt.Errorf("provider not found: %s", providerID)
	}
	return p, nil
}

func (r *Registry) List(ctx context.Context) []*domain.Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*domain.Provider, 0, len(r.providers))
	for _, p := range r.providers {
		result = append(result, p)
	}
	return result
}

func (r *Registry) UpdateHealth(ctx context.Context, providerID string, state domain.HealthState) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	p, ok := r.providers[providerID]
	if !ok {
		return fmt.Errorf("provider not found: %s", providerID)
	}

	p.HealthState = state
	p.ObservedGeneration = p.Generation
	return r.providerRepo.Update(ctx, p)
}

func (r *Registry) SetEnabled(ctx context.Context, providerID string, enabled bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	p, ok := r.providers[providerID]
	if !ok {
		return fmt.Errorf("provider not found: %s", providerID)
	}

	if enabled {
		p.AdministrativeState = domain.ProviderStateEnabled
	} else {
		p.AdministrativeState = domain.ProviderStateDisabled
	}
	p.Generation++

	return r.providerRepo.Update(ctx, p)
}

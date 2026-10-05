package main

import (
	"context"
	"slices"
	"sync"

	"github.com/horizon/orion/services/orion-placement/internal/domain"
	"github.com/horizon/orion/services/orion-placement/internal/ports"
)

type memoryPlacementStore struct {
	mu        sync.Mutex
	hosts     map[string]domain.Host
	providers map[string]domain.ResourceProvider
}

func newMemoryPlacementStore() ports.PlacementStore {
	m := &memoryPlacementStore{
		hosts:     map[string]domain.Host{},
		providers: map[string]domain.ResourceProvider{},
	}
	// Seed default host.
	m.hosts["host_local"] = domain.Host{
		HostID:              "host_local",
		CellID:              "cell_local",
		Group:               "general",
		Enabled:             true,
		Drained:             false,
		NodeAgentURL:        "http://127.0.0.1:8084",
		VolumeHostAgentURL:  "grpc://127.0.0.1:50055",
		NetworkHostAgentURL: "grpc://127.0.0.1:50054",
		Traits:              []string{"general", "kvm"},
		Inventory: domain.Inventory{
			VCPUsTotal:    8,
			MemoryMBTotal: 16384,
			DiskGBTotal:   500,
		},
		Generation: 1,
	}
	return m
}

func (m *memoryPlacementStore) SaveHost(_ context.Context, h domain.Host) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	clone := h
	clone.Traits = slices.Clone(h.Traits)
	m.hosts[h.HostID] = clone
	return nil
}

func (m *memoryPlacementStore) GetHost(_ context.Context, hostID string) (domain.Host, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	h, ok := m.hosts[hostID]
	if !ok {
		return domain.Host{}, ports.ErrHostRecordNotFound
	}
	return h, nil
}

func (m *memoryPlacementStore) ListHosts(_ context.Context) ([]domain.Host, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	items := make([]domain.Host, 0, len(m.hosts))
	for _, h := range m.hosts {
		items = append(items, h)
	}
	return items, nil
}

func (m *memoryPlacementStore) UpdateAllocation(_ context.Context, req domain.AllocationUpdate) (domain.Host, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	h, ok := m.hosts[req.HostID]
	if !ok {
		return domain.Host{}, ports.ErrHostRecordNotFound
	}
	if h.Generation != req.ExpectedGen {
		return domain.Host{}, ports.ErrConcurrentUpdate
	}

	h.Inventory.VCPUsAllocated += req.DeltaVCPUs
	h.Inventory.MemoryAllocatedMB += req.DeltaMemoryMB
	h.Inventory.DiskAllocatedGB += req.DeltaDiskGB
	h.Generation++
	m.hosts[h.HostID] = h
	return h, nil
}

func (m *memoryPlacementStore) CreateReservation(_ context.Context, r domain.Reservation) error {
	return nil
}

func (m *memoryPlacementStore) GetReservation(_ context.Context, id string) (domain.Reservation, error) {
	return domain.Reservation{}, nil
}

func (m *memoryPlacementStore) DeleteReservation(_ context.Context, id string) error {
	return nil
}

func (m *memoryPlacementStore) DeleteReservationByHostAndProject(_ context.Context, hostID, projectID string) error {
	return nil
}

func (m *memoryPlacementStore) ListReservationsByProject(_ context.Context, projectID string) ([]domain.Reservation, error) {
	return nil, nil
}

func (m *memoryPlacementStore) CountProjectInstancesOnHost(_ context.Context, hostID, projectID string) (int, error) {
	return 0, nil
}

func (m *memoryPlacementStore) CleanupExpiredReservations(_ context.Context) (int64, error) {
	return 0, nil
}

func (m *memoryPlacementStore) SaveResourceProvider(_ context.Context, provider domain.ResourceProvider) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.providers[provider.UUID] = provider
	return nil
}
func (m *memoryPlacementStore) GetResourceProvider(_ context.Context, uuid string) (domain.ResourceProvider, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	provider, ok := m.providers[uuid]
	if !ok {
		return domain.ResourceProvider{}, ports.ErrResourceProviderNotFound
	}
	return provider, nil
}
func (m *memoryPlacementStore) ListResourceProviders(_ context.Context, rootUUID string) ([]domain.ResourceProvider, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	items := make([]domain.ResourceProvider, 0)
	for _, provider := range m.providers {
		if rootUUID == "" || provider.RootProviderID == rootUUID {
			items = append(items, provider)
		}
	}
	return items, nil
}
func (m *memoryPlacementStore) DeleteResourceProvider(_ context.Context, uuid string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.providers, uuid)
	return nil
}

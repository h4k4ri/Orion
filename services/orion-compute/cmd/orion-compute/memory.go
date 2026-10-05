package main

import (
	"context"
	"sync"

	"github.com/horizon/orion/libs/go/kit/compute"
	"github.com/horizon/orion/libs/go/kit/task"
	"github.com/horizon/orion/services/orion-compute/internal/ports"
)

// memoryStore is a legacy in-memory implementation kept for isolated tests and tooling.
type memoryStore struct {
	mu      sync.RWMutex
	servers map[string]compute.Server
	tasks   map[string]task.Task
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		servers: map[string]compute.Server{},
		tasks:   map[string]task.Task{},
	}
}

func (m *memoryStore) SaveServer(_ context.Context, srv compute.Server) error {
	m.mu.Lock()
	m.servers[srv.ID] = srv
	m.mu.Unlock()
	return nil
}

func (m *memoryStore) GetServer(_ context.Context, id string) (compute.Server, error) {
	m.mu.RLock()
	srv, ok := m.servers[id]
	m.mu.RUnlock()
	if !ok {
		return compute.Server{}, ports.ErrServerNotFound
	}
	return srv, nil
}

func (m *memoryStore) ListServers(_ context.Context) ([]compute.Server, error) {
	m.mu.RLock()
	items := make([]compute.Server, 0, len(m.servers))
	for _, s := range m.servers {
		items = append(items, s)
	}
	m.mu.RUnlock()
	return items, nil
}

func (m *memoryStore) DeleteServer(_ context.Context, id string) error {
	m.mu.Lock()
	delete(m.servers, id)
	m.mu.Unlock()
	return nil
}

func (m *memoryStore) SaveTask(_ context.Context, t task.Task) error {
	m.mu.Lock()
	m.tasks[t.ID] = t
	m.mu.Unlock()
	return nil
}

func (m *memoryStore) GetTask(_ context.Context, id string) (task.Task, error) {
	m.mu.RLock()
	t, ok := m.tasks[id]
	m.mu.RUnlock()
	if !ok {
		return task.Task{}, ports.ErrTaskNotFound
	}
	return t, nil
}

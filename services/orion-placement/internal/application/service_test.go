package application

import (
	"context"
	"testing"

	"github.com/horizon/orion/services/orion-placement/internal/domain"
	"github.com/horizon/orion/services/orion-placement/internal/ports"
)

type placementStoreStub struct {
	host domain.Host
}

func (s *placementStoreStub) SaveHost(_ context.Context, host domain.Host) error {
	s.host = host
	return nil
}

func (s *placementStoreStub) GetHost(_ context.Context, hostID string) (domain.Host, error) {
	if s.host.HostID != hostID {
		return domain.Host{}, ports.ErrHostRecordNotFound
	}
	return s.host, nil
}

func (s *placementStoreStub) ListHosts(context.Context) ([]domain.Host, error) {
	return []domain.Host{s.host}, nil
}

func (s *placementStoreStub) UpdateAllocation(context.Context, domain.AllocationUpdate) (domain.Host, error) {
	return s.host, nil
}

func (s *placementStoreStub) CreateReservation(context.Context, domain.Reservation) error {
	return nil
}

func (s *placementStoreStub) GetReservation(context.Context, string) (domain.Reservation, error) {
	return domain.Reservation{}, nil
}

func (s *placementStoreStub) DeleteReservation(context.Context, string) error {
	return nil
}

func (s *placementStoreStub) DeleteReservationByHostAndProject(context.Context, string, string) error {
	return nil
}

func (s *placementStoreStub) ListReservationsByProject(context.Context, string) ([]domain.Reservation, error) {
	return nil, nil
}

func (s *placementStoreStub) CountProjectInstancesOnHost(context.Context, string, string) (int, error) {
	return 0, nil
}

func (s *placementStoreStub) CleanupExpiredReservations(context.Context) (int64, error) {
	return 0, nil
}

func TestRegisterHostNormalizesInventoryAndPreservesAllocations(t *testing.T) {
	store := &placementStoreStub{
		host: domain.Host{
			HostID:             "host-1",
			NodeAgentURL:       "http://host-1:8084",
			VolumeHostAgentURL: "http://host-1:8088",
			Generation:         4,
			Inventory: domain.Inventory{
				VCPUsAllocated:    2,
				MemoryAllocatedMB: 2048,
				DiskAllocatedGB:   10,
				NUMA: []domain.NUMANode{
					{ID: 0, MemoryAllocatedMB: 1024},
				},
				GPUs: []domain.GPUDevice{
					{ID: "gpu0", AllocatedTo: "srv_existing"},
				},
			},
		},
	}

	service := NewService(store)
	host := service.RegisterHost(context.Background(), domain.RegisterHostRequest{
		HostID:   "host-1",
		CellID:   "cell-a",
		Group:    "general",
		Enabled:  true,
		Drained:  false,
		Traits:   []string{" KVM ", "general", "kvm", "GENERAL"},
		VCPUs:    8,
		MemoryMB: 16384,
		DiskGB:   200,
		NUMA: []domain.NUMANode{
			{ID: 1, VCPUs: []int{5, 4}, MemoryMBTotal: 8192},
			{ID: 0, VCPUs: []int{1, 0}, MemoryMBTotal: 8192},
		},
		GPUs: []domain.GPUDevice{
			{ID: "gpu0", Vendor: "NVIDIA", Model: "T4", MemoryMB: 16384, Traits: []string{"gpu", "GPU"}},
		},
	})

	if host.Generation != 5 {
		t.Fatalf("expected generation 5, got %d", host.Generation)
	}
	if host.NodeAgentURL != "http://host-1:8084" {
		t.Fatalf("expected node agent url preserved, got %q", host.NodeAgentURL)
	}
	if host.VolumeHostAgentURL != "http://host-1:8088" {
		t.Fatalf("expected volume host agent url preserved, got %q", host.VolumeHostAgentURL)
	}
	if got, want := host.Traits, []string{"general", "kvm"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("unexpected traits: %#v", got)
	}
	if host.Inventory.VCPUsAllocated != 2 || host.Inventory.MemoryAllocatedMB != 2048 || host.Inventory.DiskAllocatedGB != 10 {
		t.Fatalf("expected top-level allocations preserved, got %#v", host.Inventory)
	}
	if len(host.Inventory.NUMA) != 2 || host.Inventory.NUMA[0].ID != 0 || host.Inventory.NUMA[0].MemoryAllocatedMB != 1024 {
		t.Fatalf("expected numa allocations preserved and sorted, got %#v", host.Inventory.NUMA)
	}
	if len(host.Inventory.NUMA[0].VCPUs) != 2 || host.Inventory.NUMA[0].VCPUs[0] != 0 || host.Inventory.NUMA[0].VCPUs[1] != 1 {
		t.Fatalf("expected numa vcpus sorted, got %#v", host.Inventory.NUMA[0].VCPUs)
	}
	if len(host.Inventory.GPUs) != 1 || host.Inventory.GPUs[0].AllocatedTo != "srv_existing" {
		t.Fatalf("expected gpu allocation preserved, got %#v", host.Inventory.GPUs)
	}
	if len(host.Inventory.GPUs[0].Traits) != 1 || host.Inventory.GPUs[0].Traits[0] != "gpu" {
		t.Fatalf("expected gpu traits normalized, got %#v", host.Inventory.GPUs[0].Traits)
	}
}

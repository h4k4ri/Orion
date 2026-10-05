package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/horizon/orion/libs/go/kit/compute"
	"github.com/horizon/orion/libs/go/kit/events"
	"github.com/horizon/orion/libs/go/kit/image"
	networkkit "github.com/horizon/orion/libs/go/kit/network"
	"github.com/horizon/orion/libs/go/kit/task"
	volumekit "github.com/horizon/orion/libs/go/kit/volume"
	"github.com/horizon/orion/services/orion-compute/internal/ports"
)

type computeStoreStub struct {
	server compute.Server
	task   task.Task
}

func (s *computeStoreStub) SaveServer(_ context.Context, server compute.Server) error {
	s.server = server
	return nil
}

func (s *computeStoreStub) GetServer(_ context.Context, id string) (compute.Server, error) {
	if s.server.ID != id {
		return compute.Server{}, ports.ErrServerNotFound
	}
	return s.server, nil
}

func (s *computeStoreStub) ListServers(context.Context) ([]compute.Server, error) {
	return []compute.Server{s.server}, nil
}

func (s *computeStoreStub) DeleteServer(context.Context, string) error {
	s.server = compute.Server{}
	return nil
}

func (s *computeStoreStub) CreateTaskIfAbsent(_ context.Context, t task.Task) (task.Task, bool, error) {
	s.task = t
	return t, true, nil
}

func (s *computeStoreStub) SaveTask(_ context.Context, t task.Task) error {
	s.task = t
	return nil
}

func (s *computeStoreStub) GetTask(_ context.Context, id string) (task.Task, error) {
	if s.task.ID != id {
		return task.Task{}, ports.ErrTaskNotFound
	}
	return s.task, nil
}

func (s *computeStoreStub) GetTaskByRequest(context.Context, string, string) (task.Task, error) {
	return task.Task{}, ports.ErrTaskNotFound
}

type publisherStub struct{}

func (publisherStub) PublishTask(context.Context, events.TaskEvent) error         { return nil }
func (publisherStub) PublishResource(context.Context, events.ResourceEvent) error { return nil }

type imageResolverStub struct{}

func (imageResolverStub) GetImage(context.Context, string) (image.Image, error) {
	return image.Image{
		ID:        "img-1",
		MinDiskGB: 1,
		Path:      "/tmp/cirros.qcow2",
	}, nil
}

type networkAllocatorStub struct {
	created int
	deleted []string
}

func (n *networkAllocatorStub) CreatePort(_ context.Context, req networkkit.CreatePortRequest) (networkkit.Port, error) {
	n.created++
	if n.created == 1 {
		return networkkit.Port{
			ID:            "port-1",
			NetworkID:     req.NetworkID,
			BindingHostID: req.BindingHostID,
		}, nil
	}
	return networkkit.Port{}, ports.ErrNetworkBackendUnavailable
}

func (n *networkAllocatorStub) DeletePort(_ context.Context, portID string) error {
	n.deleted = append(n.deleted, portID)
	return nil
}

type volumeManagerStub struct{}

func (volumeManagerStub) GetVolume(context.Context, string) (volumekit.Volume, error) {
	return volumekit.Volume{}, nil
}
func (volumeManagerStub) CreateVolume(context.Context, volumekit.CreateVolumeRequest) (volumekit.Volume, error) {
	return volumekit.Volume{}, nil
}
func (volumeManagerStub) DeleteVolume(context.Context, string) error { return nil }
func (volumeManagerStub) AttachVolume(context.Context, string, string, string) (volumekit.Volume, error) {
	return volumekit.Volume{}, nil
}
func (volumeManagerStub) DetachVolume(context.Context, string) (volumekit.Volume, error) {
	return volumekit.Volume{}, nil
}

type selectorStub struct {
	releases []ports.ReleaseHostRequest
}

func (s *selectorStub) SelectHost(context.Context, ports.SelectHostRequest) (ports.HostSelection, error) {
	return ports.HostSelection{CellID: "cell-local", HostID: "host-local"}, nil
}

func (s *selectorStub) ReleaseHost(_ context.Context, req ports.ReleaseHostRequest) error {
	s.releases = append(s.releases, req)
	return nil
}

type executorStub struct{}

func (executorStub) BuildServer(context.Context, compute.Server, image.Image, []networkkit.Port, int, int, int) (compute.Server, error) {
	return compute.Server{}, errors.New("not used")
}

func (executorStub) ObserveServer(context.Context, compute.Server) (ports.ServerObservation, error) {
	return ports.ServerObservation{}, nil
}

func (executorStub) DeleteServer(context.Context, compute.Server) error {
	return nil
}

func (executorStub) AttachVolume(context.Context, compute.Server, volumekit.Volume) error {
	return nil
}

func (executorStub) DetachVolume(context.Context, compute.Server, volumekit.Volume) error {
	return nil
}

func TestCreateServerFailureCleansAllocatedResources(t *testing.T) {
	store := &computeStoreStub{}
	networks := &networkAllocatorStub{}
	selector := &selectorStub{}

	service := NewService(
		store,
		publisherStub{},
		nil,
		nil,
		imageResolverStub{},
		networks,
		volumeManagerStub{},
		selector,
		executorStub{},
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, taskResult, err := service.CreateServer(ctx, "usr-1", "proj-1", compute.CreateServerRequest{
		Name:     "srv-a",
		ImageID:  "img-1",
		Flavor:   "tiny",
		Networks: []string{"net-a", "net-b"},
	})
	if !errors.Is(err, ErrNetworkBackendUnavailable) {
		t.Fatalf("expected ErrNetworkBackendUnavailable, got %v", err)
	}
	if taskResult.ErrorCode != "network_backend_unavailable" {
		t.Fatalf("unexpected task error code: %s", taskResult.ErrorCode)
	}
	if store.server.Status != "error" {
		t.Fatalf("expected server status error, got %s", store.server.Status)
	}
	if len(networks.deleted) != 1 || networks.deleted[0] != "port-1" {
		t.Fatalf("expected created port to be cleaned up, got %#v", networks.deleted)
	}
	if len(selector.releases) != 1 || selector.releases[0].HostID != "host-local" {
		t.Fatalf("expected host release on failure, got %#v", selector.releases)
	}
}

package application

import (
	"context"
	"errors"
	"testing"

	"github.com/horizon/orion/libs/go/kit/events"
	volumekit "github.com/horizon/orion/libs/go/kit/volume"
	"github.com/horizon/orion/services/orion-volume/internal/ports"
)

type volumeStoreStub struct {
	volume volumekit.Volume
}

func (s *volumeStoreStub) SaveVolume(_ context.Context, v volumekit.Volume) error {
	s.volume = v
	return nil
}

func (s *volumeStoreStub) GetVolume(_ context.Context, id string) (volumekit.Volume, error) {
	if s.volume.ID != id {
		return volumekit.Volume{}, ports.ErrVolumeRecordNotFound
	}
	return s.volume, nil
}

func (s *volumeStoreStub) ListVolumes(context.Context) ([]volumekit.Volume, error) {
	if s.volume.ID == "" {
		return nil, nil
	}
	return []volumekit.Volume{s.volume}, nil
}

func (s *volumeStoreStub) DeleteVolume(context.Context, string) error {
	s.volume = volumekit.Volume{}
	return nil
}

type volumeHostAgentStub struct {
	createHostID string
}

func (s *volumeHostAgentStub) CreateVolume(_ context.Context, hostID string, req ports.CreateVolumeRequest) (ports.CreateVolumeResult, error) {
	s.createHostID = hostID
	return ports.CreateVolumeResult{DevicePath: "/dev/orion/" + req.VolumeID}, nil
}

func (s *volumeHostAgentStub) DeleteVolume(context.Context, string, string) error {
	return nil
}

func (s *volumeHostAgentStub) ObserveVolume(context.Context, string, string) (ports.VolumeObservation, error) {
	return ports.VolumeObservation{Exists: true}, nil
}
func (s *volumeHostAgentStub) CreateSnapshot(context.Context, string, string, string) error {
	return nil
}
func (s *volumeHostAgentStub) DeleteSnapshot(context.Context, string, string, string) error {
	return nil
}
func (s *volumeHostAgentStub) RestoreSnapshot(context.Context, string, string, string) error {
	return nil
}

type volumeHostDirectoryStub struct {
	hosts     []ports.HostRecord
	err       error
	selection ports.HostSelection
	selectErr error
	releases  []ports.ReleaseHostRequest
}

func (s volumeHostDirectoryStub) GetHost(context.Context, string) (ports.HostRecord, error) {
	return ports.HostRecord{}, errors.New("not implemented")
}

func (s volumeHostDirectoryStub) ListHosts(context.Context) ([]ports.HostRecord, error) {
	return s.hosts, s.err
}

func (s volumeHostDirectoryStub) SelectHost(context.Context, ports.SelectHostRequest) (ports.HostSelection, error) {
	return s.selection, s.selectErr
}

func (s volumeHostDirectoryStub) ReleaseHost(context.Context, ports.ReleaseHostRequest) error {
	return nil
}

type volumePublisherStub struct{}

func (volumePublisherStub) PublishTask(context.Context, events.TaskEvent) error         { return nil }
func (volumePublisherStub) PublishResource(context.Context, events.ResourceEvent) error { return nil }

func TestCreateVolumeSelectsEligibleHost(t *testing.T) {
	store := &volumeStoreStub{}
	hostAgent := &volumeHostAgentStub{}
	service := NewService(
		store,
		hostAgent,
		volumeHostDirectoryStub{
			selection: ports.HostSelection{
				CellID: "cell_local",
				HostID: "host-a",
			},
		},
		volumePublisherStub{},
		"cell_local",
	)

	volume, err := service.CreateVolume(context.Background(), volumekit.CreateVolumeRequest{
		ProjectID: "proj-1",
		Name:      "data-1",
		SizeGB:    10,
	})
	if err != nil {
		t.Fatalf("CreateVolume returned error: %v", err)
	}

	if volume.HostID != "host-a" {
		t.Fatalf("expected host-a, got %q", volume.HostID)
	}
	if hostAgent.createHostID != "host-a" {
		t.Fatalf("expected create routed to host-a, got %q", hostAgent.createHostID)
	}
}

func TestAttachVolumeRejectsDifferentHost(t *testing.T) {
	store := &volumeStoreStub{
		volume: volumekit.Volume{
			ID:     "vol-1",
			HostID: "host-a",
			Status: "available",
		},
	}
	service := NewService(
		store,
		&volumeHostAgentStub{},
		volumeHostDirectoryStub{},
		volumePublisherStub{},
		"cell_local",
	)

	_, err := service.AttachVolume(context.Background(), "vol-1", "srv-1", "host-b")
	if !errors.Is(err, ErrHostMismatch) {
		t.Fatalf("expected ErrHostMismatch, got %v", err)
	}
}

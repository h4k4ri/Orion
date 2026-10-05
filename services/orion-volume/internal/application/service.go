package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/horizon/orion/libs/go/kit/events"
	"github.com/horizon/orion/libs/go/kit/httpx"
	"github.com/horizon/orion/libs/go/kit/ids"
	volumekit "github.com/horizon/orion/libs/go/kit/volume"
	"github.com/horizon/orion/services/orion-volume/internal/ports"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

var (
	ErrBackendUnavailable  = errors.New("volume backend unavailable")
	ErrVolumeNotFound      = errors.New("volume not found")
	ErrInvalidSize         = errors.New("invalid volume size")
	ErrVolumeInUse         = errors.New("volume in use")
	ErrVolumeNotAttached   = errors.New("volume not attached")
	ErrNoEligibleHost      = errors.New("no eligible volume host")
	ErrHostMismatch        = errors.New("volume is bound to a different host")
	ErrQuotaExceeded       = ports.ErrQuotaExceeded
	ErrSnapshotNotFound    = errors.New("volume snapshot not found")
	ErrSnapshotInUse       = errors.New("volume snapshot is being created")
	ErrSnapshotUnsupported = errors.New("volume snapshots are not supported")
)

type Service struct {
	store     ports.VolumeStore
	host      ports.HostAgent
	hosts     ports.HostDirectory
	publisher events.Publisher
	cellID    string
}

var tracer = otel.Tracer("github.com/horizon/orion/services/orion-volume")

func NewService(store ports.VolumeStore, host ports.HostAgent, hosts ports.HostDirectory, publisher events.Publisher, cellID string) *Service {
	return &Service{
		store:     store,
		host:      host,
		hosts:     hosts,
		publisher: publisher,
		cellID:    cellID,
	}
}

func (s *Service) ListVolumes(ctx context.Context) []volumekit.Volume {
	items, _ := s.store.ListVolumes(ctx)
	return items
}

func (s *Service) Reconcile(ctx context.Context) (int, error) {
	ctx, span := tracer.Start(ctx, "volume.Reconcile")
	defer span.End()

	volumes, err := s.store.ListVolumes(ctx)
	if err != nil {
		return 0, err
	}

	reconciled := 0
	for _, item := range volumes {
		if item.HostID == "" {
			continue
		}

		observed, err := s.host.ObserveVolume(ctx, item.HostID, item.ID)
		if errors.Is(err, ports.ErrBackendUnavailable) {
			return reconciled, ErrBackendUnavailable
		}
		if err != nil {
			return reconciled, err
		}

		updated := item
		switch {
		case !observed.Exists && item.Status != "error":
			updated.Status = "error"
		case observed.Exists && item.AttachedServerID == "" && item.Status != "available":
			updated.Status = "available"
		case observed.Exists && item.AttachedServerID != "" && item.Status != "in-use":
			updated.Status = "in-use"
		}

		if observed.DevicePath != "" && updated.DevicePath != observed.DevicePath {
			updated.DevicePath = observed.DevicePath
		}

		if updated.Status == item.Status && updated.DevicePath == item.DevicePath {
			continue
		}

		updated.UpdatedAt = time.Now().UTC()
		if err := s.store.SaveVolume(ctx, updated); err != nil {
			return reconciled, err
		}
		s.publishResource(ctx, "volume.reconciled", updated)
		reconciled++
	}

	span.SetAttributes(attribute.Int("orion.reconciled_volumes", reconciled))
	return reconciled, nil
}

func (s *Service) GetVolume(ctx context.Context, volumeID string) (volumekit.Volume, error) {
	v, err := s.store.GetVolume(ctx, volumeID)
	if errors.Is(err, ports.ErrVolumeRecordNotFound) {
		return volumekit.Volume{}, ErrVolumeNotFound
	}
	return v, err
}

func (s *Service) CreateVolume(ctx context.Context, req volumekit.CreateVolumeRequest) (volumekit.Volume, error) {
	ctx, span := tracer.Start(ctx, "volume.CreateVolume")
	defer span.End()

	if req.SizeGB <= 0 {
		return volumekit.Volume{}, ErrInvalidSize
	}
	quotaStore, quotaEnabled := s.store.(ports.QuotaStore)
	quotaCommitted := false
	if quotaEnabled {
		if err := quotaStore.AllocateProjectQuota(ctx, req.ProjectID, 1, req.SizeGB); err != nil {
			return volumekit.Volume{}, err
		}
		defer func() {
			if !quotaCommitted {
				_ = quotaStore.ReleaseProjectQuota(context.Background(), req.ProjectID, 1, req.SizeGB)
			}
		}()
	}

	now := time.Now().UTC()
	volumeID := ids.New("vol")
	selection, err := s.hosts.SelectHost(ctx, ports.SelectHostRequest{
		ServerID:               volumeID,
		CellID:                 s.cellID,
		DiskGB:                 req.SizeGB,
		RequireVolumeHostAgent: true,
	})
	if err != nil {
		if errors.Is(err, ports.ErrNoValidHost) {
			return volumekit.Volume{}, err
		}
		return volumekit.Volume{}, err
	}

	item := volumekit.Volume{
		ID:          volumeID,
		ProjectID:   req.ProjectID,
		Name:        req.Name,
		SizeGB:      req.SizeGB,
		CellID:      firstNonEmpty(selection.CellID, s.cellID),
		HostID:      selection.HostID,
		Status:      "creating",
		BackendType: "lvm",
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	result, err := s.host.CreateVolume(ctx, item.HostID, ports.CreateVolumeRequest{
		VolumeID: item.ID,
		SizeGB:   item.SizeGB,
	})
	if err != nil {
		s.releaseHost(ctx, item)
		if errors.Is(err, ports.ErrBackendUnavailable) {
			return volumekit.Volume{}, ErrBackendUnavailable
		}
		return volumekit.Volume{}, err
	}

	item.DevicePath = result.DevicePath
	item.Status = "available"
	item.UpdatedAt = time.Now().UTC()

	if err := s.store.SaveVolume(ctx, item); err != nil {
		_ = s.host.DeleteVolume(ctx, item.HostID, item.ID)
		s.releaseHost(ctx, item)
		return volumekit.Volume{}, err
	}
	quotaCommitted = true
	span.SetAttributes(attribute.String("orion.volume_id", item.ID))
	s.publishResource(ctx, "volume.created", item)
	return item, nil
}

func (s *Service) DeleteVolume(ctx context.Context, volumeID string) error {
	ctx, span := tracer.Start(ctx, "volume.DeleteVolume")
	defer span.End()

	item, err := s.store.GetVolume(ctx, volumeID)
	if errors.Is(err, ports.ErrVolumeRecordNotFound) {
		return ErrVolumeNotFound
	}
	if err != nil {
		return err
	}
	if item.AttachedServerID != "" {
		return ErrVolumeInUse
	}

	finalizerStore, finalizersEnabled := s.store.(ports.FinalizerStore)
	if finalizersEnabled {
		if err := finalizerStore.EnsureFinalizers(ctx, "volume", item.ID, []string{"backend.delete", "placement.release", "quota.release"}); err != nil {
			return err
		}
	}

	if err := s.host.DeleteVolume(ctx, item.HostID, volumeID); err != nil {
		if errors.Is(err, ports.ErrBackendUnavailable) {
			return ErrBackendUnavailable
		}
		return err
	}
	if finalizersEnabled {
		if err := finalizerStore.RemoveFinalizer(ctx, "volume", item.ID, "backend.delete"); err != nil {
			return err
		}
	}

	if err := s.hosts.ReleaseHost(ctx, ports.ReleaseHostRequest{
		ServerID: item.ID,
		HostID:   item.HostID,
		DiskGB:   item.SizeGB,
	}); err != nil {
		return fmt.Errorf("release volume host allocation: %w", err)
	}
	if finalizersEnabled {
		if err := finalizerStore.RemoveFinalizer(ctx, "volume", item.ID, "placement.release"); err != nil {
			return err
		}
	}
	if quotaStore, ok := s.store.(ports.QuotaStore); ok {
		if err := quotaStore.ReleaseProjectQuota(ctx, item.ProjectID, 1, item.SizeGB); err != nil {
			return err
		}
	}
	if finalizersEnabled {
		if err := finalizerStore.RemoveFinalizer(ctx, "volume", item.ID, "quota.release"); err != nil {
			return err
		}
		if remaining, err := finalizerStore.ListFinalizers(ctx, "volume", item.ID); err != nil {
			return err
		} else if len(remaining) > 0 {
			return fmt.Errorf("volume %s still has finalizers: %v", item.ID, remaining)
		}
	}
	if err := s.store.DeleteVolume(ctx, volumeID); err != nil {
		return err
	}
	item.Status = "deleted"
	item.UpdatedAt = time.Now().UTC()
	span.SetAttributes(attribute.String("orion.volume_id", volumeID))
	s.publishResource(ctx, "volume.deleted", item)
	return nil
}

func (s *Service) AttachVolume(ctx context.Context, volumeID, serverID, hostID string) (volumekit.Volume, error) {
	ctx, span := tracer.Start(ctx, "volume.AttachVolume")
	defer span.End()

	item, err := s.store.GetVolume(ctx, volumeID)
	if errors.Is(err, ports.ErrVolumeRecordNotFound) {
		return volumekit.Volume{}, ErrVolumeNotFound
	}
	if err != nil {
		return volumekit.Volume{}, err
	}
	if item.AttachedServerID != "" {
		return volumekit.Volume{}, ErrVolumeInUse
	}
	if hostID != "" && item.HostID != "" && item.HostID != hostID {
		return volumekit.Volume{}, ErrHostMismatch
	}
	if item.HostID == "" && hostID != "" {
		item.HostID = hostID
	}
	item.AttachedServerID = serverID
	item.Status = "in-use"
	item.UpdatedAt = time.Now().UTC()

	if err := s.store.SaveVolume(ctx, item); err != nil {
		return volumekit.Volume{}, err
	}
	span.SetAttributes(
		attribute.String("orion.volume_id", volumeID),
		attribute.String("orion.server_id", serverID),
	)
	s.publishResource(ctx, "volume.attached", item)
	return item, nil
}

func (s *Service) DetachVolume(ctx context.Context, volumeID string) (volumekit.Volume, error) {
	ctx, span := tracer.Start(ctx, "volume.DetachVolume")
	defer span.End()

	item, err := s.store.GetVolume(ctx, volumeID)
	if errors.Is(err, ports.ErrVolumeRecordNotFound) {
		return volumekit.Volume{}, ErrVolumeNotFound
	}
	if err != nil {
		return volumekit.Volume{}, err
	}
	if item.AttachedServerID == "" {
		return volumekit.Volume{}, ErrVolumeNotAttached
	}
	item.AttachedServerID = ""
	item.Status = "available"
	item.UpdatedAt = time.Now().UTC()

	if err := s.store.SaveVolume(ctx, item); err != nil {
		return volumekit.Volume{}, err
	}
	span.SetAttributes(attribute.String("orion.volume_id", volumeID))
	s.publishResource(ctx, "volume.detached", item)
	return item, nil
}

func (s *Service) ListSnapshots(ctx context.Context, projectID, volumeID string) ([]volumekit.VolumeSnapshot, error) {
	store, ok := s.store.(ports.SnapshotStore)
	if !ok {
		return nil, ErrSnapshotUnsupported
	}
	return store.ListSnapshots(ctx, projectID, volumeID)
}

func (s *Service) CreateSnapshot(ctx context.Context, req volumekit.CreateVolumeSnapshotRequest) (volumekit.VolumeSnapshot, error) {
	store, ok := s.store.(ports.SnapshotStore)
	if !ok {
		return volumekit.VolumeSnapshot{}, ErrSnapshotUnsupported
	}
	volume, err := s.GetVolume(ctx, req.VolumeID)
	if err != nil {
		return volumekit.VolumeSnapshot{}, err
	}
	if volume.ProjectID != req.ProjectID || volume.Status == "creating" {
		return volumekit.VolumeSnapshot{}, ErrSnapshotInUse
	}
	if req.Name == "" {
		return volumekit.VolumeSnapshot{}, fmt.Errorf("snapshot name is required")
	}
	snapshot := volumekit.VolumeSnapshot{ID: ids.New("snap"), VolumeID: volume.ID, ProjectID: volume.ProjectID, Name: req.Name, SizeGB: volume.SizeGB, Status: "creating", CreatedAt: time.Now().UTC()}
	if err := s.host.CreateSnapshot(ctx, volume.HostID, volume.ID, snapshot.ID); err != nil {
		if errors.Is(err, ports.ErrBackendUnavailable) {
			return volumekit.VolumeSnapshot{}, ErrBackendUnavailable
		}
		return volumekit.VolumeSnapshot{}, err
	}
	snapshot.Status = "available"
	snapshot.UpdatedAt = time.Now().UTC()
	if err := store.SaveSnapshot(ctx, snapshot); err != nil {
		_ = s.host.DeleteSnapshot(ctx, volume.HostID, volume.ID, snapshot.ID)
		return volumekit.VolumeSnapshot{}, err
	}
	return snapshot, nil
}

func (s *Service) GetSnapshot(ctx context.Context, id string) (volumekit.VolumeSnapshot, error) {
	store, ok := s.store.(ports.SnapshotStore)
	if !ok {
		return volumekit.VolumeSnapshot{}, ErrSnapshotUnsupported
	}
	snapshot, err := store.GetSnapshot(ctx, id)
	if errors.Is(err, ports.ErrSnapshotRecordNotFound) {
		return volumekit.VolumeSnapshot{}, ErrSnapshotNotFound
	}
	return snapshot, err
}

func (s *Service) DeleteSnapshot(ctx context.Context, id string) error {
	store, ok := s.store.(ports.SnapshotStore)
	if !ok {
		return ErrSnapshotUnsupported
	}
	snapshot, err := s.GetSnapshot(ctx, id)
	if err != nil {
		return err
	}
	volume, err := s.GetVolume(ctx, snapshot.VolumeID)
	if err != nil {
		return err
	}
	if err := s.host.DeleteSnapshot(ctx, volume.HostID, volume.ID, snapshot.ID); err != nil {
		if errors.Is(err, ports.ErrBackendUnavailable) {
			return ErrBackendUnavailable
		}
		return err
	}
	return store.DeleteSnapshot(ctx, id)
}

func (s *Service) RestoreSnapshot(ctx context.Context, id string) (volumekit.Volume, error) {
	store, ok := s.store.(ports.SnapshotStore)
	if !ok {
		return volumekit.Volume{}, ErrSnapshotUnsupported
	}
	snapshot, err := s.GetSnapshot(ctx, id)
	if err != nil {
		return volumekit.Volume{}, err
	}
	volume, err := s.GetVolume(ctx, snapshot.VolumeID)
	if err != nil {
		return volumekit.Volume{}, err
	}
	if volume.AttachedServerID != "" {
		return volumekit.Volume{}, ErrVolumeInUse
	}
	if err := s.host.RestoreSnapshot(ctx, volume.HostID, volume.ID, snapshot.ID); err != nil {
		if errors.Is(err, ports.ErrBackendUnavailable) {
			return volumekit.Volume{}, ErrBackendUnavailable
		}
		return volumekit.Volume{}, err
	}
	volume.UpdatedAt = time.Now().UTC()
	_ = store
	if err := s.store.SaveVolume(ctx, volume); err != nil {
		return volumekit.Volume{}, err
	}
	return volume, nil
}

func (s *Service) publishResource(ctx context.Context, eventType string, item volumekit.Volume) {
	if s.publisher == nil {
		return
	}
	_ = s.publisher.PublishResource(ctx, events.ResourceEvent{
		EventType:    eventType,
		ResourceType: "volume",
		ResourceID:   item.ID,
		ProjectID:    item.ProjectID,
		RequestID:    httpx.RequestIDFromContext(ctx),
		Status:       item.Status,
		OccurredAt:   time.Now().UTC(),
	})
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func (s *Service) releaseHost(ctx context.Context, item volumekit.Volume) {
	if item.HostID == "" || item.SizeGB <= 0 {
		return
	}
	_ = s.hosts.ReleaseHost(ctx, ports.ReleaseHostRequest{
		ServerID: item.ID,
		HostID:   item.HostID,
		DiskGB:   item.SizeGB,
	})
}

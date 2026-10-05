package ports

import (
	"context"
	"errors"

	volumekit "github.com/horizon/orion/libs/go/kit/volume"
)

var (
	ErrVolumeRecordNotFound   = errors.New("volume record not found")
	ErrQuotaExceeded          = errors.New("project quota exceeded")
	ErrSnapshotRecordNotFound = errors.New("snapshot record not found")
)

type QuotaStore interface {
	AllocateProjectQuota(ctx context.Context, projectID string, volumes, volumeGB int) error
	ReleaseProjectQuota(ctx context.Context, projectID string, volumes, volumeGB int) error
}

type FinalizerStore interface {
	EnsureFinalizers(ctx context.Context, resourceType, resourceID string, finalizers []string) error
	RemoveFinalizer(ctx context.Context, resourceType, resourceID, finalizer string) error
	ListFinalizers(ctx context.Context, resourceType, resourceID string) ([]string, error)
}

// VolumeStore persists volume records.
type VolumeStore interface {
	SaveVolume(ctx context.Context, v volumekit.Volume) error
	GetVolume(ctx context.Context, id string) (volumekit.Volume, error)
	ListVolumes(ctx context.Context) ([]volumekit.Volume, error)
	DeleteVolume(ctx context.Context, id string) error
}

type SnapshotStore interface {
	SaveSnapshot(ctx context.Context, snapshot volumekit.VolumeSnapshot) error
	GetSnapshot(ctx context.Context, id string) (volumekit.VolumeSnapshot, error)
	ListSnapshots(ctx context.Context, projectID, volumeID string) ([]volumekit.VolumeSnapshot, error)
	DeleteSnapshot(ctx context.Context, id string) error
}

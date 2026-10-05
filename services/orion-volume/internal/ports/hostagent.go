package ports

import (
	"context"
	"errors"
)

var ErrBackendUnavailable = errors.New("volume backend unavailable")

type CreateVolumeRequest struct {
	VolumeID string `json:"volume_id"`
	SizeGB   int    `json:"size_gb"`
}

type CreateVolumeResult struct {
	DevicePath string
}

type VolumeObservation struct {
	Exists     bool
	DevicePath string
}

type CreateSnapshotResult struct{}

type HostAgent interface {
	CreateVolume(ctx context.Context, hostID string, req CreateVolumeRequest) (CreateVolumeResult, error)
	DeleteVolume(ctx context.Context, hostID, volumeID string) error
	ObserveVolume(ctx context.Context, hostID, volumeID string) (VolumeObservation, error)
	CreateSnapshot(ctx context.Context, hostID, volumeID, snapshotID string) error
	DeleteSnapshot(ctx context.Context, hostID, volumeID, snapshotID string) error
	RestoreSnapshot(ctx context.Context, hostID, volumeID, snapshotID string) error
}

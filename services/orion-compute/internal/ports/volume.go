package ports

import (
	"context"
	"errors"

	volumekit "github.com/horizon/orion/libs/go/kit/volume"
)

var ErrVolumeNotFound = errors.New("volume not found")
var ErrVolumeBackendUnavailable = errors.New("volume backend unavailable")
var ErrVolumeInUse = errors.New("volume in use")
var ErrVolumeNotAttached = errors.New("volume not attached")

type VolumeManager interface {
	GetVolume(ctx context.Context, volumeID string) (volumekit.Volume, error)
	CreateVolume(ctx context.Context, req volumekit.CreateVolumeRequest) (volumekit.Volume, error)
	DeleteVolume(ctx context.Context, volumeID string) error
	AttachVolume(ctx context.Context, volumeID, serverID, hostID string) (volumekit.Volume, error)
	DetachVolume(ctx context.Context, volumeID string) (volumekit.Volume, error)
}

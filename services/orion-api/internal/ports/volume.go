package ports

import (
	"context"

	"github.com/horizon/orion/libs/go/kit/authn"
	volumekit "github.com/horizon/orion/libs/go/kit/volume"
)

type VolumeClient interface {
	CreateVolume(ctx context.Context, actor authn.Actor, req volumekit.CreateVolumeRequest) (volumekit.Volume, error)
	ListVolumes(ctx context.Context, actor authn.Actor) ([]volumekit.Volume, error)
	GetVolume(ctx context.Context, actor authn.Actor, volumeID string) (volumekit.Volume, error)
	DeleteVolume(ctx context.Context, actor authn.Actor, volumeID string) error
}

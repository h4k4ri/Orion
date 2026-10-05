package ports

import (
	"context"
	"errors"

	"github.com/horizon/orion/libs/go/kit/compute"
	"github.com/horizon/orion/libs/go/kit/image"
	networkkit "github.com/horizon/orion/libs/go/kit/network"
	volumekit "github.com/horizon/orion/libs/go/kit/volume"
)

var ErrServerInstanceNotFound = errors.New("server instance not found")

type ServerObservation struct {
	Status string
}

type Executor interface {
	BuildServer(ctx context.Context, server compute.Server, img image.Image, ports []networkkit.Port, vcpus, memoryMB, diskGB int) (compute.Server, error)
	DeleteServer(ctx context.Context, server compute.Server) error
	AttachVolume(ctx context.Context, server compute.Server, volume volumekit.Volume) error
	DetachVolume(ctx context.Context, server compute.Server, volume volumekit.Volume) error
	ObserveServer(ctx context.Context, server compute.Server) (ServerObservation, error)
}

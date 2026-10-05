package ports

import (
	"context"
	"errors"

	"github.com/horizon/orion/libs/go/kit/authn"
	"github.com/horizon/orion/libs/go/kit/compute"
	"github.com/horizon/orion/libs/go/kit/task"
)

var (
	ErrNetworkBackendUnavailable = errors.New("network backend unavailable")
	ErrNetworkNotFound           = errors.New("network not found")
	ErrImageNotFound             = errors.New("image not found")
	ErrUnknownFlavor             = errors.New("unknown flavor")
	ErrNoValidHost               = errors.New("no valid host")
	ErrQuotaExceeded             = errors.New("project quota exceeded")
	ErrServerNotFound            = errors.New("server not found")
	ErrTaskNotFound              = errors.New("task not found")
	ErrVolumeBackendUnavailable  = errors.New("volume backend unavailable")
	ErrVolumeNotFound            = errors.New("volume not found")
	ErrVolumeInUse               = errors.New("volume in use")
	ErrVolumeNotAttached         = errors.New("volume not attached")
)

type ComputeClient interface {
	CreateServer(ctx context.Context, actor authn.Actor, req compute.CreateServerRequest) (compute.Server, task.Task, error)
	CreateServerAsync(ctx context.Context, actor authn.Actor, req compute.CreateServerRequest) (compute.CreateServerResponseAsync, error)
	ListServers(ctx context.Context, actor authn.Actor) ([]compute.Server, error)
	GetServer(ctx context.Context, actor authn.Actor, serverID string) (compute.Server, error)
	DeleteServer(ctx context.Context, actor authn.Actor, serverID string) (task.Task, error)
	AttachVolume(ctx context.Context, actor authn.Actor, serverID, volumeID string) (compute.Server, task.Task, error)
	DetachVolume(ctx context.Context, actor authn.Actor, serverID, volumeID string) (compute.Server, task.Task, error)
	GetTask(ctx context.Context, actor authn.Actor, taskID string) (task.Task, error)
}

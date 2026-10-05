package ports

import (
	"context"
	"errors"

	networkkit "github.com/horizon/orion/libs/go/kit/network"
)

var ErrNetworkNotFound = errors.New("network not found")
var ErrNetworkBackendUnavailable = errors.New("network backend unavailable")

type NetworkAllocator interface {
	CreatePort(ctx context.Context, req networkkit.CreatePortRequest) (networkkit.Port, error)
	DeletePort(ctx context.Context, portID string) error
}

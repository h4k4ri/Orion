package ports

import (
	"context"
	"errors"

	"github.com/horizon/orion/libs/go/kit/compute"
	"github.com/horizon/orion/libs/go/kit/task"
)

var (
	ErrServerNotFound = errors.New("server not found")
	ErrTaskNotFound   = errors.New("task not found")
	ErrQuotaExceeded  = errors.New("project quota exceeded")
)

type QuotaStore interface {
	AllocateProjectQuota(ctx context.Context, projectID string, instances, vcpus, ramMB int) error
	ReleaseProjectQuota(ctx context.Context, projectID string, instances, vcpus, ramMB int) error
}

type FinalizerStore interface {
	EnsureFinalizers(ctx context.Context, resourceType, resourceID string, finalizers []string) error
	RemoveFinalizer(ctx context.Context, resourceType, resourceID, finalizer string) error
	ListFinalizers(ctx context.Context, resourceType, resourceID string) ([]string, error)
}

// Store persists compute servers and tasks.
type Store interface {
	SaveServer(ctx context.Context, server compute.Server) error
	GetServer(ctx context.Context, id string) (compute.Server, error)
	ListServers(ctx context.Context) ([]compute.Server, error)
	DeleteServer(ctx context.Context, id string) error

	CreateTaskIfAbsent(ctx context.Context, t task.Task) (task.Task, bool, error)
	SaveTask(ctx context.Context, t task.Task) error
	GetTask(ctx context.Context, id string) (task.Task, error)
	GetTaskByRequest(ctx context.Context, kind, requestID string) (task.Task, error)
}

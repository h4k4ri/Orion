package driver

import (
	"context"

	"github.com/horizon/orion/plugins/containers/container"
)

type Driver interface {
	ListContainers(ctx context.Context, opts ListOptions) ([]container.Container, error)
	GetContainer(ctx context.Context, id string) (*container.Container, error)
	CreateContainer(ctx context.Context, input *container.CreateContainerInput) (*container.Container, error)
	StartContainer(ctx context.Context, id string) error
	StopContainer(ctx context.Context, id string, timeout int) error
	DeleteContainer(ctx context.Context, id string) error
	LogsContainer(ctx context.Context, id string, opts LogsOptions) (string, error)
	ExecContainer(ctx context.Context, id string, cmd []string) (string, error)

	ListImages(ctx context.Context, opts ListOptions) ([]container.Image, error)
	PullImage(ctx context.Context, ref string) error
	DeleteImage(ctx context.Context, ref string) error

	Close() error
}

type ListOptions struct {
	All       bool
	Namespace string
	Labels    map[string]string
}

type LogsOptions struct {
	Stdout bool
	Stderr bool
	Tail   int
	Since  string
}

type Config struct {
	Address string
}

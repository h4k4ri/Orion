package driver

import (
	"context"

	"github.com/horizon/orion/plugins/orchestration/stack"
)

type Driver interface {
	Create(ctx context.Context, st *stack.Stack, template *stack.Template) error
	Update(ctx context.Context, st *stack.Stack, template *stack.Template) error
	Delete(ctx context.Context, st *stack.Stack) error
	Suspend(ctx context.Context, st *stack.Stack) error
	Resume(ctx context.Context, st *stack.Stack) error
	Check(ctx context.Context, st *stack.Stack) error
	GetOutputs(ctx context.Context, st *stack.Stack) (map[string]interface{}, error)
	Close() error
}

type Config struct {
	Backend string
	WorkDir string
}

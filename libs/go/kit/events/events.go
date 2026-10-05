package events

import (
	"context"
	"time"

	"github.com/horizon/orion/libs/go/kit/task"
)

// EventType constants for task lifecycle events.
const (
	TaskCreated   = "task.created"
	TaskStarted   = "task.started"
	TaskSucceeded = "task.succeeded"
	TaskFailed    = "task.failed"
)

// TaskEvent is the envelope published to NATS on every task state change.
type TaskEvent struct {
	EventType     string    `json:"event_type"`
	OperationID   string    `json:"operation_id,omitempty"`
	ObservedState string    `json:"observed_state,omitempty"`
	Task          task.Task `json:"task"`
}

// ResourceEvent is the envelope published for network and volume lifecycle changes.
type ResourceEvent struct {
	EventType    string    `json:"event_type"`
	ResourceType string    `json:"resource_type"`
	ResourceID   string    `json:"resource_id"`
	ProjectID    string    `json:"project_id,omitempty"`
	RequestID    string    `json:"request_id,omitempty"`
	Status       string    `json:"status,omitempty"`
	OccurredAt   time.Time `json:"occurred_at"`
}

// Publisher publishes task lifecycle events.
// Implementations must be safe for concurrent use.
type Publisher interface {
	PublishTask(ctx context.Context, event TaskEvent) error
	PublishResource(ctx context.Context, event ResourceEvent) error
}

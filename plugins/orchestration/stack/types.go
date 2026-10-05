package stack

import (
	"encoding/json"
	"time"
)

type Stack struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	TenantID    string                 `json:"tenantId"`
	ProjectID   string                 `json:"projectId"`
	Kind        string                 `json:"kind"`
	State       StackState             `json:"state"`
	Variables   map[string]interface{} `json:"variables,omitempty"`
	Outputs     map[string]interface{} `json:"outputs,omitempty"`
	CreatedAt   time.Time              `json:"createdAt"`
	UpdatedAt   time.Time              `json:"updatedAt"`
	CompletedAt *time.Time             `json:"completedAt,omitempty"`
	Error       string                 `json:"error,omitempty"`
}

type Event struct {
	ID        string    `json:"id"`
	StackID   string    `json:"stackId"`
	Action    string    `json:"action"`
	Status    string    `json:"status"`
	Message   string    `json:"message,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

type StackState string

const (
	StackStatePending   StackState = "PENDING"
	StackStateRunning   StackState = "RUNNING"
	StackStateCompleted StackState = "COMPLETED"
	StackStateFailed    StackState = "FAILED"
	StackStateCancelled StackState = "CANCELLED"
	StackStateSuspended StackState = "SUSPENDED"
)

type Template struct {
	Format    string     `json:"format"`
	Content   string     `json:"content"`
	Variables []Variable `json:"variables,omitempty"`
}

type Variable struct {
	Name        string      `json:"name"`
	Type        string      `json:"type,omitempty"`
	Default     interface{} `json:"default,omitempty"`
	Description string      `json:"description,omitempty"`
	Required    bool        `json:"required,omitempty"`
	Sensitive   bool        `json:"sensitive,omitempty"`
}

type Operation string

const (
	OpCreate  Operation = "create"
	OpUpdate  Operation = "update"
	OpDelete  Operation = "delete"
	OpSuspend Operation = "suspend"
	OpResume  Operation = "resume"
	OpCheck   Operation = "check"
)

type CreateInput struct {
	Name            string                 `json:"name"`
	TenantID        string                 `json:"tenantId,omitempty"`
	ProjectID       string                 `json:"projectId,omitempty"`
	TemplateFormat  string                 `json:"templateFormat"`
	TemplateContent string                 `json:"templateContent"`
	Variables       map[string]interface{} `json:"variables,omitempty"`
	Backend         string                 `json:"backend,omitempty"`
}

type UpdateInput struct {
	ID              string                 `json:"id"`
	TemplateContent string                 `json:"templateContent,omitempty"`
	Variables       map[string]interface{} `json:"variables,omitempty"`
}

func (s *Stack) MarshalJSON() ([]byte, error) {
	type Alias Stack
	return json.Marshal(&struct {
		*Alias
		CreatedAt string `json:"createdAt"`
		UpdatedAt string `json:"updatedAt"`
	}{
		Alias:     (*Alias)(s),
		CreatedAt: s.CreatedAt.Format(time.RFC3339),
		UpdatedAt: s.UpdatedAt.Format(time.RFC3339),
	})
}

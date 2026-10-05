package task

import "time"

type Status string

const (
	StatusPending   Status = "pending"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
)

type Task struct {
	ID           string    `json:"id"`
	Kind         string    `json:"kind"`
	Scope        string    `json:"scope"`
	TargetRef    string    `json:"target_ref"`
	Status       Status    `json:"status"`
	RequestedBy  string    `json:"requested_by"`
	RequestID    string    `json:"request_id"`
	CellID       string    `json:"cell_id,omitempty"`
	HostID       string    `json:"host_id,omitempty"`
	ErrorCode    string    `json:"error_code,omitempty"`
	ErrorMessage string    `json:"error_message,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

package domain

import (
	"time"

	"github.com/horizon/orion/libs/go/kit/compute"
	volumekit "github.com/horizon/orion/libs/go/kit/volume"
)

type CreateServerRequest = compute.CreateServerRequest
type Server = compute.Server
type CreateServerResponse = compute.CreateServerResponse
type CreateVolumeRequest = volumekit.CreateVolumeRequest
type Volume = volumekit.Volume

type Operation struct {
	ID            string           `json:"id"`
	ResourceType  string           `json:"resource_type"`
	ResourceID    string           `json:"resource_id"`
	ProjectID     string           `json:"project_id"`
	RequestID     string           `json:"request_id"`
	OperationType string           `json:"operation_type"`
	State         string           `json:"state"`
	CurrentStep   string           `json:"current_step"`
	Attempt       int              `json:"attempt"`
	CreatedAt     time.Time        `json:"created_at"`
	StartedAt     *time.Time       `json:"started_at,omitempty"`
	FinishedAt    *time.Time       `json:"finished_at,omitempty"`
	ErrorCode     string           `json:"error_code,omitempty"`
	ErrorMessage  string           `json:"error_message,omitempty"`
	Events        []OperationEvent `json:"events,omitempty"`
}

type OperationEvent struct {
	ID          string    `json:"id"`
	OperationID string    `json:"operation_id"`
	Sequence    int64     `json:"sequence"`
	EventType   string    `json:"event_type"`
	FromState   string    `json:"from_state,omitempty"`
	ToState     string    `json:"to_state,omitempty"`
	Step        string    `json:"step,omitempty"`
	Payload     []byte    `json:"payload,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

package domain

import "time"

type OperationState string

const (
	StatePending           OperationState = "PENDING"
	StateValidating        OperationState = "VALIDATING"
	StateScheduling        OperationState = "SCHEDULING"
	StateAllocatingNetwork OperationState = "ALLOCATING_NETWORK"
	StatePreparingStorage  OperationState = "PREPARING_STORAGE"
	StateSpawning          OperationState = "SPAWNING"
	StateVerifying         OperationState = "VERIFYING"
	StateSucceeded         OperationState = "SUCCEEDED"
	StateFailed            OperationState = "FAILED"
	StateCancelled         OperationState = "CANCELLED"
	StateTimedOut          OperationState = "TIMED_OUT"
	StateCompensating      OperationState = "COMPENSATING"
	StateCompensated       OperationState = "COMPENSATED"
)

func (s OperationState) IsTerminal() bool {
	switch s {
	case StateSucceeded, StateFailed, StateCancelled, StateTimedOut, StateCompensated:
		return true
	}
	return false
}

func (s OperationState) CanTransitionTo(next OperationState) bool {
	transitions := map[OperationState][]OperationState{
		StatePending:           {StateValidating, StateFailed, StateCancelled, StateTimedOut},
		StateValidating:        {StateScheduling, StateFailed, StateCancelled, StateTimedOut},
		StateScheduling:        {StateAllocatingNetwork, StateFailed, StateCancelled, StateTimedOut},
		StateAllocatingNetwork: {StatePreparingStorage, StateFailed, StateCancelled, StateTimedOut, StateCompensating},
		StatePreparingStorage:  {StateSpawning, StateFailed, StateCancelled, StateTimedOut, StateCompensating},
		StateSpawning:          {StateVerifying, StateFailed, StateCancelled, StateTimedOut, StateCompensating},
		StateVerifying:         {StateSucceeded, StateFailed, StateCancelled, StateTimedOut, StateCompensating},
		StateCompensating:      {StateCompensated, StateFailed},
	}
	allowed, ok := transitions[s]
	if !ok {
		return false
	}
	for _, s := range allowed {
		if s == next {
			return true
		}
	}
	return false
}

type OperationStep string

const (
	StepValidateIdentity    OperationStep = "validate_identity"
	StepValidateQuota       OperationStep = "validate_quota"
	StepValidateFlavor      OperationStep = "validate_flavor"
	StepSelectHost          OperationStep = "select_host"
	StepAllocateNetwork     OperationStep = "allocate_network"
	StepAllocateVolume      OperationStep = "allocate_volume"
	StepSendSpawnCommand    OperationStep = "send_spawn_command"
	StepWaitForInstance     OperationStep = "wait_for_instance"
	StepVerifyInstance      OperationStep = "verify_instance"
	StepCompensateNetwork   OperationStep = "compensate_network"
	StepCompensateVolume    OperationStep = "compensate_volume"
	StepCompensatePlacement OperationStep = "compensate_placement"
	StepCompensateInstance  OperationStep = "compensate_instance"
)

type Operation struct {
	ID            string           `json:"id"`
	ResourceType  string           `json:"resource_type"`
	ResourceID    string           `json:"resource_id"`
	ProjectID     string           `json:"project_id"`
	RequestID     string           `json:"request_id"`
	OperationType string           `json:"operation_type"`
	State         OperationState   `json:"state"`
	CurrentStep   OperationStep    `json:"current_step"`
	Attempt       int              `json:"attempt"`
	Version       int64            `json:"version"`
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

const (
	EventTypeStateTransition OperationEventType = "STATE_TRANSITION"
	EventTypeCommandSent     OperationEventType = "COMMAND_SENT"
	EventTypeEventReceived   OperationEventType = "EVENT_RECEIVED"
	EventTypeStepStarted     OperationEventType = "STEP_STARTED"
	EventTypeStepFinished    OperationEventType = "STEP_FINISHED"
)

type OperationEventType string

type CreateOperationRequest struct {
	ID            string
	ResourceType  string
	ResourceID    string
	ProjectID     string
	RequestID     string
	OperationType string
}

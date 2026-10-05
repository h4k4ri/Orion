package domain

import (
	"encoding/json"
	"time"
)

type ResourceState string

const (
	ResourceStatePending       ResourceState = "PENDING"
	ResourceStateProvisioning  ResourceState = "PROVISIONING"
	ResourceStateAvailable    ResourceState = "AVAILABLE"
	ResourceStateUpdating      ResourceState = "UPDATING"
	ResourceStateDeleting     ResourceState = "DELETING"
	ResourceStateDeleted      ResourceState = "DELETED"
	ResourceStateError        ResourceState = "ERROR"
	ResourceStateImporting    ResourceState = "IMPORTING"
)

type Resource struct {
	ID                  string          `json:"id"`
	TenantID            string          `json:"tenantId"`
	ProjectID           string          `json:"projectId"`
	Kind                string          `json:"kind"`
	Version             string          `json:"version"`
	ProviderID          string          `json:"providerId"`
	ExternalID          string          `json:"externalId,omitempty"`
	State               ResourceState   `json:"state"`
	DesiredSpec         json.RawMessage `json:"desiredSpec,omitempty"`
	ActualState         json.RawMessage `json:"actualState,omitempty"`
	Generation          int64           `json:"generation"`
	ObservedGeneration  int64           `json:"observedGeneration"`
	CreatedAt           time.Time       `json:"createdAt"`
	UpdatedAt           time.Time       `json:"updatedAt"`
	ObservedAt          *time.Time      `json:"observedAt,omitempty"`
	DeletedAt           *time.Time      `json:"deletedAt,omitempty"`
}

type ProviderState string

const (
	ProviderStateEnabled  ProviderState = "ENABLED"
	ProviderStateDisabled ProviderState = "DISABLED"
	ProviderStateDraining ProviderState = "DRAINING"
)

type HealthState string

const (
	HealthStateHealthy   HealthState = "HEALTHY"
	HealthStateDegraded HealthState = "DEGRADED"
	HealthStateUnhealthy HealthState = "UNHEALTHY"
	HealthStateUnknown   HealthState = "UNKNOWN"
)

type Provider struct {
	ProviderID               string          `json:"providerId"`
	PluginID                string          `json:"pluginId"`
	NodeID                  string          `json:"nodeId"`
	Name                    string          `json:"name"`
	Endpoint                string          `json:"endpoint"`
	Config                  json.RawMessage `json:"config"`
	EffectiveCapabilities    json.RawMessage `json:"effectiveCapabilities"`
	AdministrativeState     ProviderState   `json:"administrativeState"`
	HealthState             HealthState     `json:"healthState"`
	Generation              int64           `json:"generation"`
	ObservedGeneration      int64           `json:"observedGeneration"`
	CreatedAt               time.Time       `json:"createdAt"`
	LastSeenAt              time.Time       `json:"lastSeenAt"`
}

type OperationState string

const (
	OperationStatePending   OperationState = "PENDING"
	OperationStateRunning  OperationState = "RUNNING"
	OperationStateSucceeded OperationState = "SUCCEEDED"
	OperationStateFailed   OperationState = "FAILED"
	OperationStateCancelling OperationState = "CANCELLING"
	OperationStateCancelled OperationState = "CANCELLED"
)

type Operation struct {
	OperationID    string          `json:"operationId"`
	RequestID     string          `json:"requestId"`
	IdempotencyKey string         `json:"idempotencyKey,omitempty"`
	TenantID      string          `json:"tenantId"`
	ProjectID     string          `json:"projectId"`
	ProviderID    string          `json:"providerId"`
	Resource      string          `json:"resource"`
	ResourceVersion string        `json:"resourceVersion"`
	OperationName string          `json:"operationName"`
	State         OperationState  `json:"state"`
	Attempt       int            `json:"attempt"`
	MaxAttempts   int            `json:"maxAttempts"`
	ErrorCode     string         `json:"errorCode,omitempty"`
	ErrorMessage  string         `json:"errorMessage,omitempty"`
	StartedAt     *time.Time     `json:"startedAt,omitempty"`
	DeadlineAt    *time.Time     `json:"deadlineAt,omitempty"`
	CompletedAt   *time.Time     `json:"completedAt,omitempty"`
	CreatedAt     time.Time      `json:"createdAt"`
}

type RelationshipState string

const (
	RelationshipStatePending      RelationshipState = "PENDING"
	RelationshipStatePreparing    RelationshipState = "PREPARING"
	RelationshipStateAttaching    RelationshipState = "ATTACHING"
	RelationshipStateActive       RelationshipState = "ACTIVE"
	RelationshipStateDetaching    RelationshipState = "DETACHING"
	RelationshipStateCompensating RelationshipState = "COMPENSATING"
	RelationshipStateDeleted      RelationshipState = "DELETED"
	RelationshipStateError        RelationshipState = "ERROR"
)

type ResourceRelationship struct {
	ID                 string            `json:"id"`
	RelationshipKind   string            `json:"relationshipKind"`
	TenantID           string            `json:"tenantId"`
	ProjectID         string            `json:"projectId"`
	SourceResourceID  string            `json:"sourceResourceId"`
	TargetResourceID  string            `json:"targetResourceId"`
	State             RelationshipState `json:"state"`
	Config            json.RawMessage   `json:"config,omitempty"`
	Generation        int64            `json:"generation"`
	ObservedGeneration int64           `json:"observedGeneration"`
	CreatedAt         time.Time        `json:"createdAt"`
	UpdatedAt         time.Time        `json:"updatedAt"`
}

type StepState string

const (
	StepStatePending            StepState = "PENDING"
	StepStateRunning           StepState = "RUNNING"
	StepStateSucceeded        StepState = "SUCCEEDED"
	StepStateFailed           StepState = "FAILED"
	StepStateCompensating     StepState = "COMPENSATING"
	StepStateCompensated      StepState = "COMPENSATED"
	StepStateCompensationFailed StepState = "COMPENSATION_FAILED"
)

type SagaStepExecution struct {
	StepID               string          `json:"stepId"`
	ExecutionID          string          `json:"executionId"`
	StepIndex            int             `json:"stepIndex"`
	Role                 string          `json:"role"`
	Operation            string          `json:"operation"`
	Input                json.RawMessage `json:"input,omitempty"`
	Output               json.RawMessage `json:"output,omitempty"`
	State                StepState       `json:"state"`
	CompensationRequired bool            `json:"compensationRequired"`
	CompensationInput    json.RawMessage `json:"compensationInput,omitempty"`
	ErrorCode            string         `json:"errorCode,omitempty"`
	ErrorMessage         string         `json:"errorMessage,omitempty"`
	StartedAt            *time.Time     `json:"startedAt,omitempty"`
	CompletedAt          *time.Time     `json:"completedAt,omitempty"`
}

type RelationshipExecution struct {
	ExecutionID  string          `json:"executionId"`
	RelationshipID string       `json:"relationshipId"`
	Operation   string          `json:"operation"`
	State       RelationshipState `json:"state"`
	Steps       []SagaStepExecution `json:"steps,omitempty"`
	ErrorCode   string         `json:"errorCode,omitempty"`
	ErrorMessage string         `json:"errorMessage,omitempty"`
	StartedAt   time.Time      `json:"startedAt"`
	CompletedAt *time.Time     `json:"completedAt,omitempty"`
}

type ResourceDeclaration struct {
	Kind     string          `json:"kind"`
	Version  string          `json:"version"`
	Spec     json.RawMessage `json:"spec,omitempty"`
}

type RelationshipDeclaration struct {
	Kind    string          `json:"kind"`
	Version string          `json:"version"`
	Spec    json.RawMessage `json:"spec,omitempty"`
}

type PluginManifest struct {
	Name        string                   `json:"name"`
	Version     string                   `json:"version"`
	Vendor      string                   `json:"vendor,omitempty"`
	Description string                   `json:"description,omitempty"`
	Resources   []ResourceDeclaration    `json:"resources,omitempty"`
	Relationships []RelationshipDeclaration `json:"relationships,omitempty"`
}

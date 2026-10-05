package container

import (
	"encoding/json"
	"time"
)

type Container struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Image       string            `json:"image"`
	Command     []string          `json:"command,omitempty"`
	Args        []string          `json:"args,omitempty"`
	Env         []EnvVar          `json:"env,omitempty"`
	Ports       []PortMapping     `json:"ports,omitempty"`
	Mounts      []Mount           `json:"mounts,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	State       ContainerState     `json:"state"`
	PodID       string            `json:"podId,omitempty"`
	NodeName    string            `json:"nodeName,omitempty"`
	CreatedAt   time.Time        `json:"createdAt"`
	StartedAt   *time.Time       `json:"startedAt,omitempty"`
	FinishedAt  *time.Time       `json:"finishedAt,omitempty"`
	ExitCode    int              `json:"exitCode,omitempty"`
	RestartCount int             `json:"restartCount,omitempty"`
}

type ContainerState string

const (
	ContainerStateCreated  ContainerState = "CREATED"
	ContainerStateRunning  ContainerState = "RUNNING"
	ContainerStatePaused   ContainerState = "PAUSED"
	ContainerStateStopped  ContainerState = "STOPPED"
	ContainerStateFailed  ContainerState = "FAILED"
	ContainerStateUnknown ContainerState = "UNKNOWN"
)

type EnvVar struct {
	Name      string `json:"name"`
	Value     string `json:"value,omitempty"`
	ValueFrom string `json:"valueFrom,omitempty"`
}

type PortMapping struct {
	ContainerPort int    `json:"containerPort"`
	HostPort      int    `json:"hostPort,omitempty"`
	Protocol      string `json:"protocol,omitempty"`
	HostIP        string `json:"hostIP,omitempty"`
}

type Mount struct {
	Type        string `json:"type,omitempty"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
	ReadOnly    bool   `json:"readOnly,omitempty"`
}

type Image struct {
	ID          string            `json:"id"`
	Repository  string            `json:"repository"`
	Tag         string            `json:"tag"`
	Size        int64             `json:"size,omitempty"`
	CreatedAt   time.Time        `json:"createdAt"`
	Labels      map[string]string `json:"labels,omitempty"`
}

type Pod struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Namespace   string         `json:"namespace"`
	Containers  []Container    `json:"containers"`
	State       PodState       `json:"state"`
	NodeName    string         `json:"nodeName,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	CreatedAt   time.Time      `json:"createdAt"`
}

type PodState string

const (
	PodStatePending   PodState = "PENDING"
	PodStateRunning   PodState = "RUNNING"
	PodStateSucceeded PodState = "SUCCEEDED"
	PodStateFailed    PodState = "FAILED"
	PodStateUnknown   PodState = "UNKNOWN"
)

type CreateContainerInput struct {
	Name      string            `json:"name"`
	Image     string            `json:"image"`
	Command   []string          `json:"command,omitempty"`
	Args      []string          `json:"args,omitempty"`
	Env       []EnvVar          `json:"env,omitempty"`
	Ports     []PortMapping     `json:"ports,omitempty"`
	Mounts    []Mount           `json:"mounts,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
	PodID     string            `json:"podId,omitempty"`
	Namespace string            `json:"namespace,omitempty"`
}

type CreatePodInput struct {
	Name        string         `json:"name"`
	Namespace   string         `json:"namespace,omitempty"`
	Containers  []ContainerSpec `json:"containers"`
	Labels      map[string]string `json:"labels,omitempty"`
	NodeName    string         `json:"nodeName,omitempty"`
}

type ContainerSpec struct {
	Name      string         `json:"name"`
	Image     string         `json:"image"`
	Command   []string       `json:"command,omitempty"`
	Args      []string       `json:"args,omitempty"`
	Env       []EnvVar       `json:"env,omitempty"`
	Ports     []PortMapping  `json:"ports,omitempty"`
	Mounts    []Mount        `json:"mounts,omitempty"`
	Resources ResourceRequirements `json:"resources,omitempty"`
}

type ResourceRequirements struct {
	Limits   ResourceList `json:"limits,omitempty"`
	Requests ResourceList `json:"requests,omitempty"`
}

type ResourceList struct {
	CPU    string `json:"cpu,omitempty"`
	Memory string `json:"memory,omitempty"`
}

func (c *Container) MarshalJSON() ([]byte, error) {
	type Alias Container
	return json.Marshal(&struct {
		*Alias
		CreatedAt string `json:"createdAt"`
	}{
		Alias:     (*Alias)(c),
		CreatedAt: c.CreatedAt.Format(time.RFC3339),
	})
}

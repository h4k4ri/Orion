package compute

import (
	"time"

	"github.com/horizon/orion/libs/go/kit/task"
)

type CreateServerRequest struct {
	Name             string   `json:"name"`
	ImageID          string   `json:"image_id"`
	Flavor           string   `json:"flavor"`
	Networks         []string `json:"networks"`
	SecurityGroupIDs []string `json:"security_group_ids,omitempty"`
	TraitsRequired   []string `json:"traits_required,omitempty"`
}

type AttachVolumeRequest struct {
	VolumeID string `json:"volume_id"`
}

type Server struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	Name      string    `json:"name"`
	ImageID   string    `json:"image_id"`
	Flavor    string    `json:"flavor"`
	VCPUs     int       `json:"vcpus"`
	MemoryMB  int       `json:"memory_mb"`
	DiskGB    int       `json:"disk_gb"`
	CellID    string    `json:"cell_id"`
	HostID    string    `json:"host_id"`
	PortIDs   []string  `json:"port_ids,omitempty"`
	VolumeIDs []string  `json:"volume_ids,omitempty"`
	Status    string    `json:"status"`
	TaskID    string    `json:"task_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type CreateServerResponse struct {
	Server Server    `json:"server"`
	Task   task.Task `json:"task"`
}

type CreateServerResponseAsync struct {
	ServerID    string `json:"server_id"`
	OperationID string `json:"operation_id"`
	Status      string `json:"status"`
}

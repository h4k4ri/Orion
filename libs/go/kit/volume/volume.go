package volume

import "time"

type CreateVolumeRequest struct {
	ProjectID string `json:"project_id"`
	Name      string `json:"name"`
	SizeGB    int    `json:"size_gb"`
}

type Volume struct {
	ID               string    `json:"id"`
	ProjectID        string    `json:"project_id"`
	Name             string    `json:"name"`
	SizeGB           int       `json:"size_gb"`
	CellID           string    `json:"cell_id"`
	HostID           string    `json:"host_id"`
	Status           string    `json:"status"`
	BackendType      string    `json:"backend_type"`
	DevicePath       string    `json:"device_path,omitempty"`
	AttachedServerID string    `json:"attached_server_id,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type AttachVolumeRequest struct {
	ServerID string `json:"server_id"`
	HostID   string `json:"host_id"`
}

type VolumeSnapshot struct {
	ID        string    `json:"id"`
	VolumeID  string    `json:"volume_id"`
	ProjectID string    `json:"project_id"`
	Name      string    `json:"name"`
	SizeGB    int       `json:"size_gb"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type CreateVolumeSnapshotRequest struct {
	ProjectID string `json:"project_id"`
	VolumeID  string `json:"volume_id"`
	Name      string `json:"name"`
}

package ports

import "context"

type SelectHostRequest struct {
	ServerID               string
	CellID                 string
	VCPUs                  int
	MemoryMB               int
	DiskGB                 int
	TraitsRequired         []string
	RequireNodeAgent       bool
	RequireVolumeHostAgent bool
}

type HostSelection struct {
	CellID string
	HostID string
}

type HostRecord struct {
	HostID              string
	NodeAgentURL        string
	NodeAgentGRPCURL    string
	VolumeHostAgentURL  string
	NetworkHostAgentURL string
}

type ReleaseHostRequest struct {
	ServerID string
	HostID   string
	VCPUs    int
	MemoryMB int
	DiskGB   int
}

type HostSelector interface {
	SelectHost(ctx context.Context, req SelectHostRequest) (HostSelection, error)
	ReleaseHost(ctx context.Context, req ReleaseHostRequest) error
}

type HostDirectory interface {
	GetHost(ctx context.Context, hostID string) (HostRecord, error)
}

package ports

import (
	"context"
	"errors"
)

type HostRecord struct {
	HostID             string
	CellID             string
	Enabled            bool
	Drained            bool
	VolumeHostAgentURL string
}

type SelectHostRequest struct {
	ServerID               string
	CellID                 string
	DiskGB                 int
	RequireVolumeHostAgent bool
}

type HostSelection struct {
	CellID string
	HostID string
}

type ReleaseHostRequest struct {
	ServerID string
	HostID   string
	DiskGB   int
}

var ErrNoValidHost = errors.New("no valid host")

type HostDirectory interface {
	GetHost(ctx context.Context, hostID string) (HostRecord, error)
	ListHosts(ctx context.Context) ([]HostRecord, error)
	SelectHost(ctx context.Context, req SelectHostRequest) (HostSelection, error)
	ReleaseHost(ctx context.Context, req ReleaseHostRequest) error
}

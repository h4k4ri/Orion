package grpcadapter

import (
	"context"

	placementv1 "github.com/horizon/orion/gen/go/placement/v1"
	"github.com/horizon/orion/services/orion-placement/internal/application"
	"github.com/horizon/orion/services/orion-placement/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Server struct {
	placementv1.UnimplementedPlacementServiceServer
	service *application.Service
}

func NewServer(service *application.Service) *Server {
	return &Server{service: service}
}

func (s *Server) RegisterHost(ctx context.Context, req *placementv1.RegisterHostRequest) (*placementv1.HostRegistrationResponse, error) {
	return s.register(ctx, req)
}

func (s *Server) Heartbeat(ctx context.Context, req *placementv1.HeartbeatRequest) (*placementv1.HostRegistrationResponse, error) {
	if req.GetHost() == nil {
		return nil, status.Error(codes.InvalidArgument, "host is required")
	}
	return s.heartbeat(ctx, req.GetHost())
}

func (s *Server) heartbeat(ctx context.Context, req *placementv1.RegisterHostRequest) (*placementv1.HostRegistrationResponse, error) {
	host, err := s.service.HeartbeatHost(ctx, toDomainRequest(req))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &placementv1.HostRegistrationResponse{Accepted: true, HostId: host.HostID, Generation: host.Generation}, nil
}

func (s *Server) register(ctx context.Context, req *placementv1.RegisterHostRequest) (*placementv1.HostRegistrationResponse, error) {
	if req.GetHostId() == "" {
		return nil, status.Error(codes.InvalidArgument, "host_id is required")
	}
	host := s.service.RegisterHost(ctx, toDomainRequest(req))
	return &placementv1.HostRegistrationResponse{Accepted: true, HostId: host.HostID, Generation: host.Generation}, nil
}

func toDomainRequest(req *placementv1.RegisterHostRequest) domain.RegisterHostRequest {
	return domain.RegisterHostRequest{
		HostID:              req.GetHostId(),
		CellID:              req.GetCellId(),
		Enabled:             true,
		NodeAgentURL:        req.GetNodeAgentUrl(),
		VolumeHostAgentURL:  req.GetVolumeHostAgentUrl(),
		NetworkHostAgentURL: req.GetNetworkHostAgentUrl(),
		Traits:              req.GetTraits(),
		VCPUs:               int(req.GetVcpusTotal()),
		MemoryMB:            int(req.GetMemoryMbTotal()),
		DiskGB:              int(req.GetDiskGbTotal()),
		AvailabilityZone:    req.GetAvailabilityZone(),
	}
}

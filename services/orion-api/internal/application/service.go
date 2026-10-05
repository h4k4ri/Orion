package application

import (
	"context"
	"errors"
	"slices"

	"github.com/horizon/orion/libs/go/kit/authn"
	"github.com/horizon/orion/libs/go/kit/compute"
	"github.com/horizon/orion/libs/go/kit/task"
	volumekit "github.com/horizon/orion/libs/go/kit/volume"
	"github.com/horizon/orion/services/orion-api/internal/domain"
	"github.com/horizon/orion/services/orion-api/internal/ports"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

var (
	ErrProjectScopeRequired      = errors.New("project scope required")
	ErrForbidden                 = errors.New("forbidden")
	ErrNetworkBackendUnavailable = errors.New("network backend unavailable")
	ErrNetworkNotFound           = errors.New("network not found")
	ErrImageNotFound             = errors.New("image not found")
	ErrUnknownFlavor             = errors.New("unknown flavor")
	ErrNoValidHost               = errors.New("no valid host")
	ErrQuotaExceeded             = errors.New("project quota exceeded")
	ErrServerNotFound            = errors.New("server not found")
	ErrTaskNotFound              = errors.New("task not found")
	ErrVolumeBackendUnavailable  = errors.New("volume backend unavailable")
	ErrVolumeNotFound            = errors.New("volume not found")
	ErrVolumeInUse               = errors.New("volume in use")
	ErrVolumeNotAttached         = errors.New("volume not attached")
)

var tracer = otel.Tracer("github.com/horizon/orion/services/orion-api")

type Service struct {
	compute   ports.ComputeClient
	volume    ports.VolumeClient
	operation ports.OperationClient
}

func NewService(computeClient ports.ComputeClient, volumeClient ports.VolumeClient, operationClient ports.OperationClient) *Service {
	return &Service{
		compute:   computeClient,
		volume:    volumeClient,
		operation: operationClient,
	}
}

func (s *Service) CreateServerAsync(ctx context.Context, actor authn.Actor, req compute.CreateServerRequest) (compute.CreateServerResponseAsync, error) {
	if actor.Scope.Type != authn.ScopeTypeProject {
		return compute.CreateServerResponseAsync{}, ErrProjectScopeRequired
	}
	if !authn.HasRole(actor.Roles, authn.RoleAdmin, authn.RoleMember) {
		return compute.CreateServerResponseAsync{}, ErrForbidden
	}
	response, err := s.compute.CreateServerAsync(ctx, actor, req)
	if err != nil {
		return compute.CreateServerResponseAsync{}, mapComputeError(err)
	}
	return response, nil
}

func (s *Service) GetOperation(ctx context.Context, actor authn.Actor, operationID string) (domain.Operation, error) {
	if actor.Scope.Type != authn.ScopeTypeProject && actor.Scope.Type != authn.ScopeTypeSystem {
		return domain.Operation{}, ErrForbidden
	}
	operation, err := s.operation.GetOperation(ctx, operationID)
	if err != nil {
		return domain.Operation{}, err
	}
	if actor.Scope.Type == authn.ScopeTypeProject && operation.ProjectID != actor.Scope.ProjectID {
		return domain.Operation{}, ErrForbidden
	}
	return operation, nil
}

func (s *Service) CreateServer(ctx context.Context, actor authn.Actor, req compute.CreateServerRequest) (compute.Server, task.Task, error) {
	ctx, span := tracer.Start(ctx, "api.CreateServer")
	defer span.End()
	span.SetAttributes(
		attribute.String("user_id", actor.UserID),
		attribute.String("project_id", actor.Scope.ProjectID),
		attribute.String("server_name", req.Name),
	)

	if actor.Scope.Type != authn.ScopeTypeProject {
		return compute.Server{}, task.Task{}, ErrProjectScopeRequired
	}

	if !authn.HasRole(actor.Roles, authn.RoleAdmin, authn.RoleMember) {
		return compute.Server{}, task.Task{}, ErrForbidden
	}

	server, resourceTask, err := s.compute.CreateServer(ctx, actor, req)
	if err != nil {
		switch {
		case errors.Is(err, ports.ErrNetworkBackendUnavailable):
			return compute.Server{}, task.Task{}, ErrNetworkBackendUnavailable
		case errors.Is(err, ports.ErrNetworkNotFound):
			return compute.Server{}, task.Task{}, ErrNetworkNotFound
		case errors.Is(err, ports.ErrImageNotFound):
			return compute.Server{}, task.Task{}, ErrImageNotFound
		case errors.Is(err, ports.ErrUnknownFlavor):
			return compute.Server{}, task.Task{}, ErrUnknownFlavor
		case errors.Is(err, ports.ErrNoValidHost):
			return compute.Server{}, task.Task{}, ErrNoValidHost
		default:
			return compute.Server{}, task.Task{}, err
		}
	}

	span.SetAttributes(attribute.String("server_id", server.ID))
	return server, resourceTask, nil
}

func (s *Service) ListServers(ctx context.Context, actor authn.Actor) ([]compute.Server, error) {
	ctx, span := tracer.Start(ctx, "api.ListServers")
	defer span.End()
	span.SetAttributes(attribute.String("project_id", actor.Scope.ProjectID))

	if actor.Scope.Type != authn.ScopeTypeProject && actor.Scope.Type != authn.ScopeTypeSystem {
		return nil, ErrForbidden
	}
	servers, err := s.compute.ListServers(ctx, actor)
	if err != nil {
		return nil, err
	}
	if actor.Scope.Type == authn.ScopeTypeSystem {
		span.SetAttributes(attribute.Int("count", len(servers)))
		return servers, nil
	}
	filtered := make([]compute.Server, 0, len(servers))
	for _, server := range servers {
		if server.ProjectID == actor.Scope.ProjectID {
			filtered = append(filtered, server)
		}
	}
	span.SetAttributes(attribute.Int("count", len(filtered)))
	return filtered, nil
}

func (s *Service) GetServer(ctx context.Context, actor authn.Actor, serverID string) (compute.Server, error) {
	ctx, span := tracer.Start(ctx, "api.GetServer")
	defer span.End()
	span.SetAttributes(
		attribute.String("server_id", serverID),
		attribute.String("project_id", actor.Scope.ProjectID),
	)

	if actor.Scope.Type != authn.ScopeTypeProject && actor.Scope.Type != authn.ScopeTypeSystem {
		return compute.Server{}, ErrForbidden
	}

	server, err := s.compute.GetServer(ctx, actor, serverID)
	if err != nil {
		if errors.Is(err, ports.ErrServerNotFound) {
			return compute.Server{}, ErrServerNotFound
		}
		return compute.Server{}, err
	}

	if actor.Scope.Type == authn.ScopeTypeProject && server.ProjectID != actor.Scope.ProjectID {
		return compute.Server{}, ErrForbidden
	}

	return server, nil
}

func (s *Service) GetTask(ctx context.Context, actor authn.Actor, taskID string) (task.Task, error) {
	ctx, span := tracer.Start(ctx, "api.GetTask")
	defer span.End()
	span.SetAttributes(attribute.String("task_id", taskID))

	if actor.Scope.Type != authn.ScopeTypeProject && actor.Scope.Type != authn.ScopeTypeSystem {
		return task.Task{}, ErrForbidden
	}

	resourceTask, err := s.compute.GetTask(ctx, actor, taskID)
	if err != nil {
		if errors.Is(err, ports.ErrTaskNotFound) {
			return task.Task{}, ErrTaskNotFound
		}
		return task.Task{}, err
	}

	return resourceTask, nil
}

func (s *Service) DeleteServer(ctx context.Context, actor authn.Actor, serverID string) (task.Task, error) {
	ctx, span := tracer.Start(ctx, "api.DeleteServer")
	defer span.End()
	span.SetAttributes(attribute.String("server_id", serverID))

	server, err := s.GetServer(ctx, actor, serverID)
	if err != nil {
		return task.Task{}, err
	}
	_ = server
	resourceTask, err := s.compute.DeleteServer(ctx, actor, serverID)
	if err != nil {
		if errors.Is(err, ports.ErrServerNotFound) {
			return task.Task{}, ErrServerNotFound
		}
		return task.Task{}, err
	}
	return resourceTask, nil
}

func (s *Service) AttachVolume(ctx context.Context, actor authn.Actor, serverID, volumeID string) (compute.Server, task.Task, error) {
	ctx, span := tracer.Start(ctx, "api.AttachVolume")
	defer span.End()
	span.SetAttributes(
		attribute.String("server_id", serverID),
		attribute.String("volume_id", volumeID),
	)

	if actor.Scope.Type != authn.ScopeTypeProject {
		return compute.Server{}, task.Task{}, ErrProjectScopeRequired
	}
	if !authn.HasRole(actor.Roles, authn.RoleAdmin, authn.RoleMember) {
		return compute.Server{}, task.Task{}, ErrForbidden
	}
	server, err := s.GetServer(ctx, actor, serverID)
	if err != nil {
		return compute.Server{}, task.Task{}, err
	}
	volume, err := s.GetVolume(ctx, actor, volumeID)
	if err != nil {
		return compute.Server{}, task.Task{}, err
	}
	if server.CellID != volume.CellID {
		return compute.Server{}, task.Task{}, ErrForbidden
	}
	server, resourceTask, err := s.compute.AttachVolume(ctx, actor, serverID, volumeID)
	if err != nil {
		return compute.Server{}, task.Task{}, mapComputeVolumeError(err)
	}
	return server, resourceTask, nil
}

func (s *Service) DetachVolume(ctx context.Context, actor authn.Actor, serverID, volumeID string) (compute.Server, task.Task, error) {
	ctx, span := tracer.Start(ctx, "api.DetachVolume")
	defer span.End()
	span.SetAttributes(
		attribute.String("server_id", serverID),
		attribute.String("volume_id", volumeID),
	)

	if actor.Scope.Type != authn.ScopeTypeProject {
		return compute.Server{}, task.Task{}, ErrProjectScopeRequired
	}
	if !authn.HasRole(actor.Roles, authn.RoleAdmin, authn.RoleMember) {
		return compute.Server{}, task.Task{}, ErrForbidden
	}
	server, err := s.GetServer(ctx, actor, serverID)
	if err != nil {
		return compute.Server{}, task.Task{}, err
	}
	if !slices.Contains(server.VolumeIDs, volumeID) {
		return compute.Server{}, task.Task{}, ports.ErrVolumeNotAttached
	}
	server, resourceTask, err := s.compute.DetachVolume(ctx, actor, serverID, volumeID)
	if err != nil {
		return compute.Server{}, task.Task{}, mapComputeVolumeError(err)
	}
	return server, resourceTask, nil
}

func (s *Service) CreateVolume(ctx context.Context, actor authn.Actor, req volumekit.CreateVolumeRequest) (volumekit.Volume, error) {
	ctx, span := tracer.Start(ctx, "api.CreateVolume")
	defer span.End()
	span.SetAttributes(
		attribute.String("project_id", actor.Scope.ProjectID),
		attribute.Int("size_gb", req.SizeGB),
	)

	if actor.Scope.Type != authn.ScopeTypeProject {
		return volumekit.Volume{}, ErrProjectScopeRequired
	}
	if !authn.HasRole(actor.Roles, authn.RoleAdmin, authn.RoleMember) {
		return volumekit.Volume{}, ErrForbidden
	}
	req.ProjectID = actor.Scope.ProjectID
	volume, err := s.volume.CreateVolume(ctx, actor, req)
	if err != nil {
		return volumekit.Volume{}, mapComputeVolumeError(err)
	}

	span.SetAttributes(attribute.String("volume_id", volume.ID))
	return volume, nil
}

func (s *Service) ListVolumes(ctx context.Context, actor authn.Actor) ([]volumekit.Volume, error) {
	ctx, span := tracer.Start(ctx, "api.ListVolumes")
	defer span.End()
	span.SetAttributes(attribute.String("project_id", actor.Scope.ProjectID))

	if actor.Scope.Type != authn.ScopeTypeProject && actor.Scope.Type != authn.ScopeTypeSystem {
		return nil, ErrForbidden
	}
	volumes, err := s.volume.ListVolumes(ctx, actor)
	if err != nil {
		return nil, mapComputeVolumeError(err)
	}
	if actor.Scope.Type == authn.ScopeTypeSystem {
		span.SetAttributes(attribute.Int("count", len(volumes)))
		return volumes, nil
	}
	filtered := make([]volumekit.Volume, 0, len(volumes))
	for _, volume := range volumes {
		if volume.ProjectID == actor.Scope.ProjectID {
			filtered = append(filtered, volume)
		}
	}
	span.SetAttributes(attribute.Int("count", len(filtered)))
	return filtered, nil
}

func (s *Service) GetVolume(ctx context.Context, actor authn.Actor, volumeID string) (volumekit.Volume, error) {
	ctx, span := tracer.Start(ctx, "api.GetVolume")
	defer span.End()
	span.SetAttributes(
		attribute.String("volume_id", volumeID),
		attribute.String("project_id", actor.Scope.ProjectID),
	)

	if actor.Scope.Type != authn.ScopeTypeProject && actor.Scope.Type != authn.ScopeTypeSystem {
		return volumekit.Volume{}, ErrForbidden
	}
	volume, err := s.volume.GetVolume(ctx, actor, volumeID)
	if err != nil {
		return volumekit.Volume{}, mapComputeVolumeError(err)
	}
	if actor.Scope.Type == authn.ScopeTypeProject && volume.ProjectID != actor.Scope.ProjectID {
		return volumekit.Volume{}, ErrForbidden
	}
	return volume, nil
}

func (s *Service) DeleteVolume(ctx context.Context, actor authn.Actor, volumeID string) error {
	ctx, span := tracer.Start(ctx, "api.DeleteVolume")
	defer span.End()
	span.SetAttributes(attribute.String("volume_id", volumeID))

	volume, err := s.GetVolume(ctx, actor, volumeID)
	if err != nil {
		return err
	}
	if volume.AttachedServerID != "" {
		return ports.ErrVolumeInUse
	}
	if err := s.volume.DeleteVolume(ctx, actor, volumeID); err != nil {
		return mapComputeVolumeError(err)
	}
	return nil
}

func mapComputeVolumeError(err error) error {
	switch {
	case errors.Is(err, ports.ErrVolumeBackendUnavailable):
		return ErrVolumeBackendUnavailable
	case errors.Is(err, ports.ErrVolumeNotFound):
		return ErrVolumeNotFound
	case errors.Is(err, ports.ErrVolumeInUse):
		return ErrVolumeInUse
	case errors.Is(err, ports.ErrVolumeNotAttached):
		return ErrVolumeNotAttached
	default:
		return err
	}
}

func mapComputeError(err error) error {
	switch {
	case errors.Is(err, ports.ErrNetworkBackendUnavailable):
		return ErrNetworkBackendUnavailable
	case errors.Is(err, ports.ErrNetworkNotFound):
		return ErrNetworkNotFound
	case errors.Is(err, ports.ErrImageNotFound):
		return ErrImageNotFound
	case errors.Is(err, ports.ErrUnknownFlavor):
		return ErrUnknownFlavor
	case errors.Is(err, ports.ErrNoValidHost):
		return ErrNoValidHost
	case errors.Is(err, ports.ErrQuotaExceeded):
		return ErrQuotaExceeded
	default:
		return err
	}
}

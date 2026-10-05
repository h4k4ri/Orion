package application

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/horizon/orion/libs/go/kit/compute"
	"github.com/horizon/orion/libs/go/kit/events"
	"github.com/horizon/orion/libs/go/kit/httpx"
	"github.com/horizon/orion/libs/go/kit/ids"
	networkkit "github.com/horizon/orion/libs/go/kit/network"
	"github.com/horizon/orion/libs/go/kit/task"
	natsadapter "github.com/horizon/orion/services/orion-compute/internal/adapters/nats"
	operationclient "github.com/horizon/orion/services/orion-compute/internal/adapters/operationclient"
	placementclient "github.com/horizon/orion/services/orion-compute/internal/adapters/placementclient"
	"github.com/horizon/orion/services/orion-compute/internal/ports"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var (
	ErrNetworkBackendUnavailable = errors.New("network backend unavailable")
	ErrVolumeBackendUnavailable  = errors.New("volume backend unavailable")
	ErrNetworkNotFound           = errors.New("network not found")
	ErrImageNotFound             = errors.New("image not found")
	ErrUnknownFlavor             = errors.New("unknown flavor")
	ErrNoValidHost               = errors.New("no valid host")
	ErrServerNotFound            = errors.New("server not found")
	ErrTaskNotFound              = errors.New("task not found")
	ErrVolumeNotFound            = errors.New("volume not found")
	ErrVolumeInUse               = errors.New("volume in use")
	ErrVolumeNotAttached         = errors.New("volume not attached")
	ErrQuotaExceeded             = ports.ErrQuotaExceeded
)

type Service struct {
	store        ports.Store
	publisher    events.Publisher
	cmdPublisher *natsadapter.CommandPublisher
	opClient     *operationclient.Client
	images       ports.ImageResolver
	networks     ports.NetworkAllocator
	volumes      ports.VolumeManager
	selector     ports.HostSelector
	executor     ports.Executor
}

var tracer = otel.Tracer("github.com/horizon/orion/services/orion-compute")

func NewService(
	store ports.Store,
	publisher events.Publisher,
	cmdPublisher *natsadapter.CommandPublisher,
	opClient *operationclient.Client,
	images ports.ImageResolver,
	networks ports.NetworkAllocator,
	volumes ports.VolumeManager,
	selector ports.HostSelector,
	executor ports.Executor,
) *Service {
	return &Service{
		store:        store,
		publisher:    publisher,
		cmdPublisher: cmdPublisher,
		opClient:     opClient,
		images:       images,
		networks:     networks,
		volumes:      volumes,
		selector:     selector,
		executor:     executor,
	}
}

func (s *Service) CreateServer(ctx context.Context, actorUserID string, projectID string, req compute.CreateServerRequest) (compute.Server, task.Task, error) {
	ctx, span := tracer.Start(ctx, "compute.CreateServer")
	defer span.End()

	now := time.Now().UTC()
	serverID := ids.New("srv")
	taskID := ids.New("tsk")
	requestID := httpx.RequestIDFromContextOrNew(ctx)
	img, err := s.images.GetImage(ctx, req.ImageID)
	if err != nil {
		if errors.Is(err, ports.ErrImageNotFound) {
			return compute.Server{}, task.Task{}, ErrImageNotFound
		}
		return compute.Server{}, task.Task{}, err
	}

	resources, err := resourcesForFlavor(req.Flavor)
	if err != nil {
		return compute.Server{}, task.Task{}, err
	}
	if resources.DiskGB < img.MinDiskGB {
		resources.DiskGB = img.MinDiskGB
	}
	server := compute.Server{
		ID:        serverID,
		ProjectID: projectID,
		Name:      req.Name,
		ImageID:   req.ImageID,
		Flavor:    req.Flavor,
		VCPUs:     resources.VCPUs,
		MemoryMB:  resources.MemoryMB,
		DiskGB:    resources.DiskGB,
		Status:    "building",
		TaskID:    taskID,
		CreatedAt: now,
		UpdatedAt: now,
	}

	resourceTask := task.Task{
		ID:          taskID,
		Kind:        "server.create",
		Scope:       "project",
		TargetRef:   serverID,
		Status:      task.StatusRunning,
		RequestedBy: actorUserID,
		RequestID:   requestID,
		CellID:      server.CellID,
		HostID:      server.HostID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	resourceTask, created, err := s.claimTask(ctx, resourceTask)
	if err != nil {
		return compute.Server{}, task.Task{}, err
	}
	if !created {
		return s.existingCreateResult(ctx, resourceTask)
	}
	if err := s.store.SaveServer(ctx, server); err != nil {
		return compute.Server{}, task.Task{}, err
	}

	selection, err := s.selector.SelectHost(ctx, ports.SelectHostRequest{
		ServerID:         serverID,
		CellID:           "cell_local",
		VCPUs:            resources.VCPUs,
		MemoryMB:         resources.MemoryMB,
		DiskGB:           resources.DiskGB,
		TraitsRequired:   req.TraitsRequired,
		RequireNodeAgent: true,
	})
	if err != nil {
		if errors.Is(err, placementclient.ErrNoValidHost) {
			return s.failServerTask(ctx, server, resourceTask, "no_valid_host", ErrNoValidHost)
		}
		return s.failServerTask(ctx, server, resourceTask, "host_selection_failed", err)
	}

	server.CellID = selection.CellID
	server.HostID = selection.HostID
	resourceTask.CellID = server.CellID
	resourceTask.HostID = server.HostID
	if err := s.store.SaveServer(ctx, server); err != nil {
		return s.failCreateServerTask(ctx, server, resourceTask, "server_persist_failed", err, nil, false, true)
	}
	s.storeTask(ctx, resourceTask)

	serverPorts := make([]networkkit.Port, 0, len(req.Networks))
	portIDs := make([]string, 0, len(req.Networks))
	for _, networkID := range req.Networks {
		port, err := s.networks.CreatePort(ctx, networkkit.CreatePortRequest{
			ProjectID:        projectID,
			NetworkID:        networkID,
			DeviceID:         serverID,
			DeviceOwner:      "compute:orion",
			BindingHostID:    selection.HostID,
			SecurityGroupIDs: req.SecurityGroupIDs,
		})
		if err != nil {
			if errors.Is(err, ports.ErrNetworkNotFound) {
				return s.failCreateServerTask(ctx, server, resourceTask, "network_not_found", ErrNetworkNotFound, portIDs, false, true)
			}
			if errors.Is(err, ports.ErrNetworkBackendUnavailable) {
				return s.failCreateServerTask(ctx, server, resourceTask, "network_backend_unavailable", ErrNetworkBackendUnavailable, portIDs, false, true)
			}
			return s.failCreateServerTask(ctx, server, resourceTask, "port_allocation_failed", err, portIDs, false, true)
		}
		serverPorts = append(serverPorts, port)
		portIDs = append(portIDs, port.ID)
	}
	server.PortIDs = portIDs
	if err := s.store.SaveServer(ctx, server); err != nil {
		return s.failCreateServerTask(ctx, server, resourceTask, "server_persist_failed", err, portIDs, false, true)
	}

	span.SetAttributes(
		attribute.String("orion.server_id", serverID),
		attribute.String("orion.task_id", resourceTask.ID),
		attribute.String("orion.project_id", projectID),
	)

	builtServer, err := s.executor.BuildServer(ctx, server, img, serverPorts, resources.VCPUs, resources.MemoryMB, resources.DiskGB)
	if err != nil {
		return s.failCreateServerTask(ctx, server, resourceTask, "build_failed", err, portIDs, true, true)
	}
	server = builtServer
	resourceTask.Status = task.StatusSucceeded
	resourceTask.UpdatedAt = server.UpdatedAt

	_ = s.store.SaveServer(ctx, server)
	_ = s.store.SaveTask(ctx, resourceTask)
	s.publishTask(ctx, resourceTask)

	return server, resourceTask, nil
}

func (s *Service) CreateServerAsync(ctx context.Context, actorUserID string, projectID string, req compute.CreateServerRequest) (compute.Server, string, error) {
	ctx, span := tracer.Start(ctx, "compute.CreateServerAsync")
	defer span.End()

	now := time.Now().UTC()
	requestID := httpx.RequestIDFromContextOrNew(ctx)
	const createKind = "server.create.async"
	if existingTask, lookupErr := s.store.GetTaskByRequest(ctx, createKind, requestID); lookupErr == nil {
		server, serverErr := s.store.GetServer(ctx, existingTask.TargetRef)
		if serverErr != nil && !errors.Is(serverErr, ports.ErrServerNotFound) {
			return compute.Server{}, "", serverErr
		}
		return server, operationIDForTask(existingTask.ID), nil
	} else if !errors.Is(lookupErr, ports.ErrTaskNotFound) {
		return compute.Server{}, "", lookupErr
	}
	serverID := ids.New("srv")

	img, err := s.images.GetImage(ctx, req.ImageID)
	if err != nil {
		if errors.Is(err, ports.ErrImageNotFound) {
			return compute.Server{}, "", ErrImageNotFound
		}
		return compute.Server{}, "", err
	}

	resources, err := resourcesForFlavor(req.Flavor)
	if err != nil {
		return compute.Server{}, "", err
	}
	if resources.DiskGB < img.MinDiskGB {
		resources.DiskGB = img.MinDiskGB
	}
	quotaStore, quotaEnabled := s.store.(ports.QuotaStore)
	quotaCommitted := false
	if quotaEnabled {
		if err := quotaStore.AllocateProjectQuota(ctx, projectID, 1, resources.VCPUs, resources.MemoryMB); err != nil {
			return compute.Server{}, "", err
		}
		defer func() {
			if !quotaCommitted {
				_ = quotaStore.ReleaseProjectQuota(context.Background(), projectID, 1, resources.VCPUs, resources.MemoryMB)
			}
		}()
	}

	server := compute.Server{
		ID:        serverID,
		ProjectID: projectID,
		Name:      req.Name,
		ImageID:   req.ImageID,
		Flavor:    req.Flavor,
		VCPUs:     resources.VCPUs,
		MemoryMB:  resources.MemoryMB,
		DiskGB:    resources.DiskGB,
		Status:    "building",
		TaskID:    "",
		CreatedAt: now,
		UpdatedAt: now,
	}
	resourceTask := task.Task{
		ID:          ids.New("tsk"),
		Kind:        createKind,
		Scope:       "project",
		TargetRef:   serverID,
		Status:      task.StatusRunning,
		RequestedBy: actorUserID,
		RequestID:   requestID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	claimedTask, created, err := s.store.CreateTaskIfAbsent(ctx, resourceTask)
	if err != nil {
		return compute.Server{}, "", err
	}
	if !created {
		server, serverErr := s.store.GetServer(ctx, claimedTask.TargetRef)
		if serverErr != nil && !errors.Is(serverErr, ports.ErrServerNotFound) {
			return compute.Server{}, "", serverErr
		}
		return server, operationIDForTask(claimedTask.ID), nil
	}
	resourceTask = claimedTask
	server.TaskID = resourceTask.ID

	if err := s.store.SaveServer(ctx, server); err != nil {
		return compute.Server{}, "", err
	}

	selection, err := s.selector.SelectHost(ctx, ports.SelectHostRequest{
		ServerID:         serverID,
		CellID:           "cell_local",
		VCPUs:            resources.VCPUs,
		MemoryMB:         resources.MemoryMB,
		DiskGB:           resources.DiskGB,
		TraitsRequired:   req.TraitsRequired,
		RequireNodeAgent: true,
	})
	if err != nil {
		if errors.Is(err, placementclient.ErrNoValidHost) {
			return s.failServerAsync(ctx, server, "no_valid_host", ErrNoValidHost)
		}
		return s.failServerAsync(ctx, server, "host_selection_failed", err)
	}

	server.CellID = selection.CellID
	server.HostID = selection.HostID
	if err := s.store.SaveServer(ctx, server); err != nil {
		return s.failServerAsync(ctx, server, "server_persist_failed", err)
	}

	serverPorts := make([]networkkit.Port, 0, len(req.Networks))
	portPayloads := make([]natsadapter.PortPayload, 0, len(req.Networks))
	for _, networkID := range req.Networks {
		port, err := s.networks.CreatePort(ctx, networkkit.CreatePortRequest{
			ProjectID:        projectID,
			NetworkID:        networkID,
			DeviceID:         serverID,
			DeviceOwner:      "compute:orion",
			BindingHostID:    selection.HostID,
			SecurityGroupIDs: req.SecurityGroupIDs,
		})
		if err != nil {
			if errors.Is(err, ports.ErrNetworkNotFound) {
				return s.failServerAsync(ctx, server, "network_not_found", ErrNetworkNotFound)
			}
			if errors.Is(err, ports.ErrNetworkBackendUnavailable) {
				return s.failServerAsync(ctx, server, "network_backend_unavailable", ErrNetworkBackendUnavailable)
			}
			return s.failServerAsync(ctx, server, "port_allocation_failed", err)
		}
		serverPorts = append(serverPorts, port)
		var ipAddr string
		if len(port.FixedIPs) > 0 {
			ipAddr = port.FixedIPs[0].IPAddress
		}
		portPayloads = append(portPayloads, natsadapter.PortPayload{
			ID:         port.ID,
			MACAddress: port.MACAddress,
			IPAddress:  ipAddr,
			NetworkID:  networkID,
		})
	}
	server.PortIDs = make([]string, len(serverPorts))
	for i, p := range serverPorts {
		server.PortIDs[i] = p.ID
	}
	if err := s.store.SaveServer(ctx, server); err != nil {
		return s.failServerAsync(ctx, server, "server_persist_failed", err)
	}

	opResp, err := s.opClient.CreateOperation(ctx, operationclient.CreateOperationRequest{
		ID:            operationIDForTask(resourceTask.ID),
		ResourceType:  "server",
		ResourceID:    serverID,
		ProjectID:     projectID,
		RequestID:     requestID,
		OperationType: "server.create",
	})
	if err != nil {
		return s.failServerAsync(ctx, server, "operation_create_failed", err)
	}

	transitions := []struct {
		state string
		step  string
	}{
		{state: "VALIDATING", step: "validate_flavor"},
		{state: "SCHEDULING", step: "select_host"},
		{state: "ALLOCATING_NETWORK", step: "allocate_network"},
		{state: "PREPARING_STORAGE", step: "allocate_volume"},
		{state: "SPAWNING", step: "send_spawn_command"},
	}
	for _, transition := range transitions {
		if err := s.opClient.TransitionOperation(ctx, opResp.ID, transition.state, transition.step, nil); err != nil {
			_ = s.opClient.TransitionOperation(ctx, opResp.ID, "FAILED", "", []byte(err.Error()))
			return s.failServerAsync(ctx, server, "operation_transition_failed", err)
		}
	}

	imagePayload := natsadapter.ImagePayload{
		ID:             img.ID,
		SourcePath:     img.Path,
		ChecksumSHA256: img.ChecksumSHA256,
		SizeBytes:      img.SizeBytes,
	}

	buildPayload := natsadapter.BuildInstancePayload{
		InstanceID: serverID,
		HostID:     selection.HostID,
		Name:       req.Name,
		VCPUs:      resources.VCPUs,
		MemoryMB:   int64(resources.MemoryMB),
		DiskGB:     resources.DiskGB,
		Image:      &imagePayload,
		Ports:      portPayloads,
	}

	msgID := ids.New("msg")
	if err := s.cmdPublisher.PublishInstanceCreate(ctx, msgID, opResp.ID, requestID, buildPayload); err != nil {
		_ = s.opClient.FailOperation(ctx, opResp.ID, err.Error())
		return s.failServerAsync(ctx, server, "command_publish_failed", err)
	}
	if err := s.cmdPublisher.PublishDesiredState(ctx, selection.HostID, now.UnixNano(), &natsadapter.DesiredInstance{
		InstanceID:      serverID,
		Name:            req.Name,
		VCPUs:           resources.VCPUs,
		MemoryMB:        resources.MemoryMB,
		DiskGB:          resources.DiskGB,
		ImageID:         img.ID,
		ImageSourcePath: img.Path,
		DesiredState:    "Running",
		Generation:      now.UnixNano(),
	}, ""); err != nil {
		_ = s.opClient.FailOperation(ctx, opResp.ID, err.Error())
		return s.failServerAsync(ctx, server, "desired_state_publish_failed", err)
	}

	span.SetAttributes(
		attribute.String("orion.server_id", serverID),
		attribute.String("orion.operation_id", opResp.ID),
		attribute.String("orion.project_id", projectID),
	)
	quotaCommitted = true
	return server, opResp.ID, nil
}

func (s *Service) failServerAsync(ctx context.Context, server compute.Server, code string, err error) (compute.Server, string, error) {
	server.Status = "error"
	server.UpdatedAt = time.Now().UTC()
	_ = s.store.SaveServer(ctx, server)
	return server, "", err
}

func operationIDForTask(taskID string) string {
	return "op_" + strings.TrimPrefix(taskID, "tsk_")
}

func (s *Service) ListServers(ctx context.Context) []compute.Server {
	items, _ := s.store.ListServers(ctx)
	return items
}

func (s *Service) Reconcile(ctx context.Context) (int, error) {
	ctx, span := tracer.Start(ctx, "compute.Reconcile")
	defer span.End()

	servers, err := s.store.ListServers(ctx)
	if err != nil {
		return 0, err
	}

	reconciled := 0
	for _, server := range servers {
		observed, err := s.executor.ObserveServer(ctx, server)
		if errors.Is(err, ports.ErrServerInstanceNotFound) {
			if server.Status != "error" {
				server.Status = "error"
				server.UpdatedAt = time.Now().UTC()
				if saveErr := s.store.SaveServer(ctx, server); saveErr != nil {
					return reconciled, saveErr
				}
				reconciled++
			}
			continue
		}
		if err != nil {
			return reconciled, err
		}
		if observed.Status == "" || observed.Status == server.Status {
			continue
		}

		server.Status = observed.Status
		server.UpdatedAt = time.Now().UTC()
		if err := s.store.SaveServer(ctx, server); err != nil {
			return reconciled, err
		}
		reconciled++
	}

	span.SetAttributes(attribute.Int("orion.reconciled_servers", reconciled))
	return reconciled, nil
}

func (s *Service) GetServer(ctx context.Context, serverID string) (compute.Server, error) {
	server, err := s.store.GetServer(ctx, serverID)
	if errors.Is(err, ports.ErrServerNotFound) {
		return compute.Server{}, ErrServerNotFound
	}
	return server, err
}

func (s *Service) GetTask(ctx context.Context, taskID string) (task.Task, error) {
	t, err := s.store.GetTask(ctx, taskID)
	if errors.Is(err, ports.ErrTaskNotFound) {
		return task.Task{}, ErrTaskNotFound
	}
	return t, err
}

func (s *Service) DeleteServer(ctx context.Context, actorUserID string, serverID string) (task.Task, error) {
	ctx, span := tracer.Start(ctx, "compute.DeleteServer")
	defer span.End()

	now := time.Now().UTC()
	resourceTask := task.Task{
		ID:          ids.New("tsk"),
		Kind:        "server.delete",
		Scope:       "project",
		TargetRef:   serverID,
		Status:      task.StatusRunning,
		RequestedBy: actorUserID,
		RequestID:   httpx.RequestIDFromContextOrNew(ctx),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	resourceTask, created, err := s.claimTask(ctx, resourceTask)
	if err != nil {
		return task.Task{}, err
	}
	if !created {
		return resourceTask, nil
	}

	server, err := s.store.GetServer(ctx, serverID)
	if errors.Is(err, ports.ErrServerNotFound) {
		resourceTask.Status = task.StatusFailed
		resourceTask.ErrorCode = "server_not_found"
		resourceTask.ErrorMessage = ErrServerNotFound.Error()
		resourceTask.UpdatedAt = time.Now().UTC()
		s.storeTask(ctx, resourceTask)
		return resourceTask, ErrServerNotFound
	}
	if err != nil {
		return task.Task{}, err
	}
	resourceTask.CellID = server.CellID
	resourceTask.HostID = server.HostID
	s.storeTask(ctx, resourceTask)
	finalizerStore, finalizersEnabled := s.store.(ports.FinalizerStore)
	if finalizersEnabled {
		if err := finalizerStore.EnsureFinalizers(ctx, "server", server.ID, []string{"network.cleanup", "volume.detach", "compute.destroy", "placement.release"}); err != nil {
			return task.Task{}, err
		}
		server.Status = "deleting"
		server.UpdatedAt = time.Now().UTC()
		_ = s.store.SaveServer(ctx, server)
	}

	span.SetAttributes(
		attribute.String("orion.server_id", serverID),
		attribute.String("orion.task_id", resourceTask.ID),
	)

	for _, volumeID := range server.VolumeIDs {
		volume, err := s.volumes.GetVolume(ctx, volumeID)
		if err != nil {
			resourceTask.Status = task.StatusFailed
			resourceTask.ErrorCode = "volume_lookup_failed"
			resourceTask.ErrorMessage = err.Error()
			resourceTask.UpdatedAt = time.Now().UTC()
			s.storeTask(ctx, resourceTask)
			return resourceTask, mapVolumeError(err)
		}
		if err := s.executor.DetachVolume(ctx, server, volume); err != nil {
			resourceTask.Status = task.StatusFailed
			resourceTask.ErrorCode = "volume_detach_failed"
			resourceTask.ErrorMessage = err.Error()
			resourceTask.UpdatedAt = time.Now().UTC()
			s.storeTask(ctx, resourceTask)
			return resourceTask, err
		}
		if _, err := s.volumes.DetachVolume(ctx, volumeID); err != nil {
			resourceTask.Status = task.StatusFailed
			resourceTask.ErrorCode = "volume_state_detach_failed"
			resourceTask.ErrorMessage = err.Error()
			resourceTask.UpdatedAt = time.Now().UTC()
			s.storeTask(ctx, resourceTask)
			return resourceTask, mapVolumeError(err)
		}
	}
	if finalizersEnabled {
		_ = finalizerStore.RemoveFinalizer(ctx, "server", server.ID, "volume.detach")
	}

	if err := s.executor.DeleteServer(ctx, server); err != nil {
		resourceTask.Status = task.StatusFailed
		resourceTask.ErrorCode = "delete_failed"
		resourceTask.ErrorMessage = err.Error()
		resourceTask.UpdatedAt = time.Now().UTC()
		s.storeTask(ctx, resourceTask)
		return resourceTask, err
	}
	if finalizersEnabled {
		_ = finalizerStore.RemoveFinalizer(ctx, "server", server.ID, "compute.destroy")
	}

	var cleanupErr error
	var cleanupMessages []string
	for _, portID := range server.PortIDs {
		if err := s.networks.DeletePort(ctx, portID); err != nil && !errors.Is(err, ports.ErrNetworkNotFound) {
			if cleanupErr == nil {
				cleanupErr = err
			}
			cleanupMessages = append(cleanupMessages, fmt.Sprintf("delete port %s: %v", portID, err))
		}
	}
	if finalizersEnabled && cleanupErr == nil {
		_ = finalizerStore.RemoveFinalizer(ctx, "server", server.ID, "network.cleanup")
	}

	releaseReq := ports.ReleaseHostRequest{
		ServerID: server.ID,
		HostID:   server.HostID,
		VCPUs:    server.VCPUs,
		MemoryMB: server.MemoryMB,
		DiskGB:   server.DiskGB,
	}
	if releaseReq.VCPUs == 0 && releaseReq.MemoryMB == 0 && releaseReq.DiskGB == 0 {
		resources, err := resourcesForFlavor(server.Flavor)
		if err == nil {
			releaseReq.VCPUs = resources.VCPUs
			releaseReq.MemoryMB = resources.MemoryMB
			releaseReq.DiskGB = resources.DiskGB
		}
	}

	releaseErr := s.selector.ReleaseHost(ctx, releaseReq)
	if finalizersEnabled && releaseErr == nil {
		_ = finalizerStore.RemoveFinalizer(ctx, "server", server.ID, "placement.release")
	}
	if err := s.cmdPublisher.PublishDesiredState(ctx, server.HostID, time.Now().UnixNano(), nil, server.ID); err != nil {
		if cleanupErr == nil {
			cleanupErr = err
		}
		cleanupMessages = append(cleanupMessages, fmt.Sprintf("publish desired-state delete: %v", err))
	}
	resourceTask.UpdatedAt = time.Now().UTC()

	if finalizersEnabled {
		remaining, err := finalizerStore.ListFinalizers(ctx, "server", server.ID)
		if err == nil && len(remaining) == 0 {
			_ = s.store.DeleteServer(ctx, serverID)
			if quotaStore, ok := s.store.(ports.QuotaStore); ok {
				_ = quotaStore.ReleaseProjectQuota(ctx, server.ProjectID, 1, server.VCPUs, server.MemoryMB)
			}
		}
	} else {
		_ = s.store.DeleteServer(ctx, serverID)
		if quotaStore, ok := s.store.(ports.QuotaStore); ok {
			_ = quotaStore.ReleaseProjectQuota(ctx, server.ProjectID, 1, server.VCPUs, server.MemoryMB)
		}
	}
	switch {
	case cleanupErr != nil && releaseErr != nil:
		resourceTask.Status = task.StatusFailed
		resourceTask.ErrorCode = "delete_cleanup_failed"
		resourceTask.ErrorMessage = strings.Join(append(cleanupMessages, releaseErr.Error()), "; ")
	case cleanupErr != nil:
		resourceTask.Status = task.StatusFailed
		resourceTask.ErrorCode = "port_cleanup_failed"
		resourceTask.ErrorMessage = strings.Join(cleanupMessages, "; ")
	case releaseErr != nil:
		resourceTask.Status = task.StatusFailed
		resourceTask.ErrorCode = "placement_release_failed"
		resourceTask.ErrorMessage = releaseErr.Error()
	default:
		resourceTask.Status = task.StatusSucceeded
	}
	_ = s.store.SaveTask(ctx, resourceTask)
	s.publishTask(ctx, resourceTask)

	if cleanupErr != nil {
		if releaseErr != nil {
			return resourceTask, fmt.Errorf("%s; %s", cleanupErr.Error(), releaseErr.Error())
		}
		return resourceTask, cleanupErr
	}
	if releaseErr != nil {
		return resourceTask, releaseErr
	}
	return resourceTask, nil
}

func (s *Service) AttachVolume(ctx context.Context, actorUserID string, serverID, volumeID string) (compute.Server, task.Task, error) {
	ctx, span := tracer.Start(ctx, "compute.AttachVolume")
	defer span.End()

	now := time.Now().UTC()
	resourceTask := task.Task{
		ID:          ids.New("tsk"),
		Kind:        "server.attach_volume",
		Scope:       "project",
		TargetRef:   serverID,
		Status:      task.StatusRunning,
		RequestedBy: actorUserID,
		RequestID:   httpx.RequestIDFromContextOrNew(ctx),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	resourceTask, created, err := s.claimTask(ctx, resourceTask)
	if err != nil {
		return compute.Server{}, task.Task{}, err
	}
	if !created {
		return s.existingServerTaskResult(ctx, resourceTask)
	}

	server, err := s.store.GetServer(ctx, serverID)
	if errors.Is(err, ports.ErrServerNotFound) {
		resourceTask.Status = task.StatusFailed
		resourceTask.ErrorCode = "server_not_found"
		resourceTask.ErrorMessage = ErrServerNotFound.Error()
		resourceTask.UpdatedAt = time.Now().UTC()
		s.storeTask(ctx, resourceTask)
		return compute.Server{}, resourceTask, ErrServerNotFound
	}
	if err != nil {
		return compute.Server{}, task.Task{}, err
	}
	resourceTask.CellID = server.CellID
	resourceTask.HostID = server.HostID
	s.storeTask(ctx, resourceTask)

	span.SetAttributes(
		attribute.String("orion.server_id", serverID),
		attribute.String("orion.volume_id", volumeID),
		attribute.String("orion.task_id", resourceTask.ID),
	)

	volume, err := s.volumes.AttachVolume(ctx, volumeID, server.ID, server.HostID)
	if err != nil {
		return s.failServerTask(ctx, server, resourceTask, "volume_attach_failed", mapVolumeError(err))
	}
	if err := s.executor.AttachVolume(ctx, server, volume); err != nil {
		_, _ = s.volumes.DetachVolume(ctx, volumeID)
		return s.failServerTask(ctx, server, resourceTask, "libvirt_attach_failed", err)
	}

	server.VolumeIDs = append(server.VolumeIDs, volumeID)
	server.UpdatedAt = time.Now().UTC()
	resourceTask.Status = task.StatusSucceeded
	resourceTask.UpdatedAt = server.UpdatedAt

	_ = s.store.SaveServer(ctx, server)
	_ = s.store.SaveTask(ctx, resourceTask)
	s.publishTask(ctx, resourceTask)
	return server, resourceTask, nil
}

func (s *Service) DetachVolume(ctx context.Context, actorUserID string, serverID, volumeID string) (compute.Server, task.Task, error) {
	ctx, span := tracer.Start(ctx, "compute.DetachVolume")
	defer span.End()

	now := time.Now().UTC()
	resourceTask := task.Task{
		ID:          ids.New("tsk"),
		Kind:        "server.detach_volume",
		Scope:       "project",
		TargetRef:   serverID,
		Status:      task.StatusRunning,
		RequestedBy: actorUserID,
		RequestID:   httpx.RequestIDFromContextOrNew(ctx),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	resourceTask, created, err := s.claimTask(ctx, resourceTask)
	if err != nil {
		return compute.Server{}, task.Task{}, err
	}
	if !created {
		return s.existingServerTaskResult(ctx, resourceTask)
	}

	server, err := s.store.GetServer(ctx, serverID)
	if errors.Is(err, ports.ErrServerNotFound) {
		resourceTask.Status = task.StatusFailed
		resourceTask.ErrorCode = "server_not_found"
		resourceTask.ErrorMessage = ErrServerNotFound.Error()
		resourceTask.UpdatedAt = time.Now().UTC()
		s.storeTask(ctx, resourceTask)
		return compute.Server{}, resourceTask, ErrServerNotFound
	}
	if err != nil {
		return compute.Server{}, task.Task{}, err
	}
	if !slices.Contains(server.VolumeIDs, volumeID) {
		return s.failServerTask(ctx, server, resourceTask, "volume_not_attached", ErrVolumeNotAttached)
	}
	resourceTask.CellID = server.CellID
	resourceTask.HostID = server.HostID
	s.storeTask(ctx, resourceTask)

	span.SetAttributes(
		attribute.String("orion.server_id", serverID),
		attribute.String("orion.volume_id", volumeID),
		attribute.String("orion.task_id", resourceTask.ID),
	)

	volume, err := s.volumes.GetVolume(ctx, volumeID)
	if err != nil {
		return s.failServerTask(ctx, server, resourceTask, "volume_lookup_failed", mapVolumeError(err))
	}
	if err := s.executor.DetachVolume(ctx, server, volume); err != nil {
		return s.failServerTask(ctx, server, resourceTask, "libvirt_detach_failed", err)
	}
	if _, err := s.volumes.DetachVolume(ctx, volumeID); err != nil {
		return s.failServerTask(ctx, server, resourceTask, "volume_state_detach_failed", mapVolumeError(err))
	}

	server.VolumeIDs = removeString(server.VolumeIDs, volumeID)
	server.UpdatedAt = time.Now().UTC()
	resourceTask.Status = task.StatusSucceeded
	resourceTask.UpdatedAt = server.UpdatedAt

	_ = s.store.SaveServer(ctx, server)
	_ = s.store.SaveTask(ctx, resourceTask)
	s.publishTask(ctx, resourceTask)
	return server, resourceTask, nil
}

func (s *Service) failServerTask(ctx context.Context, server compute.Server, resourceTask task.Task, code string, err error) (compute.Server, task.Task, error) {
	server.Status = "error"
	server.UpdatedAt = time.Now().UTC()
	resourceTask.Status = task.StatusFailed
	resourceTask.ErrorCode = code
	resourceTask.ErrorMessage = err.Error()
	resourceTask.UpdatedAt = server.UpdatedAt

	_ = s.store.SaveServer(ctx, server)
	_ = s.store.SaveTask(ctx, resourceTask)
	s.publishTask(ctx, resourceTask)
	return server, resourceTask, err
}

func (s *Service) failCreateServerTask(
	ctx context.Context,
	server compute.Server,
	resourceTask task.Task,
	code string,
	err error,
	portIDs []string,
	deleteInstance bool,
	releaseHost bool,
) (compute.Server, task.Task, error) {
	if cleanupErr := s.cleanupFailedServerCreate(ctx, server, portIDs, deleteInstance, releaseHost); cleanupErr != nil {
		err = fmt.Errorf("%w; cleanup: %v", err, cleanupErr)
	}
	return s.failServerTask(ctx, server, resourceTask, code, err)
}

func (s *Service) claimTask(ctx context.Context, resourceTask task.Task) (task.Task, bool, error) {
	resourceTask, created, err := s.store.CreateTaskIfAbsent(ctx, resourceTask)
	if err != nil {
		return task.Task{}, false, err
	}
	return resourceTask, created, nil
}

func (s *Service) existingCreateResult(ctx context.Context, resourceTask task.Task) (compute.Server, task.Task, error) {
	server, err := s.store.GetServer(ctx, resourceTask.TargetRef)
	if errors.Is(err, ports.ErrServerNotFound) {
		return compute.Server{
			ID:        resourceTask.TargetRef,
			TaskID:    resourceTask.ID,
			Status:    serverStatusFromTask(resourceTask.Status),
			CreatedAt: resourceTask.CreatedAt,
			UpdatedAt: resourceTask.UpdatedAt,
		}, resourceTask, nil
	}
	if err != nil {
		return compute.Server{}, task.Task{}, err
	}
	return server, resourceTask, nil
}

func (s *Service) existingServerTaskResult(ctx context.Context, resourceTask task.Task) (compute.Server, task.Task, error) {
	server, err := s.store.GetServer(ctx, resourceTask.TargetRef)
	if errors.Is(err, ports.ErrServerNotFound) {
		return compute.Server{}, resourceTask, ErrServerNotFound
	}
	if err != nil {
		return compute.Server{}, task.Task{}, err
	}
	return server, resourceTask, nil
}

func (s *Service) storeTask(ctx context.Context, resourceTask task.Task) {
	_ = s.store.SaveTask(ctx, resourceTask)
	if span := trace.SpanFromContext(ctx); span.IsRecording() {
		span.SetAttributes(
			attribute.String("orion.task_id", resourceTask.ID),
			attribute.String("orion.task_kind", resourceTask.Kind),
			attribute.String("orion.task_status", string(resourceTask.Status)),
		)
	}
	s.publishTask(ctx, resourceTask)
}

func (s *Service) publishTask(ctx context.Context, t task.Task) {
	eventType := events.TaskSucceeded
	switch t.Status {
	case task.StatusFailed:
		eventType = events.TaskFailed
	case task.StatusRunning:
		eventType = events.TaskStarted
	case task.StatusPending:
		eventType = events.TaskCreated
	}
	_ = s.publisher.PublishTask(ctx, events.TaskEvent{EventType: eventType, Task: t})
}

func mapVolumeError(err error) error {
	switch {
	case errors.Is(err, ports.ErrVolumeNotFound):
		return ErrVolumeNotFound
	case errors.Is(err, ports.ErrVolumeBackendUnavailable):
		return ErrVolumeBackendUnavailable
	case errors.Is(err, ports.ErrVolumeInUse):
		return ErrVolumeInUse
	case errors.Is(err, ports.ErrVolumeNotAttached):
		return ErrVolumeNotAttached
	default:
		return err
	}
}

func (s *Service) cleanupFailedServerCreate(ctx context.Context, server compute.Server, portIDs []string, deleteInstance bool, releaseHost bool) error {
	var cleanupMessages []string

	if deleteInstance {
		if err := s.executor.DeleteServer(ctx, server); err != nil && !errors.Is(err, ports.ErrServerInstanceNotFound) {
			cleanupMessages = append(cleanupMessages, fmt.Sprintf("delete server instance: %v", err))
		}
	}

	for _, portID := range portIDs {
		if err := s.networks.DeletePort(ctx, portID); err != nil && !errors.Is(err, ports.ErrNetworkNotFound) {
			cleanupMessages = append(cleanupMessages, fmt.Sprintf("delete port %s: %v", portID, err))
		}
	}

	if releaseHost && server.HostID != "" {
		if err := s.selector.ReleaseHost(ctx, ports.ReleaseHostRequest{
			ServerID: server.ID,
			HostID:   server.HostID,
			VCPUs:    server.VCPUs,
			MemoryMB: server.MemoryMB,
			DiskGB:   server.DiskGB,
		}); err != nil {
			cleanupMessages = append(cleanupMessages, fmt.Sprintf("release host %s: %v", server.HostID, err))
		}
	}

	if len(cleanupMessages) == 0 {
		return nil
	}
	return errors.New(strings.Join(cleanupMessages, "; "))
}

func removeString(items []string, target string) []string {
	filtered := make([]string, 0, len(items))
	for _, item := range items {
		if item != target {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func serverStatusFromTask(status task.Status) string {
	switch status {
	case task.StatusFailed:
		return "error"
	case task.StatusSucceeded:
		return "active"
	default:
		return "building"
	}
}

type flavorResources struct {
	VCPUs    int
	MemoryMB int
	DiskGB   int
}

func resourcesForFlavor(flavor string) (flavorResources, error) {
	switch flavor {
	case "tiny":
		return flavorResources{VCPUs: 1, MemoryMB: 512, DiskGB: 1}, nil
	case "small":
		return flavorResources{VCPUs: 1, MemoryMB: 1024, DiskGB: 2}, nil
	case "medium":
		return flavorResources{VCPUs: 2, MemoryMB: 2048, DiskGB: 4}, nil
	case "large":
		return flavorResources{VCPUs: 4, MemoryMB: 4096, DiskGB: 8}, nil
	default:
		return flavorResources{}, ErrUnknownFlavor
	}
}

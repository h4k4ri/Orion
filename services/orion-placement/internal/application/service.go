package application

import (
	"context"
	"errors"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/horizon/orion/libs/go/kit/ids"
	"github.com/horizon/orion/services/orion-placement/internal/domain"
	"github.com/horizon/orion/services/orion-placement/internal/ports"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

var ErrNoValidHost = errors.New("no valid host")
var ErrHostNotFound = errors.New("host not found")
var ErrReservationNotFound = errors.New("reservation not found")
var ErrResourceProviderNotFound = errors.New("resource provider not found")
var ErrResourceProviderConflict = errors.New("resource provider hierarchy conflict")

var tracer = otel.Tracer("github.com/horizon/orion/services/orion-placement")

type Service struct {
	store        ports.PlacementStore
	scoreWeights domain.ScoreWeights
}

func NewService(store ports.PlacementStore, configuredWeights ...domain.ScoreWeights) *Service {
	weights := domain.ScoreWeights{VCPUs: 1, Memory: 1.0 / 1024.0, Disk: 1}
	if len(configuredWeights) > 0 {
		if configuredWeights[0].VCPUs > 0 {
			weights.VCPUs = configuredWeights[0].VCPUs
		}
		if configuredWeights[0].Memory > 0 {
			weights.Memory = configuredWeights[0].Memory
		}
		if configuredWeights[0].Disk > 0 {
			weights.Disk = configuredWeights[0].Disk
		}
	}
	return &Service{store: store, scoreWeights: weights}
}

func (s *Service) CreateResourceProvider(ctx context.Context, req domain.CreateResourceProviderRequest) (domain.ResourceProvider, error) {
	store, ok := s.store.(ports.ResourceProviderStore)
	if !ok {
		return domain.ResourceProvider{}, ErrResourceProviderConflict
	}
	if strings.TrimSpace(req.Name) == "" {
		return domain.ResourceProvider{}, ErrResourceProviderConflict
	}
	uuid := strings.TrimSpace(req.UUID)
	if uuid == "" {
		uuid = ids.New("rp")
	}
	root := uuid
	if req.ParentProviderID != "" {
		parent, err := store.GetResourceProvider(ctx, req.ParentProviderID)
		if errors.Is(err, ports.ErrResourceProviderNotFound) {
			return domain.ResourceProvider{}, ErrResourceProviderNotFound
		}
		if err != nil {
			return domain.ResourceProvider{}, err
		}
		root = parent.RootProviderID
	}
	provider := domain.ResourceProvider{UUID: uuid, Name: strings.TrimSpace(req.Name), ParentProviderID: req.ParentProviderID, RootProviderID: root, Generation: 1, Traits: normalizeStrings(req.Traits), Inventories: cloneInventories(req.Inventories)}
	if err := store.SaveResourceProvider(ctx, provider); err != nil {
		return domain.ResourceProvider{}, err
	}
	return provider, nil
}

func (s *Service) GetResourceProvider(ctx context.Context, uuid string) (domain.ResourceProvider, error) {
	store, ok := s.store.(ports.ResourceProviderStore)
	if !ok {
		return domain.ResourceProvider{}, ErrResourceProviderConflict
	}
	provider, err := store.GetResourceProvider(ctx, uuid)
	if errors.Is(err, ports.ErrResourceProviderNotFound) {
		return domain.ResourceProvider{}, ErrResourceProviderNotFound
	}
	return provider, err
}

func (s *Service) ListResourceProviders(ctx context.Context, rootUUID string) ([]domain.ResourceProvider, error) {
	store, ok := s.store.(ports.ResourceProviderStore)
	if !ok {
		return nil, ErrResourceProviderConflict
	}
	return store.ListResourceProviders(ctx, rootUUID)
}

func (s *Service) UpdateResourceProvider(ctx context.Context, req domain.UpdateResourceProviderRequest) (domain.ResourceProvider, error) {
	store, ok := s.store.(ports.ResourceProviderStore)
	if !ok {
		return domain.ResourceProvider{}, ErrResourceProviderConflict
	}
	provider, err := s.GetResourceProvider(ctx, req.UUID)
	if err != nil {
		return domain.ResourceProvider{}, err
	}
	if req.Name != "" {
		provider.Name = strings.TrimSpace(req.Name)
	}
	if req.Traits != nil {
		provider.Traits = normalizeStrings(req.Traits)
	}
	if req.Inventories != nil {
		provider.Inventories = cloneInventories(req.Inventories)
	}
	provider.Generation++
	if err := store.SaveResourceProvider(ctx, provider); err != nil {
		return domain.ResourceProvider{}, err
	}
	return provider, nil
}

func (s *Service) DeleteResourceProvider(ctx context.Context, uuid string) error {
	store, ok := s.store.(ports.ResourceProviderStore)
	if !ok {
		return ErrResourceProviderConflict
	}
	provider, err := s.GetResourceProvider(ctx, uuid)
	if err != nil {
		return err
	}
	items, err := store.ListResourceProviders(ctx, provider.RootProviderID)
	if err != nil {
		return err
	}
	for _, item := range items {
		if item.ParentProviderID == uuid {
			return ErrResourceProviderConflict
		}
	}
	return store.DeleteResourceProvider(ctx, uuid)
}

func cloneInventories(items map[string]domain.ProviderInventory) map[string]domain.ProviderInventory {
	if items == nil {
		return nil
	}
	result := make(map[string]domain.ProviderInventory, len(items))
	for key, item := range items {
		result[key] = item
	}
	return result
}
func normalizeStrings(items []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.ToLower(strings.TrimSpace(item))
		if item != "" {
			if _, ok := seen[item]; !ok {
				seen[item] = struct{}{}
				result = append(result, item)
			}
		}
	}
	sort.Strings(result)
	return result
}

func (s *Service) RegisterHost(ctx context.Context, req domain.RegisterHostRequest) domain.Host {
	ctx, span := tracer.Start(ctx, "placement.RegisterHost")
	defer span.End()
	span.SetAttributes(
		attribute.String("host_id", req.HostID),
		attribute.String("cell_id", req.CellID),
	)

	existing, err := s.store.GetHost(ctx, req.HostID)
	normalizedTraits := normalizeTraits(req.Traits)
	normalizedNUMA := normalizeNUMA(req.NUMA)
	normalizedGPUs := normalizeGPUs(req.GPUs)

	var gen int64 = 1
	var inventory domain.Inventory
	if err == nil {
		gen = existing.Generation + 1
		inventory = existing.Inventory
	}
	inventory.VCPUsTotal = req.VCPUs
	inventory.MemoryMBTotal = req.MemoryMB
	inventory.DiskGBTotal = req.DiskGB
	inventory.NUMA = mergeNUMAAllocations(normalizedNUMA, existing.Inventory.NUMA)
	inventory.GPUs = mergeGPUAllocations(normalizedGPUs, existing.Inventory.GPUs)

	host := domain.Host{
		HostID:              req.HostID,
		CellID:              req.CellID,
		Group:               req.Group,
		Enabled:             req.Enabled,
		Drained:             req.Drained,
		NodeAgentURL:        coalesceString(req.NodeAgentURL, existing.NodeAgentURL),
		VolumeHostAgentURL:  coalesceString(req.VolumeHostAgentURL, existing.VolumeHostAgentURL),
		NetworkHostAgentURL: coalesceString(req.NetworkHostAgentURL, existing.NetworkHostAgentURL),
		Traits:              normalizedTraits,
		Inventory:           inventory,
		Generation:          gen,
		AvailabilityZone:    coalesceString(req.AvailabilityZone, existing.AvailabilityZone),
		Topology:            existing.Topology,
	}

	_ = s.store.SaveHost(ctx, host)
	return host
}

// HeartbeatHost refreshes inventory without accidentally clearing an operator's
// drain/disable state. Registration is allowed to establish a new host; later
// heartbeats preserve lifecycle flags owned by placement operators.
func (s *Service) HeartbeatHost(ctx context.Context, req domain.RegisterHostRequest) (domain.Host, error) {
	existing, err := s.store.GetHost(ctx, req.HostID)
	if err != nil && !errors.Is(err, ports.ErrHostRecordNotFound) {
		return domain.Host{}, err
	}
	if err == nil {
		req.Enabled = existing.Enabled
		req.Drained = existing.Drained
		req.Group = existing.Group
		req.NodeAgentURL = coalesceString(req.NodeAgentURL, existing.NodeAgentURL)
		req.VolumeHostAgentURL = coalesceString(req.VolumeHostAgentURL, existing.VolumeHostAgentURL)
		req.NetworkHostAgentURL = coalesceString(req.NetworkHostAgentURL, existing.NetworkHostAgentURL)
		req.AvailabilityZone = coalesceString(req.AvailabilityZone, existing.AvailabilityZone)
	}
	return s.RegisterHost(ctx, req), nil
}

func (s *Service) UpdateHostTopology(ctx context.Context, hostID string, az string, datacenter, rack, aggregate string) error {
	ctx, span := tracer.Start(ctx, "placement.UpdateHostTopology")
	defer span.End()
	span.SetAttributes(attribute.String("host_id", hostID))

	host, err := s.store.GetHost(ctx, hostID)
	if errors.Is(err, ports.ErrHostRecordNotFound) {
		return ErrHostNotFound
	}
	if err != nil {
		return err
	}

	host.AvailabilityZone = coalesceString(az, host.AvailabilityZone)
	host.Topology.Datacenter = coalesceString(datacenter, host.Topology.Datacenter)
	host.Topology.Rack = coalesceString(rack, host.Topology.Rack)
	host.Topology.HostAggregate = coalesceString(aggregate, host.Topology.HostAggregate)
	host.Generation++

	return s.store.SaveHost(ctx, host)
}

func (s *Service) ListHosts(ctx context.Context) []domain.Host {
	hosts, _ := s.store.ListHosts(ctx)
	sort.Slice(hosts, func(i, j int) bool {
		return hosts[i].HostID < hosts[j].HostID
	})
	return hosts
}

func (s *Service) GetHost(ctx context.Context, hostID string) (domain.Host, error) {
	ctx, span := tracer.Start(ctx, "placement.GetHost")
	defer span.End()
	span.SetAttributes(attribute.String("host_id", hostID))

	h, err := s.store.GetHost(ctx, hostID)
	if errors.Is(err, ports.ErrHostRecordNotFound) {
		return domain.Host{}, ErrHostNotFound
	}
	return h, err
}

func (s *Service) SelectHost(ctx context.Context, req domain.SelectHostRequest) (domain.HostSelection, error) {
	ctx, span := tracer.Start(ctx, "placement.SelectHost")
	defer span.End()
	span.SetAttributes(
		attribute.Int("vcpus", req.VCPUs),
		attribute.Int("memory_mb", req.MemoryMB),
		attribute.Int("disk_gb", req.DiskGB),
		attribute.String("scoring_strategy", string(req.ScoringStrategy)),
	)

	hosts, err := s.store.ListHosts(ctx)
	if err != nil {
		return domain.HostSelection{}, err
	}

	strategy := req.ScoringStrategy
	if strategy == "" {
		strategy = domain.ScoringStrategyBinPacking
	}

	candidates := filterHosts(hosts, req)
	span.SetAttributes(attribute.Int("candidates", len(candidates)))

	if len(candidates) == 0 {
		return domain.HostSelection{}, ErrNoValidHost
	}

	scored, err := s.scoreHosts(ctx, candidates, req, strategy)
	if err != nil {
		return domain.HostSelection{}, err
	}

	sort.Slice(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return scored[i].host.HostID < scored[j].host.HostID
	})

	for _, candidate := range scored {
		host := candidate.host
		updated, err := s.store.UpdateAllocation(ctx, domain.AllocationUpdate{
			HostID:        host.HostID,
			ExpectedGen:   host.Generation,
			DeltaVCPUs:    req.VCPUs,
			DeltaMemoryMB: req.MemoryMB,
			DeltaDiskGB:   req.DiskGB,
		})
		if errors.Is(err, ports.ErrConcurrentUpdate) {
			continue
		}
		if err != nil {
			return domain.HostSelection{}, err
		}

		span.SetAttributes(
			attribute.String("selected_host_id", updated.HostID),
			attribute.String("cell_id", updated.CellID),
		)
		return domain.HostSelection{
			CellID: updated.CellID,
			HostID: updated.HostID,
		}, nil
	}

	return domain.HostSelection{}, ErrNoValidHost
}

type scoredHost struct {
	host  domain.Host
	score float64
}

func filterHosts(hosts []domain.Host, req domain.SelectHostRequest) []domain.Host {
	var result []domain.Host
	for _, host := range hosts {
		if req.AffinityHostID != "" && host.HostID != req.AffinityHostID {
			continue
		}
		if req.CellID != "" && host.CellID != req.CellID {
			continue
		}
		if !host.Enabled || host.Drained {
			continue
		}
		if req.RequireNodeAgent && strings.TrimSpace(host.NodeAgentURL) == "" {
			continue
		}
		if req.RequireVolumeHostAgent && strings.TrimSpace(host.VolumeHostAgentURL) == "" {
			continue
		}
		if !hasTraits(host.Traits, req.TraitsRequired) {
			continue
		}
		if len(req.AvailabilityZones) > 0 && !slices.Contains(req.AvailabilityZones, host.AvailabilityZone) {
			continue
		}
		if req.HostAggregate != "" && host.Topology.HostAggregate != req.HostAggregate {
			continue
		}
		if availableVCPUs(host) < req.VCPUs || availableMemoryMB(host) < req.MemoryMB || availableDiskGB(host) < req.DiskGB {
			continue
		}
		result = append(result, host)
	}
	return result
}

func (s *Service) scoreHosts(ctx context.Context, hosts []domain.Host, req domain.SelectHostRequest, strategy domain.ScoringStrategy) ([]scoredHost, error) {
	var scored []scoredHost
	for _, host := range hosts {
		score, err := s.calculateScore(ctx, host, req, strategy)
		if err != nil {
			return nil, err
		}
		scored = append(scored, scoredHost{host: host, score: score})
	}
	return scored, nil
}

func (s *Service) calculateScore(ctx context.Context, host domain.Host, req domain.SelectHostRequest, strategy domain.ScoringStrategy) (float64, error) {
	availCPU := availableVCPUs(host)
	availMem := availableMemoryMB(host)
	availDisk := availableDiskGB(host)

	weights := req.ScoreWeights
	if weights.VCPUs == 0 {
		weights.VCPUs = s.scoreWeights.VCPUs
	}
	if weights.Memory == 0 {
		weights.Memory = s.scoreWeights.Memory
	}
	if weights.Disk == 0 {
		weights.Disk = s.scoreWeights.Disk
	}
	totalAvail := float64(availCPU)*weights.VCPUs + float64(availMem)*weights.Memory + float64(availDisk)*weights.Disk

	var score float64

	switch strategy {
	case domain.ScoringStrategyBinPacking:
		score = 1000.0 - float64(totalAvail)
	case domain.ScoringStrategySpread:
		score = float64(totalAvail)
	default:
		score = 1000.0 - float64(totalAvail)
	}

	if req.AntiAffinityProjectID != "" {
		count, err := s.store.CountProjectInstancesOnHost(ctx, host.HostID, req.AntiAffinityProjectID)
		if err != nil {
			return 0, err
		}
		antiAffinityWeight := 100.0 * float64(count)
		score -= antiAffinityWeight
	}

	return score, nil
}

func (s *Service) ReleaseHost(ctx context.Context, req domain.ReleaseHostRequest) error {
	ctx, span := tracer.Start(ctx, "placement.ReleaseHost")
	defer span.End()
	span.SetAttributes(
		attribute.String("host_id", req.HostID),
		attribute.String("server_id", req.ServerID),
		attribute.Int("vcpus", req.VCPUs),
		attribute.Int("memory_mb", req.MemoryMB),
		attribute.Int("disk_gb", req.DiskGB),
	)

	host, err := s.store.GetHost(ctx, req.HostID)
	if errors.Is(err, ports.ErrHostRecordNotFound) {
		return ErrHostNotFound
	}
	if err != nil {
		return err
	}

	host.Inventory.VCPUsAllocated = max(0, host.Inventory.VCPUsAllocated-req.VCPUs)
	host.Inventory.MemoryAllocatedMB = max(0, host.Inventory.MemoryAllocatedMB-req.MemoryMB)
	host.Inventory.DiskAllocatedGB = max(0, host.Inventory.DiskAllocatedGB-req.DiskGB)
	host.Generation++
	return s.store.SaveHost(ctx, host)
}

func (s *Service) UpdateHostState(ctx context.Context, req domain.UpdateHostStateRequest) (domain.Host, error) {
	ctx, span := tracer.Start(ctx, "placement.UpdateHostState")
	defer span.End()
	span.SetAttributes(attribute.String("host_id", req.HostID))

	host, err := s.store.GetHost(ctx, req.HostID)
	if errors.Is(err, ports.ErrHostRecordNotFound) {
		return domain.Host{}, ErrHostNotFound
	}
	if err != nil {
		return domain.Host{}, err
	}

	if req.Enabled != nil {
		host.Enabled = *req.Enabled
	}
	if req.Drained != nil {
		host.Drained = *req.Drained
	}
	host.Generation++

	if err := s.store.SaveHost(ctx, host); err != nil {
		return domain.Host{}, err
	}
	return host, nil
}

func (s *Service) CreateReservation(ctx context.Context, req domain.CreateReservationRequest) (domain.Reservation, error) {
	ctx, span := tracer.Start(ctx, "placement.CreateReservation")
	defer span.End()
	span.SetAttributes(
		attribute.String("host_id", req.HostID),
		attribute.String("project_id", req.ProjectID),
		attribute.Int("vcpus", req.VCPUs),
		attribute.Int("memory_mb", req.MemoryMB),
		attribute.Int("disk_gb", req.DiskGB),
		attribute.Int64("ttl_seconds", req.TTLSeconds),
	)

	now := time.Now().UTC()
	if req.TTLSeconds <= 0 {
		req.TTLSeconds = 300
	}
	reservation := domain.Reservation{
		ID:           ids.New("rsv"),
		HostID:       req.HostID,
		ProjectID:    req.ProjectID,
		ServerID:     req.ServerID,
		VCPUs:        req.VCPUs,
		MemoryMB:     req.MemoryMB,
		DiskGB:       req.DiskGB,
		FencingToken: time.Now().UnixNano(),
		ExpiresAt:    now.Add(time.Duration(req.TTLSeconds) * time.Second).Unix(),
		CreatedAt:    now.Unix(),
	}

	if err := s.store.CreateReservation(ctx, reservation); err != nil {
		return domain.Reservation{}, err
	}

	span.SetAttributes(attribute.String("reservation_id", reservation.ID))
	return reservation, nil
}

func (s *Service) GetReservation(ctx context.Context, id string) (domain.Reservation, error) {
	ctx, span := tracer.Start(ctx, "placement.GetReservation")
	defer span.End()
	span.SetAttributes(attribute.String("reservation_id", id))

	r, err := s.store.GetReservation(ctx, id)
	if errors.Is(err, ErrReservationNotFound) {
		return domain.Reservation{}, ErrReservationNotFound
	}
	return r, err
}

func (s *Service) DeleteReservation(ctx context.Context, id string) error {
	ctx, span := tracer.Start(ctx, "placement.DeleteReservation")
	defer span.End()
	span.SetAttributes(attribute.String("reservation_id", id))

	return s.store.DeleteReservation(ctx, id)
}

func (s *Service) ListReservations(ctx context.Context, projectID string) ([]domain.Reservation, error) {
	ctx, span := tracer.Start(ctx, "placement.ListReservations")
	defer span.End()
	span.SetAttributes(attribute.String("project_id", projectID))

	return s.store.ListReservationsByProject(ctx, projectID)
}

func (s *Service) CleanupExpiredReservations(ctx context.Context) (int64, error) {
	ctx, span := tracer.Start(ctx, "placement.CleanupExpiredReservations")
	defer span.End()

	count, err := s.store.CleanupExpiredReservations(ctx)
	if err != nil {
		return 0, err
	}
	span.SetAttributes(attribute.Int64("cleaned_up", count))
	return count, nil
}

func hasTraits(hostTraits, required []string) bool {
	for _, trait := range required {
		if !slices.Contains(hostTraits, trait) {
			return false
		}
	}
	return true
}

func availableVCPUs(host domain.Host) int {
	return host.Inventory.VCPUsTotal - host.Inventory.VCPUsAllocated
}

func availableMemoryMB(host domain.Host) int {
	return host.Inventory.MemoryMBTotal - host.Inventory.MemoryAllocatedMB
}

func availableDiskGB(host domain.Host) int {
	return host.Inventory.DiskGBTotal - host.Inventory.DiskAllocatedGB
}

func normalizeTraits(traits []string) []string {
	if len(traits) == 0 {
		return []string{}
	}

	seen := make(map[string]struct{}, len(traits))
	normalized := make([]string, 0, len(traits))
	for _, trait := range traits {
		value := strings.ToLower(strings.TrimSpace(trait))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}
	sort.Strings(normalized)
	return normalized
}

func normalizeNUMA(nodes []domain.NUMANode) []domain.NUMANode {
	if len(nodes) == 0 {
		return []domain.NUMANode{}
	}

	normalized := make([]domain.NUMANode, 0, len(nodes))
	for _, node := range nodes {
		copyNode := domain.NUMANode{
			ID:                node.ID,
			VCPUs:             append([]int(nil), node.VCPUs...),
			MemoryMBTotal:     node.MemoryMBTotal,
			MemoryAllocatedMB: node.MemoryAllocatedMB,
		}
		sort.Ints(copyNode.VCPUs)
		normalized = append(normalized, copyNode)
	}
	sort.Slice(normalized, func(i, j int) bool {
		return normalized[i].ID < normalized[j].ID
	})
	return normalized
}

func normalizeGPUs(gpus []domain.GPUDevice) []domain.GPUDevice {
	if len(gpus) == 0 {
		return []domain.GPUDevice{}
	}

	normalized := make([]domain.GPUDevice, 0, len(gpus))
	for _, gpu := range gpus {
		normalized = append(normalized, domain.GPUDevice{
			ID:          strings.TrimSpace(gpu.ID),
			Vendor:      strings.TrimSpace(gpu.Vendor),
			Model:       strings.TrimSpace(gpu.Model),
			MemoryMB:    gpu.MemoryMB,
			Traits:      normalizeTraits(gpu.Traits),
			AllocatedTo: strings.TrimSpace(gpu.AllocatedTo),
		})
	}
	sort.Slice(normalized, func(i, j int) bool {
		return normalized[i].ID < normalized[j].ID
	})
	return normalized
}

func mergeNUMAAllocations(nodes, existing []domain.NUMANode) []domain.NUMANode {
	if len(nodes) == 0 {
		return []domain.NUMANode{}
	}

	existingByID := make(map[int]domain.NUMANode, len(existing))
	for _, node := range existing {
		existingByID[node.ID] = node
	}

	merged := make([]domain.NUMANode, 0, len(nodes))
	for _, node := range nodes {
		current := node
		if previous, ok := existingByID[node.ID]; ok {
			current.MemoryAllocatedMB = previous.MemoryAllocatedMB
		}
		merged = append(merged, current)
	}
	return merged
}

func mergeGPUAllocations(gpus, existing []domain.GPUDevice) []domain.GPUDevice {
	if len(gpus) == 0 {
		return []domain.GPUDevice{}
	}

	existingByID := make(map[string]domain.GPUDevice, len(existing))
	for _, gpu := range existing {
		existingByID[gpu.ID] = gpu
	}

	merged := make([]domain.GPUDevice, 0, len(gpus))
	for _, gpu := range gpus {
		current := gpu
		if previous, ok := existingByID[gpu.ID]; ok {
			current.AllocatedTo = previous.AllocatedTo
		}
		merged = append(merged, current)
	}
	return merged
}

func coalesceString(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value != "" {
		return value
	}
	return strings.TrimSpace(fallback)
}

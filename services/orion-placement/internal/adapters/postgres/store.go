package postgres

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	kitpg "github.com/horizon/orion/libs/go/kit/postgres"
	"github.com/horizon/orion/services/orion-placement/internal/domain"
	"github.com/horizon/orion/services/orion-placement/internal/ports"
)

var errOpNotFound = errors.New("operation not found")

//go:embed migrations/*.sql
var migrationsFS embed.FS

type Store struct {
	pool   *pgxpool.Pool
	schema string
}

func New(ctx context.Context, pool *pgxpool.Pool) (*Store, error) {
	if err := kitpg.ApplyMigrations(ctx, pool, "orion-placement", migrationsFS, "migrations"); err != nil {
		return nil, err
	}
	return &Store{pool: pool, schema: "orion_placement"}, nil
}

func (s *Store) q(t string) string {
	return s.schema + "." + t
}

func (s *Store) SaveResourceProvider(ctx context.Context, provider domain.ResourceProvider) error {
	inventories, err := json.Marshal(provider.Inventories)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO orion_placement.resource_providers(uuid, name, parent_provider_id, root_provider_id, generation, traits, inventories) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(uuid) DO UPDATE SET name=EXCLUDED.name, traits=EXCLUDED.traits, inventories=EXCLUDED.inventories, generation=EXCLUDED.generation`, provider.UUID, provider.Name, nullableText(provider.ParentProviderID), provider.RootProviderID, provider.Generation, provider.Traits, inventories)
	return err
}

func (s *Store) GetResourceProvider(ctx context.Context, uuid string) (domain.ResourceProvider, error) {
	var provider domain.ResourceProvider
	var parent *string
	var inventories []byte
	err := s.pool.QueryRow(ctx, `SELECT uuid, name, parent_provider_id, root_provider_id, generation, traits, inventories FROM orion_placement.resource_providers WHERE uuid=$1`, uuid).Scan(&provider.UUID, &provider.Name, &parent, &provider.RootProviderID, &provider.Generation, &provider.Traits, &inventories)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ResourceProvider{}, ports.ErrResourceProviderNotFound
	}
	if err != nil {
		return domain.ResourceProvider{}, err
	}
	if parent != nil {
		provider.ParentProviderID = *parent
	}
	_ = json.Unmarshal(inventories, &provider.Inventories)
	return provider, nil
}

func (s *Store) ListResourceProviders(ctx context.Context, rootUUID string) ([]domain.ResourceProvider, error) {
	rows, err := s.pool.Query(ctx, `SELECT uuid, name, parent_provider_id, root_provider_id, generation, traits, inventories FROM orion_placement.resource_providers WHERE ($1='' OR root_provider_id=$1) ORDER BY uuid`, rootUUID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domain.ResourceProvider, 0)
	for rows.Next() {
		var provider domain.ResourceProvider
		var parent *string
		var inventories []byte
		if err := rows.Scan(&provider.UUID, &provider.Name, &parent, &provider.RootProviderID, &provider.Generation, &provider.Traits, &inventories); err != nil {
			return nil, err
		}
		if parent != nil {
			provider.ParentProviderID = *parent
		}
		_ = json.Unmarshal(inventories, &provider.Inventories)
		items = append(items, provider)
	}
	return items, rows.Err()
}

func (s *Store) DeleteResourceProvider(ctx context.Context, uuid string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM orion_placement.resource_providers WHERE uuid=$1`, uuid)
	return err
}

func nullableText(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func (s *Store) SaveHost(ctx context.Context, h domain.Host) error {
	numaJSON, err := json.Marshal(h.Inventory.NUMA)
	if err != nil {
		return err
	}
	gpuJSON, err := json.Marshal(h.Inventory.GPUs)
	if err != nil {
		return err
	}

	_, err = s.pool.Exec(ctx, `
		INSERT INTO orion_placement.placement_hosts
		    (host_id, cell_id, group_name, enabled, drained, traits,
		     node_agent_url, volume_host_agent_url, network_host_agent_url,
		     vcpus_total, vcpus_allocated, memory_mb_total, memory_mb_allocated,
		     disk_gb_total, disk_gb_allocated, numa, gpus, generation,
		     availability_zone, datacenter, rack, host_aggregate)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24)
		ON CONFLICT (host_id) DO UPDATE SET
		    cell_id             = EXCLUDED.cell_id,
		    group_name          = EXCLUDED.group_name,
		    enabled             = EXCLUDED.enabled,
		    drained             = EXCLUDED.drained,
		    traits              = EXCLUDED.traits,
		    node_agent_url      = EXCLUDED.node_agent_url,
		    volume_host_agent_url = EXCLUDED.volume_host_agent_url,
		    network_host_agent_url = EXCLUDED.network_host_agent_url,
		    vcpus_total         = EXCLUDED.vcpus_total,
		    vcpus_allocated     = EXCLUDED.vcpus_allocated,
		    memory_mb_total     = EXCLUDED.memory_mb_total,
		    memory_mb_allocated = EXCLUDED.memory_mb_allocated,
		    disk_gb_total       = EXCLUDED.disk_gb_total,
		    disk_gb_allocated   = EXCLUDED.disk_gb_allocated,
		    numa                = EXCLUDED.numa,
		    gpus                = EXCLUDED.gpus,
		    generation          = EXCLUDED.generation,
		    availability_zone   = EXCLUDED.availability_zone,
		    datacenter         = EXCLUDED.datacenter,
		    rack               = EXCLUDED.rack,
		    host_aggregate     = EXCLUDED.host_aggregate`,
		h.HostID, h.CellID, h.Group, h.Enabled, h.Drained, h.Traits,
		h.NodeAgentURL, h.VolumeHostAgentURL, h.NetworkHostAgentURL,
		h.Inventory.VCPUsTotal, h.Inventory.VCPUsAllocated,
		h.Inventory.MemoryMBTotal, h.Inventory.MemoryAllocatedMB,
		h.Inventory.DiskGBTotal, h.Inventory.DiskAllocatedGB,
		numaJSON, gpuJSON,
		h.Generation,
		h.AvailabilityZone,
		h.Topology.Datacenter,
		h.Topology.Rack,
		h.Topology.HostAggregate,
	)
	return err
}

func (s *Store) GetHost(ctx context.Context, hostID string) (domain.Host, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT host_id, cell_id, group_name, enabled, drained, traits,
		       node_agent_url, volume_host_agent_url, network_host_agent_url,
		       vcpus_total, vcpus_allocated, memory_mb_total, memory_mb_allocated,
		       disk_gb_total, disk_gb_allocated, numa, gpus, generation,
		       availability_zone, datacenter, rack, host_aggregate
		FROM `+s.q("placement_hosts")+` WHERE host_id = $1`, hostID)

	h, err := scanHost(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Host{}, ports.ErrHostRecordNotFound
	}
	return h, err
}

func (s *Store) ListHosts(ctx context.Context) ([]domain.Host, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT host_id, cell_id, group_name, enabled, drained, traits,
		       node_agent_url, volume_host_agent_url, network_host_agent_url,
		       vcpus_total, vcpus_allocated, memory_mb_total, memory_mb_allocated,
		       disk_gb_total, disk_gb_allocated, numa, gpus, generation,
		       availability_zone, datacenter, rack, host_aggregate
		FROM `+s.q("placement_hosts")+` ORDER BY host_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var hosts []domain.Host
	for rows.Next() {
		h, err := scanHost(rows)
		if err != nil {
			return nil, err
		}
		hosts = append(hosts, h)
	}
	return hosts, rows.Err()
}

func (s *Store) UpdateAllocation(ctx context.Context, req domain.AllocationUpdate) (domain.Host, error) {
	row := s.pool.QueryRow(ctx, `
		UPDATE `+s.q("placement_hosts")+` SET
		    vcpus_allocated     = vcpus_allocated     + $1,
		    memory_mb_allocated = memory_mb_allocated + $2,
		    disk_gb_allocated   = disk_gb_allocated   + $3,
		    generation          = generation + 1
		WHERE host_id   = $4
			  AND generation = $5
		RETURNING host_id, cell_id, group_name, enabled, drained, traits,
		          node_agent_url, volume_host_agent_url, network_host_agent_url,
		          vcpus_total, vcpus_allocated, memory_mb_total, memory_mb_allocated,
			  disk_gb_total, disk_gb_allocated, numa, gpus, generation,
			  availability_zone, datacenter, rack, host_aggregate`,
		req.DeltaVCPUs, req.DeltaMemoryMB, req.DeltaDiskGB,
		req.HostID, req.ExpectedGen,
	)

	h, err := scanHost(row)
	if errors.Is(err, pgx.ErrNoRows) {
		if _, e2 := s.GetHost(ctx, req.HostID); errors.Is(e2, ports.ErrHostRecordNotFound) {
			return domain.Host{}, ports.ErrHostRecordNotFound
		}
		return domain.Host{}, ports.ErrConcurrentUpdate
	}
	return h, err
}

func (s *Store) CreateReservation(ctx context.Context, r domain.Reservation) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var enabled, drained bool
	var totalCPU, allocatedCPU, totalMemory, allocatedMemory, totalDisk, allocatedDisk int
	err = tx.QueryRow(ctx, `
		SELECT enabled, drained, vcpus_total, vcpus_allocated,
		       memory_mb_total, memory_mb_allocated, disk_gb_total, disk_gb_allocated
		FROM `+s.q("placement_hosts")+` WHERE host_id = $1 FOR UPDATE`, r.HostID).
		Scan(&enabled, &drained, &totalCPU, &allocatedCPU, &totalMemory, &allocatedMemory, &totalDisk, &allocatedDisk)
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.ErrHostRecordNotFound
	}
	if err != nil {
		return err
	}
	if !enabled || drained || totalCPU-allocatedCPU < r.VCPUs || totalMemory-allocatedMemory < r.MemoryMB || totalDisk-allocatedDisk < r.DiskGB {
		return ports.ErrInsufficientCapacity
	}

	if _, err := tx.Exec(ctx, `
		UPDATE `+s.q("placement_hosts")+`
		SET vcpus_allocated = vcpus_allocated + $1,
		    memory_mb_allocated = memory_mb_allocated + $2,
		    disk_gb_allocated = disk_gb_allocated + $3,
		    generation = generation + 1
		WHERE host_id = $4`, r.VCPUs, r.MemoryMB, r.DiskGB, r.HostID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO `+s.q("placement_reservations")+`
		    (id, host_id, project_id, server_id, vcpus, memory_mb, disk_gb, fencing_token, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		r.ID, r.HostID, r.ProjectID, r.ServerID, r.VCPUs, r.MemoryMB, r.DiskGB, r.FencingToken,
		time.Unix(r.ExpiresAt, 0), time.Unix(r.CreatedAt, 0),
	); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) GetReservation(ctx context.Context, id string) (domain.Reservation, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, host_id, project_id, server_id, vcpus, memory_mb, disk_gb,
		       fencing_token, expires_at, created_at
		FROM `+s.q("placement_reservations")+`
		WHERE id = $1 AND expires_at > NOW()`,
		id,
	)
	var r domain.Reservation
	var expiresAt, createdAt time.Time
	err := row.Scan(&r.ID, &r.HostID, &r.ProjectID, &r.ServerID, &r.VCPUs, &r.MemoryMB, &r.DiskGB,
		&r.FencingToken, &expiresAt, &createdAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Reservation{}, errOpNotFound
	}
	if err != nil {
		return domain.Reservation{}, err
	}
	r.ExpiresAt = expiresAt.Unix()
	r.CreatedAt = createdAt.Unix()
	return r, nil
}

func (s *Store) DeleteReservation(ctx context.Context, id string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var hostID string
	var vcpus, memoryMB, diskGB int
	err = tx.QueryRow(ctx, `
		SELECT host_id, vcpus, memory_mb, disk_gb
		FROM `+s.q("placement_reservations")+` WHERE id = $1 FOR UPDATE`, id).
		Scan(&hostID, &vcpus, &memoryMB, &diskGB)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "DELETE FROM "+s.q("placement_reservations")+" WHERE id = $1", id); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE `+s.q("placement_hosts")+`
		SET vcpus_allocated = GREATEST(0, vcpus_allocated - $1),
		    memory_mb_allocated = GREATEST(0, memory_mb_allocated - $2),
		    disk_gb_allocated = GREATEST(0, disk_gb_allocated - $3),
		    generation = generation + 1
		WHERE host_id = $4`, vcpus, memoryMB, diskGB, hostID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) DeleteReservationByHostAndProject(ctx context.Context, hostID, projectID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var lockedHost string
	err = tx.QueryRow(ctx, `
		SELECT host_id FROM `+s.q("placement_hosts")+` WHERE host_id = $1 FOR UPDATE`, hostID).Scan(&lockedHost)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}

	var vcpus, memoryMB, diskGB int
	err = tx.QueryRow(ctx, `
		SELECT COALESCE(SUM(vcpus), 0), COALESCE(SUM(memory_mb), 0), COALESCE(SUM(disk_gb), 0)
		FROM `+s.q("placement_reservations")+`
		WHERE host_id = $1 AND project_id = $2`, hostID, projectID).Scan(&vcpus, &memoryMB, &diskGB)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		"DELETE FROM "+s.q("placement_reservations")+" WHERE host_id = $1 AND project_id = $2",
		hostID, projectID,
	); err != nil {
		return err
	}
	if vcpus != 0 || memoryMB != 0 || diskGB != 0 {
		if _, err := tx.Exec(ctx, `
			UPDATE `+s.q("placement_hosts")+`
			SET vcpus_allocated = GREATEST(0, vcpus_allocated - $1),
			    memory_mb_allocated = GREATEST(0, memory_mb_allocated - $2),
			    disk_gb_allocated = GREATEST(0, disk_gb_allocated - $3),
			    generation = generation + 1
			WHERE host_id = $4`, vcpus, memoryMB, diskGB, hostID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) ListReservationsByProject(ctx context.Context, projectID string) ([]domain.Reservation, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, host_id, project_id, server_id, vcpus, memory_mb, disk_gb,
		       fencing_token, expires_at, created_at
		FROM `+s.q("placement_reservations")+`
		WHERE project_id = $1 AND expires_at > NOW()`,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reservations []domain.Reservation
	for rows.Next() {
		var r domain.Reservation
		var expiresAt, createdAt time.Time
		err := rows.Scan(&r.ID, &r.HostID, &r.ProjectID, &r.ServerID, &r.VCPUs, &r.MemoryMB, &r.DiskGB,
			&r.FencingToken, &expiresAt, &createdAt)
		if err != nil {
			return nil, err
		}
		r.ExpiresAt = expiresAt.Unix()
		r.CreatedAt = createdAt.Unix()
		reservations = append(reservations, r)
	}
	return reservations, rows.Err()
}

func (s *Store) CountProjectInstancesOnHost(ctx context.Context, hostID, projectID string) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(1), 0)
		FROM `+s.q("placement_reservations")+`
		WHERE host_id = $1 AND project_id = $2 AND expires_at > NOW()`,
		hostID, projectID,
	).Scan(&count)
	return count, err
}

func (s *Store) CleanupExpiredReservations(ctx context.Context) (int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `
		DELETE FROM `+s.q("placement_reservations")+`
		WHERE expires_at <= NOW()
		RETURNING host_id, vcpus, memory_mb, disk_gb`)
	if err != nil {
		return 0, err
	}
	type release struct{ cpu, memory, disk int }
	byHost := map[string]release{}
	var count int64
	for rows.Next() {
		var hostID string
		var item release
		if err := rows.Scan(&hostID, &item.cpu, &item.memory, &item.disk); err != nil {
			rows.Close()
			return 0, err
		}
		current := byHost[hostID]
		current.cpu += item.cpu
		current.memory += item.memory
		current.disk += item.disk
		byHost[hostID] = current
		count++
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()
	for hostID, item := range byHost {
		if _, err := tx.Exec(ctx, `
			UPDATE `+s.q("placement_hosts")+`
			SET vcpus_allocated = GREATEST(0, vcpus_allocated - $1),
			    memory_mb_allocated = GREATEST(0, memory_mb_allocated - $2),
			    disk_gb_allocated = GREATEST(0, disk_gb_allocated - $3),
			    generation = generation + 1
			WHERE host_id = $4`, item.cpu, item.memory, item.disk, hostID); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return count, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanHost(row scanner) (domain.Host, error) {
	var h domain.Host
	var numaJSON, gpuJSON []byte
	var az, datacenter, rack, hostAggregate pgtype.Text
	err := row.Scan(
		&h.HostID, &h.CellID, &h.Group, &h.Enabled, &h.Drained, &h.Traits,
		&h.NodeAgentURL, &h.VolumeHostAgentURL, &h.NetworkHostAgentURL,
		&h.Inventory.VCPUsTotal, &h.Inventory.VCPUsAllocated,
		&h.Inventory.MemoryMBTotal, &h.Inventory.MemoryAllocatedMB,
		&h.Inventory.DiskGBTotal, &h.Inventory.DiskAllocatedGB,
		&numaJSON, &gpuJSON,
		&h.Generation,
		&az, &datacenter, &rack, &hostAggregate,
	)
	if err != nil {
		return h, err
	}
	if numaJSON != nil && len(numaJSON) > 0 {
		if err := json.Unmarshal(numaJSON, &h.Inventory.NUMA); err != nil {
			return domain.Host{}, err
		}
	}
	if gpuJSON != nil && len(gpuJSON) > 0 {
		if err := json.Unmarshal(gpuJSON, &h.Inventory.GPUs); err != nil {
			return domain.Host{}, err
		}
	}
	if az.Valid {
		h.AvailabilityZone = az.String
	}
	if datacenter.Valid {
		h.Topology.Datacenter = datacenter.String
	}
	if rack.Valid {
		h.Topology.Rack = rack.String
	}
	if hostAggregate.Valid {
		h.Topology.HostAggregate = hostAggregate.String
	}
	return h, nil
}
